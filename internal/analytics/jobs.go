package analytics

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// UnsubscribeKeyName names the signing key for unsubscribe links in app_keys.
const UnsubscribeKeyName = "weekly-stats-unsubscribe"

// Jobs runs the background stats work. Now is injectable for tests.
type Jobs struct {
	Store         *store.Store
	Mail          mail.Sender
	Log           *slog.Logger
	BaseURL       string
	AppName       string
	RetentionDays int
	Now           func() time.Time
}

func (j *Jobs) now() time.Time {
	if j.Now != nil {
		return j.Now().UTC()
	}
	return time.Now().UTC()
}

// Run performs both jobs at start and then every interval until ctx ends.
func (j *Jobs) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := j.Cleanup(ctx); err != nil {
			j.Log.Error("stats retention cleanup", "err", err)
		} else if n > 0 {
			j.Log.Info("stats retention cleanup", "deleted_rows", n)
		}
		if n, err := j.SendWeekly(ctx); err != nil {
			j.Log.Error("weekly stats email", "err", err, "sent", n)
		} else if n > 0 {
			j.Log.Info("weekly stats email", "sent", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Cleanup deletes aggregate rows older than RetentionDays.
func (j *Jobs) Cleanup(ctx context.Context) (int64, error) {
	days := j.RetentionDays
	if days < 1 {
		return 0, nil
	}
	cutoff := day(j.now()).AddDate(0, 0, -days)
	return j.Store.DeleteStatsBefore(ctx, cutoff)
}

// WeekStart returns the Monday (UTC) of the week containing t.
func WeekStart(t time.Time) time.Time {
	d := day(t.UTC())
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

func day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// SendWeekly emails last week's summary to every opted-in user not yet sent
// one this week. Running it again in the same week sends nothing.
func (j *Jobs) SendWeekly(ctx context.Context) (int, error) {
	week := WeekStart(j.now())
	users, err := j.Store.WeeklyDigestDue(ctx, week)
	if err != nil || len(users) == 0 {
		return 0, err
	}
	key, err := j.Store.SigningKey(ctx, UnsubscribeKeyName)
	if err != nil {
		return 0, err
	}
	from, to := week.AddDate(0, 0, -7), week.AddDate(0, 0, -1)
	sent := 0
	var firstErr error
	for _, u := range users {
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		msg, err := j.digest(ctx, u, key, from, to)
		if err != nil {
			firstErr = keepFirst(firstErr, err)
			continue
		}
		ok, err := j.Store.ClaimWeeklyDigest(ctx, u.ID, week)
		if err != nil || !ok {
			firstErr = keepFirst(firstErr, err)
			continue
		}
		if err := j.Mail.Send(ctx, msg); err != nil {
			j.Log.Error("send weekly stats email", "user", u.ID, "err", err)
			_ = j.Store.ReleaseWeeklyDigest(ctx, u.ID, week, u.SentWeek)
			firstErr = keepFirst(firstErr, err)
			continue
		}
		sent++
	}
	return sent, firstErr
}

func keepFirst(first, err error) error {
	if first != nil {
		return first
	}
	return err
}

func (j *Jobs) digest(ctx context.Context, u store.DigestUser, key []byte, from, to time.Time) (mail.Message, error) {
	pages, err := j.Store.UserPageTotals(ctx, u.ID, from, to)
	if err != nil {
		return mail.Message{}, err
	}
	referrers, countries, err := j.Store.UserTopNames(ctx, u.ID, from, to, 5)
	if err != nil {
		return mail.Message{}, err
	}
	unsub := j.BaseURL + "/unsubscribe/weekly-stats?token=" + url.QueryEscape(UnsubscribeToken(key, u.ID))
	period := from.Format("Jan 2") + " to " + to.Format("Jan 2, 2006")

	var b strings.Builder
	fmt.Fprintf(&b, "Your %s stats for %s (UTC).\n\n", j.AppName, period)
	for _, p := range pages {
		name := p.Title
		if name == "" {
			name = p.Slug
		}
		fmt.Fprintf(&b, "%s (%s/%s)\n", name, j.BaseURL, p.Slug)
		fmt.Fprintf(&b, "  Views: %d\n  Clicks: %d", p.Views, p.Clicks)
		if p.Views > 0 {
			fmt.Fprintf(&b, " (%s click-through)", Percent(ClickThrough(p.Clicks, p.Views)))
		}
		b.WriteString("\n\n")
	}
	if len(referrers) > 0 {
		b.WriteString("Top referrers: " + joinCounts(referrers) + "\n")
	}
	if len(countries) > 0 {
		b.WriteString("Top countries: " + joinCounts(countries) + "\n")
	}
	if len(referrers) > 0 || len(countries) > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Full stats are in the Stats tab of each page: %s/app/\n\n", j.BaseURL)
	fmt.Fprintf(&b, "You get this email because you turned on weekly stats. Stop them with one click:\n%s\n", unsub)

	return mail.Message{
		To:      u.Email,
		Subject: "Your " + j.AppName + " stats for " + period,
		Body:    b.String(),
		Headers: map[string]string{
			"List-Unsubscribe":      "<" + unsub + ">",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	}, nil
}

func joinCounts(list []store.NameCount) string {
	parts := make([]string, len(list))
	for i, nc := range list {
		parts[i] = fmt.Sprintf("%s %d", nc.Name, nc.Count)
	}
	return strings.Join(parts, ", ")
}

// Percent formats a ratio as a whole-number percentage, or one decimal below 10%.
func Percent(ratio float64) string {
	p := ratio * 100
	if p > 0 && p < 10 {
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}
