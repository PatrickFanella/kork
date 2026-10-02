package store

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// MaxReferrerLen caps a stored referrer label.
	MaxReferrerLen = 100
	// MaxReferrersPerDay caps distinct referrers per page per day; later ones
	// are counted under OtherReferrer.
	MaxReferrersPerDay = 200
	OtherReferrer      = "other"
)

// CountView records one page view with its referrer domain and country.
// referrer and country must already be normalized (see internal/analytics).
func (s *Store) CountView(ctx context.Context, pageID, referrer, country string) error {
	if len(referrer) > MaxReferrerLen {
		referrer = referrer[:MaxReferrerLen]
	}
	if len(country) > 7 {
		country = "unknown"
	}
	b := &pgx.Batch{}
	b.Queue(`INSERT INTO stats_daily (page_id, block_id, day, count) VALUES ($1, $2, current_date, 1)
		ON CONFLICT (page_id, block_id, day) DO UPDATE SET count = stats_daily.count + 1`, pageID, ZeroBlockID)
	// A new referrer only gets its own row while the page has fewer than
	// MaxReferrersPerDay rows that day; otherwise it is folded into "other".
	b.Queue(`INSERT INTO stats_referrers (page_id, day, referrer, count)
		SELECT $1, current_date, CASE
			WHEN EXISTS (SELECT 1 FROM stats_referrers WHERE page_id = $1 AND day = current_date AND referrer = $2) THEN $2
			WHEN (SELECT count(*) FROM stats_referrers WHERE page_id = $1 AND day = current_date) < $3 THEN $2
			ELSE $4 END, 1
		ON CONFLICT (page_id, day, referrer) DO UPDATE SET count = stats_referrers.count + 1`,
		pageID, referrer, MaxReferrersPerDay, OtherReferrer)
	b.Queue(`INSERT INTO stats_countries (page_id, day, country, count) VALUES ($1, current_date, $2, 1)
		ON CONFLICT (page_id, day, country) DO UPDATE SET count = stats_countries.count + 1`, pageID, country)
	return s.pool.SendBatch(ctx, b).Close()
}

// NameCount is one row of a referrer or country breakdown.
type NameCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// Breakdown returns the top referrers and countries for the last days.
func (s *Store) Breakdown(ctx context.Context, pageID string, days, limit int) (referrers, countries []NameCount, err error) {
	referrers, err = s.topNames(ctx, `SELECT referrer, SUM(count) FROM stats_referrers
		WHERE page_id = $1 AND day > current_date - $2::int GROUP BY referrer ORDER BY 2 DESC, 1 LIMIT $3`, pageID, days, limit)
	if err != nil {
		return nil, nil, err
	}
	countries, err = s.topNames(ctx, `SELECT country, SUM(count) FROM stats_countries
		WHERE page_id = $1 AND day > current_date - $2::int GROUP BY country ORDER BY 2 DESC, 1 LIMIT $3`, pageID, days, limit)
	return referrers, countries, err
}

func (s *Store) topNames(ctx context.Context, sql string, args ...any) ([]NameCount, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NameCount{}
	for rows.Next() {
		var nc NameCount
		if err := rows.Scan(&nc.Name, &nc.Count); err != nil {
			return nil, err
		}
		out = append(out, nc)
	}
	return out, rows.Err()
}

// DeleteStatsBefore removes aggregate rows for days before cutoff and returns
// how many rows were deleted.
func (s *Store) DeleteStatsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64
	for _, table := range []string{"stats_daily", "stats_referrers", "stats_countries"} {
		tag, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE day < $1::date`, cutoff.Format(time.DateOnly))
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// ---- weekly stats email ----

// AccountSettings are per-user preferences.
type AccountSettings struct {
	WeeklyStatsEmail bool `json:"weekly_stats_email"`
}

func (s *Store) AccountSettings(ctx context.Context, userID string) (*AccountSettings, error) {
	var a AccountSettings
	err := s.pool.QueryRow(ctx, `SELECT weekly_stats_email FROM users WHERE id = $1`, userID).Scan(&a.WeeklyStatsEmail)
	if err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}

// SetWeeklyStatsEmail switches the weekly email on or off. Switching it on
// marks week (a Monday) as already sent, so the first email covers a full
// week after opting in.
func (s *Store) SetWeeklyStatsEmail(ctx context.Context, userID string, on bool, week time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET weekly_stats_email = $2,
		weekly_stats_sent_week = CASE WHEN $2 AND NOT weekly_stats_email
			THEN GREATEST(weekly_stats_sent_week, $3::date) ELSE weekly_stats_sent_week END
		WHERE id = $1`, userID, on, week.Format(time.DateOnly))
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DigestUser is a user due a weekly stats email.
type DigestUser struct {
	ID       string
	Email    string
	SentWeek *time.Time
}

// WeeklyDigestDue lists opted-in, verified, active users with at least one
// page who have not been sent the email for week (a Monday).
func (s *Store) WeeklyDigestDue(ctx context.Context, week time.Time) ([]DigestUser, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.email, u.weekly_stats_sent_week FROM users u
		WHERE u.weekly_stats_email AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
		AND (u.weekly_stats_sent_week IS NULL OR u.weekly_stats_sent_week < $1::date)
		AND EXISTS (SELECT 1 FROM pages p WHERE p.user_id = u.id)
		ORDER BY u.created_at`, week.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DigestUser
	for rows.Next() {
		var d DigestUser
		if err := rows.Scan(&d.ID, &d.Email, &d.SentWeek); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClaimWeeklyDigest marks week as sent for the user if it was not already,
// and reports whether this call made the claim. Concurrent senders therefore
// send at most one email per user per week.
func (s *Store) ClaimWeeklyDigest(ctx context.Context, userID string, week time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET weekly_stats_sent_week = $2::date
		WHERE id = $1 AND weekly_stats_email
		AND (weekly_stats_sent_week IS NULL OR weekly_stats_sent_week < $2::date)`, userID, week.Format(time.DateOnly))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ReleaseWeeklyDigest undoes a claim after a failed send so a later run retries.
func (s *Store) ReleaseWeeklyDigest(ctx context.Context, userID string, week time.Time, prev *time.Time) error {
	var p any
	if prev != nil {
		p = prev.Format(time.DateOnly)
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET weekly_stats_sent_week = $3::date
		WHERE id = $1 AND weekly_stats_sent_week = $2::date`, userID, week.Format(time.DateOnly), p)
	return err
}

// PageWeek is one page's totals over a date range.
type PageWeek struct {
	ID     string
	Slug   string
	Title  string
	Views  int64
	Clicks int64
}

// UserPageTotals returns views and clicks per page of a user for the
// inclusive date range from..to.
func (s *Store) UserPageTotals(ctx context.Context, userID string, from, to time.Time) ([]PageWeek, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id, p.slug, p.title,
		COALESCE(SUM(sd.count) FILTER (WHERE sd.block_id = $4), 0),
		COALESCE(SUM(sd.count) FILTER (WHERE sd.block_id <> $4), 0)
		FROM pages p LEFT JOIN stats_daily sd ON sd.page_id = p.id AND sd.day BETWEEN $2::date AND $3::date
		WHERE p.user_id = $1 GROUP BY p.id ORDER BY p.created_at`,
		userID, from.Format(time.DateOnly), to.Format(time.DateOnly), ZeroBlockID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PageWeek
	for rows.Next() {
		var pw PageWeek
		if err := rows.Scan(&pw.ID, &pw.Slug, &pw.Title, &pw.Views, &pw.Clicks); err != nil {
			return nil, err
		}
		out = append(out, pw)
	}
	return out, rows.Err()
}

// UserTopNames returns the top referrers and countries across a user's pages
// for the inclusive date range from..to.
func (s *Store) UserTopNames(ctx context.Context, userID string, from, to time.Time, limit int) (referrers, countries []NameCount, err error) {
	f, t := from.Format(time.DateOnly), to.Format(time.DateOnly)
	referrers, err = s.topNames(ctx, `SELECT sr.referrer, SUM(sr.count) FROM stats_referrers sr
		JOIN pages p ON p.id = sr.page_id WHERE p.user_id = $1 AND sr.day BETWEEN $2::date AND $3::date
		GROUP BY sr.referrer ORDER BY 2 DESC, 1 LIMIT $4`, userID, f, t, limit)
	if err != nil {
		return nil, nil, err
	}
	countries, err = s.topNames(ctx, `SELECT sc.country, SUM(sc.count) FROM stats_countries sc
		JOIN pages p ON p.id = sc.page_id WHERE p.user_id = $1 AND sc.day BETWEEN $2::date AND $3::date
		GROUP BY sc.country ORDER BY 2 DESC, 1 LIMIT $4`, userID, f, t, limit)
	return referrers, countries, err
}

// SigningKey returns the named 32-byte server key, creating it on first use.
func (s *Store) SigningKey(ctx context.Context, name string) ([]byte, error) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO app_keys (name, key) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING`, name, k); err != nil {
		return nil, err
	}
	var key []byte
	err := s.pool.QueryRow(ctx, `SELECT key FROM app_keys WHERE name = $1`, name).Scan(&key)
	return key, err
}
