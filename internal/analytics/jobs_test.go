package analytics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/db"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore opens the test database in its own schema, so it doesn't
// collide with other packages' tests that reset the public schema.
func testStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("ZELDA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ZELDA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	const schema = "analytics_jobs_test"
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	admin.Close()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	pool, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return store.New(pool), pool
}

type captured struct {
	mu   sync.Mutex
	msgs []mail.Message
	fail bool
}

func (c *captured) Send(_ context.Context, m mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("relay down")
	}
	c.msgs = append(c.msgs, m)
	return nil
}

func TestWeeklyEmailAndCleanup(t *testing.T) {
	st, pool := testStore(t)
	ctx := context.Background()
	// Wednesday; the email covers Monday 2026-09-28 to Sunday 2026-10-04.
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	u, err := st.CreateUser(ctx, "alice@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET email_verified_at = now() WHERE id = $1`, u.ID); err != nil {
		t.Fatal(err)
	}
	p, err := st.CreatePage(ctx, u.ID, "alice", "Alice's prints", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	// Opting in during an earlier week.
	if err := st.SetWeeklyStatsEmail(ctx, u.ID, true, WeekStart(now).AddDate(0, 0, -14)); err != nil {
		t.Fatal(err)
	}
	const view = "00000000-0000-0000-0000-000000000000"
	const block = "11111111-1111-1111-1111-111111111111"
	for _, r := range []struct {
		block string
		day   string
		n     int
	}{{view, "2026-09-29", 6}, {view, "2026-10-03", 4}, {block, "2026-10-01", 3}, {view, "2026-10-06", 99}, {view, "2025-01-01", 5}} {
		if _, err := pool.Exec(ctx, `INSERT INTO stats_daily (page_id, block_id, day, count) VALUES ($1, $2, $3, $4)`, p.ID, r.block, r.day, r.n); err != nil {
			t.Fatal(err)
		}
	}
	box := &captured{}
	j := &Jobs{Store: st, Mail: box, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), BaseURL: "https://kork.example",
		AppName: "Kork", RetentionDays: 400, Now: func() time.Time { return now }}

	// Retention deletes only rows older than 400 days.
	if n, err := j.Cleanup(ctx); err != nil || n != 1 {
		t.Fatalf("cleanup: %d %v", n, err)
	}

	// The weekly email covers last week only, once.
	if n, err := j.SendWeekly(ctx); err != nil || n != 1 || len(box.msgs) != 1 {
		t.Fatalf("send: %d %v %d", n, err, len(box.msgs))
	}
	m := box.msgs[0]
	for _, want := range []string{"Alice's prints (https://kork.example/alice)", "Views: 10", "Clicks: 3", "(30% click-through)", "Sep 28 to Oct 4, 2026"} {
		if !strings.Contains(m.Body+m.Subject, want) {
			t.Errorf("email is missing %q:\n%s\n%s", want, m.Subject, m.Body)
		}
	}
	if !strings.HasPrefix(m.Headers["List-Unsubscribe"], "<https://kork.example/unsubscribe/weekly-stats?token=") ||
		m.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
		t.Fatalf("unsubscribe headers: %v", m.Headers)
	}
	if n, err := j.SendWeekly(ctx); err != nil || n != 0 {
		t.Fatalf("second run in the same week: %d %v", n, err)
	}

	// A failed send releases the claim, so the next run retries.
	now = now.AddDate(0, 0, 7)
	box.fail = true
	if n, err := j.SendWeekly(ctx); err == nil || n != 0 {
		t.Fatalf("failed send: %d %v", n, err)
	}
	box.fail = false
	if n, err := j.SendWeekly(ctx); err != nil || n != 1 {
		t.Fatalf("retry: %d %v", n, err)
	}
}

// Run does both jobs at once and stops when its context ends.
func TestRunStopsWithContext(t *testing.T) {
	st, _ := testStore(t)
	j := &Jobs{Store: st, Mail: &captured{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), RetentionDays: 30}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		j.Run(ctx, time.Hour)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't stop")
	}
}

func TestPercent(t *testing.T) {
	for ratio, want := range map[float64]string{0: "0%", 0.05: "5.0%", 0.3: "30%", 1: "100%"} {
		if got := Percent(ratio); got != want {
			t.Errorf("Percent(%v) = %s, want %s", ratio, got, want)
		}
	}
}
