package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/plans"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// setPlan stores a plan for the signed-in user directly (test setup).
func (c *client) setPlan(tier plans.Tier, expires *time.Time) {
	c.e.t.Helper()
	var me struct{ User store.User }
	if code := c.do("GET", "/api/auth/me", nil, &me); code != 200 {
		c.e.t.Fatalf("me: %d", code)
	}
	if err := c.e.srv.store.SetPlan(context.Background(), me.User.ID, plans.Account{Plan: tier, Source: plans.Manual, ExpiresAt: expires}); err != nil {
		c.e.t.Fatal(err)
	}
}

type upgradeErr struct {
	Error struct{ Code, Message, Field, Feature string }
}

type meResp struct {
	User store.User
	Plan struct {
		Plan              string          `json:"plan"`
		StoredPlan        string          `json:"stored_plan"`
		Source            string          `json:"source"`
		ExpiresAt         *time.Time      `json:"expires_at"`
		DomainsGraceUntil *time.Time      `json:"domains_grace_until"`
		Pages             int             `json:"pages"`
		StatsDays         int             `json:"stats_days"`
		CustomDomains     int             `json:"custom_domains"`
		Features          map[string]bool `json:"features"`
		Pro               struct {
			Pages int `json:"pages"`
		} `json:"pro"`
	}
}

func (c *client) me() meResp {
	c.e.t.Helper()
	var m meResp
	if code := c.do("GET", "/api/auth/me", nil, &m); code != 200 {
		c.e.t.Fatalf("me: %d", code)
	}
	return m
}

func TestPlansAndFeatureGates(t *testing.T) {
	e := setup(t)
	e.srv.cfg.FreePageLimit = 2
	e.srv.cfg.StatsRetentionDays = 400
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	e.srv.SetClock(clock.Now)
	e.srv.SetResolver(fakeDNS{
		txt: map[string][]string{},
		ip:  map[string]string{"edge.example.net": "203.0.113.7", "links.bob.example": "203.0.113.7"},
	})
	anon := e.client()

	admin := e.client()
	admin.signupVerified("alice@example.com") // listed in AdminEmails
	bob := e.client()
	bobUser := bob.signupVerified("bob@example.com")

	// ---- Free ----
	m := bob.me()
	if m.Plan.Plan != "free" || m.Plan.StoredPlan != "free" || m.Plan.Pages != 2 || m.Plan.StatsDays != 30 ||
		m.Plan.CustomDomains != 0 || m.Plan.Features["custom_domains"] || m.Plan.Features["remove_branding"] ||
		!m.Plan.Features["scheduled_links"] || m.Plan.Pro.Pages != 25 || m.Plan.ExpiresAt != nil {
		t.Fatalf("free plan: %+v", m.Plan)
	}
	pageID := bob.newPage("bobpage")
	bob.newPage("bobpage2")
	var ue upgradeErr
	if code := bob.do("POST", "/api/pages", map[string]string{"slug": "bobpage3"}, &ue); code != 403 ||
		ue.Error.Code != "upgrade_required" || ue.Error.Feature != "pages" || !strings.Contains(ue.Error.Message, "2 pages") {
		t.Fatalf("free page limit: %d %+v", code, ue)
	}
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &ue); code != 403 ||
		ue.Error.Code != "upgrade_required" || ue.Error.Feature != "custom_domains" || ue.Error.Field != "hostname" {
		t.Fatalf("free domain: %d %+v", code, ue)
	}
	if code := bob.do("PATCH", "/api/pages/"+pageID, map[string]any{"theme": map[string]any{"preset": "paper", "hide_branding": true}}, &ue); code != 403 ||
		ue.Error.Feature != "remove_branding" || ue.Error.Field != "hide_branding" {
		t.Fatalf("free hide branding: %d %+v", code, ue)
	}
	var st statsBody
	bob.do("GET", "/api/pages/"+pageID+"/stats?days=365", nil, &st)
	if st.Days != 30 || len(st.Views) != 30 {
		t.Fatalf("free stats range: %d", st.Days)
	}
	// Scheduled links are free for now.
	if code := bob.do("POST", "/api/pages/"+pageID+"/blocks", map[string]any{"type": "heading", "data": map[string]any{"text": "Soon"},
		"starts_at": "2026-10-03T00:00:00Z"}, nil); code != 201 {
		t.Fatalf("free schedule: %d", code)
	}
	if code := bob.do("PATCH", "/api/pages/"+pageID, map[string]any{"published": true}, nil); code != 200 {
		t.Fatalf("publish: %d", code)
	}

	// ---- Admin grants Pro ----
	if code := bob.do("GET", "/api/admin/users", nil, &ue); code != 403 {
		t.Fatalf("non-admin list: %d", code)
	}
	if code := bob.do("POST", "/api/admin/users/"+bobUser.ID+"/plan", map[string]any{"plan": "pro"}, &ue); code != 403 {
		t.Fatalf("non-admin grant: %d", code)
	}
	var users []struct {
		ID, Email, Plan string
		EffectivePlan   string `json:"effective_plan"`
		Pages           int
	}
	if code := admin.do("GET", "/api/admin/users?q=BOB@", nil, &users); code != 200 || len(users) != 1 ||
		users[0].ID != bobUser.ID || users[0].Plan != "free" || users[0].Pages != 2 {
		t.Fatalf("admin search: %d %+v", code, users)
	}
	if code := admin.do("GET", "/api/admin/users?q=%25", nil, &users); code != 200 || len(users) != 0 {
		t.Fatalf("LIKE wildcards must be literal: %+v", users)
	}
	var ae apiErr
	if code := admin.do("POST", "/api/admin/users/"+bobUser.ID+"/plan", map[string]any{"plan": "gold"}, &ae); code != 422 || ae.Error.Field != "plan" {
		t.Fatalf("bad plan: %d %+v", code, ae)
	}
	if code := admin.do("POST", "/api/admin/users/"+bobUser.ID+"/plan", map[string]any{"plan": "pro", "expires_at": "2026-10-01T00:00:00Z"}, &ae); code != 422 || ae.Error.Field != "expires_at" {
		t.Fatalf("past expiry: %d %+v", code, ae)
	}
	if code := admin.do("POST", "/api/admin/users/00000000-0000-0000-0000-000000000001/plan", map[string]any{"plan": "pro"}, &ae); code != 404 {
		t.Fatalf("unknown user: %d", code)
	}
	expires := now.Add(30 * 24 * time.Hour)
	if code := admin.do("POST", "/api/admin/users/"+bobUser.ID+"/plan", map[string]any{"plan": "pro", "expires_at": expires.Format(time.RFC3339)}, nil); code != 200 {
		t.Fatalf("grant pro: %d", code)
	}

	// ---- Pro: unlocked on the next request ----
	m = bob.me()
	if m.Plan.Plan != "pro" || m.Plan.Source != "manual" || m.Plan.ExpiresAt == nil || !m.Plan.ExpiresAt.Equal(expires) ||
		m.Plan.Pages != 25 || m.Plan.StatsDays != 365 || !m.Plan.Features["remove_branding"] {
		t.Fatalf("pro plan: %+v", m.Plan)
	}
	bob.newPage("bobpage3")
	bob.do("GET", "/api/pages/"+pageID+"/stats?days=365", nil, &st)
	if st.Days != 365 || len(st.Views) != 365 {
		t.Fatalf("pro stats range: %d", st.Days)
	}
	if code := bob.do("PATCH", "/api/pages/"+pageID, map[string]any{"theme": map[string]any{"preset": "paper", "hide_branding": true}}, nil); code != 200 {
		t.Fatalf("pro hide branding: %d", code)
	}
	if _, body, _ := anon.get("/bobpage", "User-Agent", "Mozilla/5.0"); strings.Contains(body, "Made with") {
		t.Fatal("pro page still shows the footer credit")
	}
	var dom map[string]any
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &dom); code != 201 {
		t.Fatalf("pro domain: %d %v", code, dom)
	}
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "other.bob.example"}, &ae); code != 422 || ae.Error.Code != "limit" {
		t.Fatalf("second domain: %d %+v", code, ae)
	}
	e.srv.SetResolver(fakeDNS{
		txt: map[string][]string{dom["txt_name"].(string): {dom["txt_value"].(string)}},
		ip:  map[string]string{"edge.example.net": "203.0.113.7", "links.bob.example": "203.0.113.7"},
	})
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains/"+dom["id"].(string)+"/verify", nil, &dom); code != 200 || dom["verified"] != true || dom["active"] != true {
		t.Fatalf("verify: %d %v", code, dom)
	}
	served := func(want bool) {
		t.Helper()
		code, _, _ := anon.get("/", "Host", "links.bob.example", "User-Agent", "Mozilla/5.0")
		ask, _, _ := anon.get("/internal/tls-ask?domain=links.bob.example")
		if (code == 200) != want || (ask == 200) != want {
			t.Fatalf("domain served = %v: page %d, tls-ask %d", want, code, ask)
		}
		var p struct {
			Domains []struct{ Active bool }
		}
		bob.do("GET", "/api/pages/"+pageID, nil, &p)
		if len(p.Domains) != 1 || p.Domains[0].Active != want {
			t.Fatalf("domain view: %+v", p.Domains)
		}
	}
	served(true)

	// ---- Expired Pro, inside the 7-day grace period ----
	clock.Set(expires.Add(time.Hour))
	m = bob.me()
	if m.Plan.Plan != "free" || m.Plan.StoredPlan != "pro" || m.Plan.DomainsGraceUntil == nil ||
		!m.Plan.DomainsGraceUntil.Equal(expires.Add(plans.DomainGrace)) {
		t.Fatalf("expired plan: %+v", m.Plan)
	}
	if _, body, _ := anon.get("/bobpage", "User-Agent", "Mozilla/5.0"); !strings.Contains(body, "Made with") {
		t.Fatal("expired Pro page should show the footer credit again")
	}
	// The stored flag is kept, so other theme edits still save.
	var pr struct {
		Theme struct {
			HideBranding bool `json:"hide_branding"`
		}
	}
	if code := bob.do("PATCH", "/api/pages/"+pageID, map[string]any{"theme": map[string]any{"preset": "neon", "hide_branding": true}}, &pr); code != 200 || !pr.Theme.HideBranding {
		t.Fatalf("theme edit after expiry: %d %+v", code, pr)
	}
	bob.do("GET", "/api/pages/"+pageID+"/stats?days=365", nil, &st)
	if st.Days != 30 {
		t.Fatalf("expired stats range: %d", st.Days)
	}
	if code := bob.do("POST", "/api/pages", map[string]string{"slug": "bobpage4"}, &ue); code != 403 || ue.Error.Feature != "pages" {
		t.Fatalf("expired page limit: %d %+v", code, ue)
	}
	var pages []pageSummary
	if bob.do("GET", "/api/pages", nil, &pages); len(pages) != 3 {
		t.Fatalf("pages over the limit must be kept: %d", len(pages))
	}
	served(true)

	// ---- Grace period over: the domain is kept but not served ----
	clock.Set(expires.Add(plans.DomainGrace))
	served(false)
	if m = bob.me(); m.Plan.DomainsGraceUntil != nil {
		t.Fatalf("grace should be over: %+v", m.Plan)
	}

	// Granting Pro again brings it back at once.
	if code := admin.do("POST", "/api/admin/users/"+bobUser.ID+"/plan", map[string]any{"plan": "pro"}, nil); code != 200 {
		t.Fatalf("regrant: %d", code)
	}
	served(true)
	if m = bob.me(); m.Plan.Plan != "pro" || m.Plan.ExpiresAt != nil {
		t.Fatalf("regranted: %+v", m.Plan)
	}

	// ---- Revoke ----
	if code := admin.do("POST", "/api/admin/users/"+bobUser.ID+"/plan", map[string]any{"plan": "free", "expires_at": "2030-01-01T00:00:00Z"}, nil); code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	if m = bob.me(); m.Plan.Plan != "free" || m.Plan.StoredPlan != "free" || m.Plan.ExpiresAt != nil || m.Plan.Source != "" {
		t.Fatalf("revoked: %+v", m.Plan)
	}
	served(false)
	if code := admin.do("GET", "/api/admin/users?q=bob", nil, &users); code != 200 || users[0].Plan != "free" || users[0].EffectivePlan != "free" {
		t.Fatalf("admin list after revoke: %+v", users)
	}
}

// Domains attached before plans existed keep working on a Free account.
func TestGrandfatheredDomain(t *testing.T) {
	e := setup(t)
	carol := e.client()
	carol.signupVerified("carol@example.com")
	pageID := carol.newPage("carol")
	if code := carol.do("PATCH", "/api/pages/"+pageID, map[string]any{"published": true}, nil); code != 200 {
		t.Fatalf("publish: %d", code)
	}
	pool := testPool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO domains (page_id, hostname, verify_token, verified_at, grandfathered)
		VALUES ($1, 'old.carol.example', 'tok', now(), true), ($1, 'new.carol.example', 'tok2', now(), false)`, pageID); err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]int{"old.carol.example": 200, "new.carol.example": 404} {
		if code, _, _ := e.client().get("/", "Host", host, "User-Agent", "Mozilla/5.0"); code != want {
			t.Errorf("%s: %d, want %d", host, code, want)
		}
	}
}

func TestScheduleSetsBound(t *testing.T) {
	cases := map[string]struct{ body, field string }{
		"none":     {`{}`, ""},
		"clear":    {`{"starts_at": null, "ends_at": null}`, ""},
		"start":    {`{"starts_at": "2026-10-02T00:00:00Z"}`, "starts_at"},
		"end only": {`{"starts_at": null, "ends_at": "2026-10-02T00:00:00Z"}`, "ends_at"},
	}
	for name, c := range cases {
		var in scheduleInput
		if err := json.Unmarshal([]byte(c.body), &in); err != nil {
			t.Fatal(err)
		}
		f, ok := in.setsBound()
		if f != c.field || ok != (c.field != "") {
			t.Errorf("%s: got %q %v", name, f, ok)
		}
	}
}
