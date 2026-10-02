package server

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/db"
	"git.subcult.tv/subculture-collective/zelda/internal/screen"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

func (c *client) signupVerified(email string) store.User {
	c.e.t.Helper()
	var me struct{ User store.User }
	if code := c.do("POST", "/api/auth/signup", map[string]any{"email": email, "password": "correct horse battery", "accept_terms": true}, &me); code != 201 {
		c.e.t.Fatalf("signup %s: %d", email, code)
	}
	tok := c.e.mail.tokenFor(c.e.t, email, "/app/verify")
	if code := c.do("POST", "/api/auth/verify", map[string]string{"token": tok}, &me); code != 200 {
		c.e.t.Fatalf("verify %s: %d", email, code)
	}
	return me.User
}

func (c *client) newPage(slug string) string {
	c.e.t.Helper()
	var p pageResp
	if code := c.do("POST", "/api/pages", map[string]string{"slug": slug}, &p); code != 201 {
		c.e.t.Fatalf("create page %s: %d", slug, code)
	}
	return p.ID
}

func (c *client) addBlock(pageID, typ string, data map[string]any) (int, apiErr) {
	c.e.t.Helper()
	var ae apiErr
	code := c.do("POST", "/api/pages/"+pageID+"/blocks", map[string]any{"type": typ, "data": data}, &ae)
	return code, ae
}

func TestScreeningAndPublishGate(t *testing.T) {
	e := setup(t)
	pool, err := db.Open(context.Background(), os.Getenv("ZELDA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fake := &screen.Fake{}
	fake.Flag("phish.example", "SOCIAL_ENGINEERING")
	e.srv.SetScreener(fake)
	e.srv.cfg.PublishMinAge = 24 * time.Hour

	admin := e.client()
	admin.signupVerified("alice@example.com")
	bob := e.client()
	bobUser := bob.signupVerified("bob@example.com")
	pid := bob.newPage("bobpage")

	// Known-bad destinations are rejected with a field error, including subdomains.
	if code, ae := bob.addBlock(pid, "link", map[string]any{"url": "https://phish.example/login", "title": "Log in"}); code != 422 ||
		ae.Error.Code != "blocked_destination" || ae.Error.Field != "url" || !strings.Contains(ae.Error.Message, "phish.example") {
		t.Fatalf("bad link: %d %+v", code, ae)
	}
	if code, ae := bob.addBlock(pid, "service", map[string]any{"service": "website", "value": "https://www.phish.example"}); code != 422 ||
		ae.Error.Code != "blocked_destination" || ae.Error.Field != "value" {
		t.Fatalf("bad service: %d %+v", code, ae)
	}

	// Good destinations pass, and verdicts are cached.
	var blk struct{ ID string }
	if code := bob.do("POST", "/api/pages/"+pid+"/blocks", map[string]any{"type": "link", "data": map[string]any{"url": "https://later.example/", "title": "Shop"}}, &blk); code != 201 {
		t.Fatalf("good link: %d", code)
	}
	calls := fake.CallCount()
	if code := bob.do("PATCH", "/api/pages/"+pid+"/blocks/"+blk.ID, map[string]any{"data": map[string]any{"url": "https://later.example/", "title": "My shop"}}, nil); code != 200 {
		t.Fatalf("update link: %d", code)
	}
	if fake.CallCount() != calls {
		t.Fatalf("cached verdict not used: %d calls, was %d", fake.CallCount(), calls)
	}
	var ae apiErr
	if code := bob.do("PATCH", "/api/pages/"+pid+"/blocks/"+blk.ID, map[string]any{"data": map[string]any{"url": "https://phish.example/", "title": "x"}}, &ae); code != 422 || ae.Error.Code != "blocked_destination" {
		t.Fatalf("update to bad link: %d %+v", code, ae)
	}

	// A brand-new account can't publish yet; the message says when it can.
	if code := bob.do("PATCH", "/api/pages/"+pid, map[string]any{"published": true}, &ae); code != 403 ||
		ae.Error.Code != "account_too_new" || !strings.Contains(ae.Error.Message, "New accounts can publish from") {
		t.Fatalf("new account published: %d %+v", code, ae)
	}

	// Admins bypass the gate.
	apid := admin.newPage("adminpage")
	var page pageResp
	if code := admin.do("PATCH", "/api/pages/"+apid, map[string]any{"published": true}, &page); code != 200 || !page.Published {
		t.Fatalf("admin publish: %d", code)
	}

	// Older accounts are unaffected.
	carol := e.client()
	carolUser := carol.signupVerified("carol@example.com")
	if _, err := pool.Exec(context.Background(), `UPDATE users SET created_at = now() - interval '2 days' WHERE id = $1`, carolUser.ID); err != nil {
		t.Fatal(err)
	}
	cpid := carol.newPage("carolpage")
	if code := carol.do("PATCH", "/api/pages/"+cpid, map[string]any{"published": true}, &page); code != 200 {
		t.Fatalf("old account publish: %d", code)
	}

	// The admin sees Bob waiting and approves him early.
	var pending []store.PendingUser
	if code := bob.do("GET", "/api/admin/users/pending", nil, &ae); code != 403 {
		t.Fatalf("non-admin pending list: %d", code)
	}
	admin.do("GET", "/api/admin/users/pending", nil, &pending)
	if len(pending) != 1 || pending[0].ID != bobUser.ID || len(pending[0].Pages) != 1 || pending[0].Pages[0] != "bobpage" {
		t.Fatalf("pending: %+v", pending)
	}
	if code := bob.do("POST", "/api/admin/users/"+bobUser.ID+"/trust", nil, &ae); code != 403 {
		t.Fatalf("non-admin trust: %d", code)
	}
	if code := admin.do("POST", "/api/admin/users/"+bobUser.ID+"/trust", nil, nil); code != 204 {
		t.Fatalf("trust: %d", code)
	}
	if code := admin.do("POST", "/api/admin/users/00000000-0000-0000-0000-000000000001/trust", nil, &ae); code != 404 {
		t.Fatalf("trust unknown: %d", code)
	}
	if code := bob.do("PATCH", "/api/pages/"+pid, map[string]any{"published": true}, &page); code != 200 || !page.Published {
		t.Fatalf("approved publish: %d %+v", code, ae)
	}
	admin.do("GET", "/api/admin/users/pending", nil, &pending)
	if len(pending) != 0 {
		t.Fatalf("pending after trust: %+v", pending)
	}
	anon := e.client()
	if code, _, _ := anon.get("/bobpage"); code != 200 {
		t.Fatalf("published page: %d", code)
	}

	// The destination turns bad later: the re-check unpublishes the page and reports it.
	if _, err := pool.Exec(context.Background(), `DELETE FROM screen_verdicts`); err != nil {
		t.Fatal(err)
	}
	fake.Flag("later.example", "MALWARE")
	checked, unpublished, err := e.srv.RecheckOnce(context.Background())
	if err != nil || checked != 3 || unpublished != 1 {
		t.Fatalf("recheck: checked=%d unpublished=%d err=%v", checked, unpublished, err)
	}
	if code, _, _ := anon.get("/bobpage"); code != 404 {
		t.Fatalf("bad page still live: %d", code)
	}
	if code, _, _ := anon.get("/carolpage"); code != 200 {
		t.Fatalf("clean page taken down: %d", code)
	}
	var reports []store.Report
	admin.do("GET", "/api/admin/reports", nil, &reports)
	if len(reports) != 1 || reports[0].Reason != "phishing" || reports[0].Slug != "bobpage" || !strings.Contains(reports[0].Details, "https://later.example/") {
		t.Fatalf("reports: %+v", reports)
	}
	if !e.mail.sentTo(t, "bob@example.com", "was unpublished") {
		t.Fatal("owner not emailed")
	}
	// A second pass does not report the page again.
	if _, unpublished, err := e.srv.RecheckOnce(context.Background()); err != nil || unpublished != 0 {
		t.Fatalf("second recheck: %d %v", unpublished, err)
	}

	// Republishing is refused until the link is fixed.
	if code := bob.do("PATCH", "/api/pages/"+pid, map[string]any{"published": true}, &ae); code != 422 || ae.Error.Code != "blocked_destination" || ae.Error.Field != "published" {
		t.Fatalf("republish: %d %+v", code, ae)
	}

	// If the provider fails, unflagged links are allowed and not cached.
	fake.SetErr(errors.New("provider down"))
	if code, ae := bob.addBlock(pid, "link", map[string]any{"url": "https://unknown.example/", "title": "x"}); code != 201 {
		t.Fatalf("fail open: %d %+v", code, ae)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM screen_verdicts WHERE url = 'https://unknown.example/'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("cached verdict after provider error: %d %v", n, err)
	}

	// Visitors can report phishing too.
	if code := anon.do("POST", "/api/report", map[string]string{"slug": "carolpage", "reason": "phishing"}, nil); code != 204 {
		t.Fatalf("phishing report: %d", code)
	}
}

func (c *captureMail) sentTo(t *testing.T, to, subject string) bool {
	t.Helper()
	for i := 0; i < 200; i++ {
		c.mu.Lock()
		for _, m := range c.msgs {
			if m.To == to && strings.Contains(m.Subject, subject) {
				c.mu.Unlock()
				return true
			}
		}
		c.mu.Unlock()
		waitABit()
	}
	return false
}
