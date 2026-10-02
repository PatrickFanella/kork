package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"git.subcult.tv/subculture-collective/zelda/internal/analytics"
	"git.subcult.tv/subculture-collective/zelda/internal/db"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// testPool opens a second connection pool for direct SQL checks.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := db.Open(context.Background(), os.Getenv("ZELDA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// publishedPage signs up and verifies a user, creates a published page with
// two link blocks, and returns the logged-in client, page ID and block IDs.
func publishedPage(t *testing.T, e *env, email, slug string) (*client, string, []string) {
	t.Helper()
	c := e.client()
	if code := c.do("POST", "/api/auth/signup", map[string]any{"email": email, "password": "correct horse battery", "accept_terms": true}, nil); code != 201 {
		t.Fatalf("signup: %d", code)
	}
	tok := e.mail.tokenFor(t, email, "/app/verify")
	if code := c.do("POST", "/api/auth/verify", map[string]string{"token": tok}, nil); code != 200 {
		t.Fatalf("verify: %d", code)
	}
	var page pageResp
	if code := c.do("POST", "/api/pages", map[string]string{"slug": slug, "title": strings.ToUpper(slug)}, &page); code != 201 {
		t.Fatalf("create page: %d", code)
	}
	var ids []string
	for _, u := range []string{"https://example.com/a", "https://example.com/b"} {
		var b struct{ ID string }
		if code := c.do("POST", "/api/pages/"+page.ID+"/blocks", map[string]any{"type": "link", "data": map[string]any{"url": u, "title": u}}, &b); code != 201 {
			t.Fatalf("create block: %d", code)
		}
		ids = append(ids, b.ID)
	}
	if code := c.do("PATCH", "/api/pages/"+page.ID, map[string]any{"published": true}, nil); code != 200 {
		t.Fatalf("publish: %d", code)
	}
	return c, page.ID, ids
}

type statsBody struct {
	Days       int
	TotalViews int64 `json:"total_views"`
	Views      []store.DayCount
	Blocks     []struct {
		BlockID string `json:"block_id"`
		Count   int64
		CTR     float64
	}
	Referrers []store.NameCount
	Countries []store.NameCount
}

func counts(list []store.NameCount) map[string]int64 {
	m := map[string]int64{}
	for _, nc := range list {
		m[nc.Name] = nc.Count
	}
	return m
}

func TestAnalyticsCounting(t *testing.T) {
	e := setup(t)
	e.srv.cfg.TrustProxy = true // CF-IPCountry is honored only behind a trusted proxy
	alice, pageID, ids := publishedPage(t, e, "alice@example.com", "alice")
	anon := e.client()
	host := strings.TrimPrefix(e.ts.URL, "http://")

	view := func(h ...string) {
		t.Helper()
		if code, _, _ := anon.get("/alice", h...); code != 200 {
			t.Fatalf("view: %d", code)
		}
	}
	ua := "Mozilla/5.0"
	view("User-Agent", ua, "Referer", "https://www.Instagram.com/p/1", "CF-IPCountry", "US")
	view("User-Agent", ua, "Referer", "https://instagram.com/", "CF-IPCountry", "us")
	view("User-Agent", ua, "Referer", "https://t.co/abc", "CF-IPCountry", "DE")
	view("User-Agent", ua, "CF-IPCountry", "XX")
	view("User-Agent", ua, "Referer", "http://"+host+"/somewhere", "CF-IPCountry", "T1")
	view("User-Agent", "Googlebot/2.1", "Referer", "https://bing.com/", "CF-IPCountry", "US") // not counted

	click := func(id string) {
		req, _ := http.NewRequest("POST", e.ts.URL+"/t/"+id, nil)
		req.Header.Set("User-Agent", ua)
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 204 {
			t.Fatalf("click: %v %v", err, res)
		}
		res.Body.Close()
	}
	click(ids[0])
	click(ids[0])
	click(ids[1])

	var st statsBody
	if code := alice.do("GET", "/api/pages/"+pageID+"/stats?days=7", nil, &st); code != 200 {
		t.Fatalf("stats: %d", code)
	}
	if st.Days != 7 || st.TotalViews != 5 || len(st.Views) != 7 {
		t.Fatalf("views: %+v", st)
	}
	refs := counts(st.Referrers)
	if len(refs) != 3 || refs["instagram.com"] != 2 || refs["t.co"] != 1 || refs["direct"] != 2 {
		t.Errorf("referrers: %+v", st.Referrers)
	}
	if st.Referrers[0].Count < st.Referrers[len(st.Referrers)-1].Count {
		t.Errorf("referrers not sorted: %+v", st.Referrers)
	}
	cs := counts(st.Countries)
	if len(cs) != 3 || cs["US"] != 2 || cs["DE"] != 1 || cs["unknown"] != 2 {
		t.Errorf("countries: %+v", st.Countries)
	}
	ctr := map[string]float64{}
	for _, b := range st.Blocks {
		ctr[b.BlockID] = b.CTR
	}
	if ctr[ids[0]] != 0.4 || ctr[ids[1]] != 0.2 {
		t.Errorf("click-through: %+v", st.Blocks)
	}

	// The range is capped at MaxStatsDays.
	for _, q := range []string{"?days=90", "?days=abc", ""} {
		st = statsBody{}
		alice.do("GET", "/api/pages/"+pageID+"/stats"+q, nil, &st)
		if st.Days != MaxStatsDays || len(st.Views) != MaxStatsDays {
			t.Errorf("stats%s: days %d, %d view rows", q, st.Days, len(st.Views))
		}
	}

	// Distinct referrers per page per day are capped; the rest go to "other".
	ctx := context.Background()
	for i := range store.MaxReferrersPerDay + 10 {
		if err := e.srv.store.CountView(ctx, pageID, fmt.Sprintf("site%d.example", i), "US"); err != nil {
			t.Fatal(err)
		}
	}
	pool := testPool(t)
	var rowsToday, other, total int64
	if err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(SUM(count) FILTER (WHERE referrer = 'other'), 0), SUM(count)
		FROM stats_referrers WHERE page_id = $1 AND day = current_date`, pageID).Scan(&rowsToday, &other, &total); err != nil {
		t.Fatal(err)
	}
	if rowsToday != store.MaxReferrersPerDay+1 || other != 13 || total != 5+store.MaxReferrersPerDay+10 {
		t.Errorf("referrer cap: %d rows, other %d, total %d", rowsToday, other, total)
	}

	// Only aggregate columns exist in the stats tables.
	var cols []string
	rows, _ := pool.Query(ctx, `SELECT table_name || '.' || column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name IN ('stats_daily', 'stats_referrers', 'stats_countries') ORDER BY 1`)
	for rows.Next() {
		var c string
		rows.Scan(&c)
		cols = append(cols, c)
	}
	rows.Close()
	want := "stats_countries.count stats_countries.country stats_countries.day stats_countries.page_id " +
		"stats_daily.block_id stats_daily.count stats_daily.day stats_daily.page_id " +
		"stats_referrers.count stats_referrers.day stats_referrers.page_id stats_referrers.referrer"
	if got := strings.Join(cols, " "); got != want {
		t.Errorf("stats columns:\n got %s\nwant %s", got, want)
	}
}

func TestStatsRetention(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	u, err := e.srv.store.CreateUser(ctx, "r@example.com", "x")
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.srv.store.CreatePage(ctx, u.ID, "retained", "", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	pool := testPool(t)
	// now = 2026-10-02; with 400 days kept, the cutoff is 2025-08-28.
	for _, day := range []string{"2025-08-26", "2025-08-27", "2025-08-28", "2026-10-01"} {
		for _, q := range []string{
			`INSERT INTO stats_daily (page_id, block_id, day, count) VALUES ($1, '` + store.ZeroBlockID + `', $2, 1)`,
			`INSERT INTO stats_referrers (page_id, day, referrer, count) VALUES ($1, $2, 'direct', 1)`,
			`INSERT INTO stats_countries (page_id, day, country, count) VALUES ($1, $2, 'US', 1)`,
		} {
			if _, err := pool.Exec(ctx, q, p.ID, day); err != nil {
				t.Fatal(err)
			}
		}
	}
	jobs := &analytics.Jobs{Store: e.srv.store, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), RetentionDays: 400,
		Now: func() time.Time { return time.Date(2026, 10, 2, 13, 0, 0, 0, time.UTC) }}
	n, err := jobs.Cleanup(ctx)
	if err != nil || n != 6 {
		t.Fatalf("cleanup deleted %d rows, err %v; want 6", n, err)
	}
	for _, table := range []string{"stats_daily", "stats_referrers", "stats_countries"} {
		var oldest string
		var rows int
		pool.QueryRow(ctx, `SELECT to_char(min(day), 'YYYY-MM-DD'), count(*) FROM `+table).Scan(&oldest, &rows)
		if oldest != "2025-08-28" || rows != 2 {
			t.Errorf("%s: oldest %s, %d rows", table, oldest, rows)
		}
	}
	if n, _ := jobs.Cleanup(ctx); n != 0 {
		t.Errorf("second cleanup deleted %d rows", n)
	}
}

type fakeMailer struct {
	mu   sync.Mutex
	msgs []mail.Message
	fail bool
}

func (f *fakeMailer) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("smtp down")
	}
	f.msgs = append(f.msgs, m)
	return nil
}

func TestWeeklyStatsEmail(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	alice, pageID, ids := publishedPage(t, e, "alice@example.com", "alice")

	// Unverified users can't opt in.
	bob := e.client()
	bob.do("POST", "/api/auth/signup", map[string]any{"email": "bob@example.com", "password": "correct horse battery", "accept_terms": true}, nil)
	var ae apiErr
	if code := bob.do("PATCH", "/api/account/settings", map[string]bool{"weekly_stats_email": true}, &ae); code != 403 || ae.Error.Code != "email_unverified" {
		t.Fatalf("unverified opt-in: %d %+v", code, ae)
	}
	if code := e.client().do("GET", "/api/account/settings", nil, &ae); code != 401 {
		t.Fatalf("anon settings: %d", code)
	}

	var set store.AccountSettings
	if code := alice.do("GET", "/api/account/settings", nil, &set); code != 200 || set.WeeklyStatsEmail {
		t.Fatalf("default settings: %d %+v", code, set)
	}
	if code := alice.do("PATCH", "/api/account/settings", map[string]bool{"weekly_stats_email": true}, &set); code != 200 || !set.WeeklyStatsEmail {
		t.Fatalf("opt in: %d %+v", code, set)
	}

	m := &fakeMailer{}
	now := time.Now().UTC()
	jobs := &analytics.Jobs{Store: e.srv.store, Mail: m, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		BaseURL: e.ts.URL, AppName: "Kork", Now: func() time.Time { return now }}

	// Opting in this week means the first email comes next week.
	if n, err := jobs.SendWeekly(ctx); n != 0 || err != nil {
		t.Fatalf("same-week send: %d %v", n, err)
	}

	// Stats for this week, read by next week's run.
	pool := testPool(t)
	week := analytics.WeekStart(now)
	for i, q := range []struct {
		block string
		count int
	}{{store.ZeroBlockID, 7}, {store.ZeroBlockID, 3}, {ids[0], 2}, {ids[1], 1}} {
		day := week.AddDate(0, 0, i%2).Format(time.DateOnly)
		if _, err := pool.Exec(ctx, `INSERT INTO stats_daily (page_id, block_id, day, count) VALUES ($1, $2, $3, $4)
			ON CONFLICT (page_id, block_id, day) DO UPDATE SET count = stats_daily.count + EXCLUDED.count`, pageID, q.block, day, q.count); err != nil {
			t.Fatal(err)
		}
	}
	pool.Exec(ctx, `INSERT INTO stats_referrers (page_id, day, referrer, count) VALUES ($1, $2, 'instagram.com', 6)`, pageID, week)
	pool.Exec(ctx, `INSERT INTO stats_countries (page_id, day, country, count) VALUES ($1, $2, 'DE', 4)`, pageID, week)
	// Last week's rows (before the reported range) must not be counted.
	pool.Exec(ctx, `INSERT INTO stats_daily (page_id, block_id, day, count) VALUES ($1, $2, $3, 100)`, pageID, store.ZeroBlockID, week.AddDate(0, 0, -1))

	now = now.AddDate(0, 0, 7)
	n, err := jobs.SendWeekly(ctx)
	if n != 1 || err != nil || len(m.msgs) != 1 {
		t.Fatalf("weekly send: %d %v %d", n, err, len(m.msgs))
	}
	msg := m.msgs[0]
	for _, want := range []string{"Views: 10", "Clicks: 3 (30% click-through)", "Top referrers: instagram.com 6",
		"Top countries: DE 4", e.ts.URL + "/alice", "/unsubscribe/weekly-stats?token="} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("email body missing %q:\n%s", want, msg.Body)
		}
	}
	if msg.To != "alice@example.com" || msg.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" ||
		!strings.HasPrefix(msg.Headers["List-Unsubscribe"], "<"+e.ts.URL+"/unsubscribe/weekly-stats?token=") {
		t.Errorf("email headers: %+v %+v", msg.To, msg.Headers)
	}

	// Idempotent: a second run the same week sends nothing.
	if n, err := jobs.SendWeekly(ctx); n != 0 || err != nil || len(m.msgs) != 1 {
		t.Fatalf("repeat send: %d %v", n, err)
	}

	// A failed send is retried on the next run.
	now = now.AddDate(0, 0, 7)
	m.fail = true
	if n, err := jobs.SendWeekly(ctx); n != 0 || err == nil {
		t.Fatalf("failing send: %d %v", n, err)
	}
	m.fail = false
	if n, err := jobs.SendWeekly(ctx); n != 1 || err != nil || len(m.msgs) != 2 {
		t.Fatalf("retry send: %d %v", n, err)
	}

	// The unsubscribe link turns the email off.
	link := regexp.MustCompile(`http\S+/unsubscribe/weekly-stats\?token=\S+`).FindString(m.msgs[1].Body)
	anon := e.client()
	if code, body, _ := anon.get(strings.TrimPrefix(link, e.ts.URL)); code != 200 || !strings.Contains(body, "Weekly stats emails are off") {
		t.Fatalf("unsubscribe: %d %s", code, body)
	}
	alice.do("GET", "/api/account/settings", nil, &set)
	if set.WeeklyStatsEmail {
		t.Fatal("still subscribed after unsubscribe")
	}
	now = now.AddDate(0, 0, 7)
	if n, _ := jobs.SendWeekly(ctx); n != 0 {
		t.Fatalf("sent after unsubscribe: %d", n)
	}

	// Tampered tokens are rejected; one-click POST works.
	u, _ := url.Parse(link)
	tok := u.Query().Get("token")
	if code, _, _ := anon.get("/unsubscribe/weekly-stats?token=" + url.QueryEscape(tok[:len(tok)-2]+"AA")); code != 400 {
		t.Fatalf("tampered token: %d", code)
	}
	alice.do("PATCH", "/api/account/settings", map[string]bool{"weekly_stats_email": true}, &set)
	res, err := http.Post(link, "application/x-www-form-urlencoded", strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil || res.StatusCode != 204 {
		t.Fatalf("one-click unsubscribe: %v %v", err, res)
	}
	res.Body.Close()
	alice.do("GET", "/api/account/settings", nil, &set)
	if set.WeeklyStatsEmail {
		t.Fatal("still subscribed after one-click unsubscribe")
	}
}

func TestCountryNeedsTrustedProxy(t *testing.T) {
	e := setup(t)
	alice, pageID, _ := publishedPage(t, e, "carol@example.com", "carol")
	req, _ := http.NewRequest("GET", e.ts.URL+"/carol", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("CF-IPCountry", "FR")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var st struct {
		Countries []struct {
			Name  string `json:"name"`
			Count int64  `json:"count"`
		} `json:"countries"`
	}
	alice.do("GET", "/api/pages/"+pageID+"/stats?days=7", nil, &st)
	unknown := false
	for _, c := range st.Countries {
		if c.Name == "FR" {
			t.Fatalf("forged country counted without a trusted proxy: %+v", st.Countries)
		}
		unknown = unknown || (c.Name == "unknown" && c.Count >= 1)
	}
	if !unknown {
		t.Fatalf("view should count as unknown country: %+v", st.Countries)
	}
}
