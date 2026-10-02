package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

func TestPublicCacheSeconds(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	in := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	block := func(enabled bool, sched store.Schedule) *store.Block {
		return &store.Block{Enabled: enabled, Schedule: sched}
	}
	cases := []struct {
		name string
		list []*store.Block
		want int
	}{
		{"no blocks", nil, 60},
		{"unscheduled", []*store.Block{block(true, store.Schedule{})}, 60},
		{"boundary beyond a minute", []*store.Block{block(true, store.Schedule{StartsAt: in(5 * time.Minute)})}, 60},
		{"start in 30s", []*store.Block{block(true, store.Schedule{StartsAt: in(30 * time.Second)})}, 30},
		{"end in 12.9s rounds down", []*store.Block{block(true, store.Schedule{EndsAt: in(12900 * time.Millisecond)})}, 12},
		{"earliest wins", []*store.Block{
			block(true, store.Schedule{EndsAt: in(40 * time.Second)}),
			block(true, store.Schedule{StartsAt: in(10 * time.Second)}),
		}, 10},
		{"disabled ignored", []*store.Block{block(false, store.Schedule{StartsAt: in(5 * time.Second)})}, 60},
		{"past bounds ignored", []*store.Block{block(true, store.Schedule{StartsAt: in(-time.Hour), EndsAt: in(-time.Second)})}, 60},
		{"boundary under a second", []*store.Block{block(true, store.Schedule{EndsAt: in(300 * time.Millisecond)})}, 0},
	}
	for _, c := range cases {
		if got := publicCacheSeconds(c.list, now); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestScheduleInputApply(t *testing.T) {
	start := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	existing := store.Schedule{StartsAt: &start}
	cases := []struct {
		body      string
		wantField string
		check     func(store.Schedule) bool
	}{
		{`{}`, "", func(s store.Schedule) bool { return s.StartsAt.Equal(start) && s.EndsAt == nil }},
		{`{"starts_at":null}`, "", func(s store.Schedule) bool { return s.StartsAt == nil }},
		{`{"ends_at":"2026-10-02T20:00:00Z"}`, "", func(s store.Schedule) bool {
			return s.StartsAt.Equal(start) && s.EndsAt.Equal(start.Add(2*time.Hour))
		}},
		{`{"starts_at":"2026-10-02T13:00:00-05:00"}`, "", func(s store.Schedule) bool {
			return s.StartsAt.Equal(start) && s.StartsAt.Location() == time.UTC
		}},
		{`{"ends_at":"2026-10-02T18:00:00Z"}`, "ends_at", nil},
		{`{"ends_at":"2026-10-02T17:00:00Z"}`, "ends_at", nil},
		{`{"starts_at":"tomorrow"}`, "starts_at", nil},
		{`{"starts_at":1759428000}`, "starts_at", nil},
		{`{"ends_at":"2026-10-02 20:00"}`, "ends_at", nil},
	}
	for _, c := range cases {
		var in scheduleInput
		if err := json.Unmarshal([]byte(c.body), &in); err != nil {
			t.Fatalf("%s: %v", c.body, err)
		}
		sched := existing
		w := httptest.NewRecorder()
		ok := in.applyTo(w, &sched)
		if c.wantField != "" {
			var ae apiErr
			json.Unmarshal(w.Body.Bytes(), &ae) //nolint:errcheck
			if ok || w.Code != http.StatusUnprocessableEntity || ae.Error.Field != c.wantField {
				t.Errorf("%s: ok=%v code=%d field=%q", c.body, ok, w.Code, ae.Error.Field)
			}
			if !sched.StartsAt.Equal(start) || sched.EndsAt != nil {
				t.Errorf("%s: rejected input changed the schedule", c.body)
			}
			continue
		}
		if !ok || !c.check(sched) {
			t.Errorf("%s: ok=%v schedule=%+v", c.body, ok, sched)
		}
	}
}

// testClock is a settable clock safe to read from handler goroutines.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

func TestScheduledBlocks(t *testing.T) {
	e := setup(t)
	t0 := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: t0}
	e.srv.SetClock(clock.Now)
	ts := func(d time.Duration) string { return t0.Add(d).Format(time.RFC3339) }

	alice := e.client()
	var ae apiErr
	if code := alice.do("POST", "/api/auth/signup", map[string]any{"email": "sched@example.com", "password": "correct horse battery", "accept_terms": true}, nil); code != 201 {
		t.Fatalf("signup: %d", code)
	}
	tok := e.mail.tokenFor(t, "sched@example.com", "/app/verify")
	if code := alice.do("POST", "/api/auth/verify", map[string]string{"token": tok}, nil); code != 200 {
		t.Fatalf("verify: %d", code)
	}
	var page pageResp
	if code := alice.do("POST", "/api/pages", map[string]string{"slug": "drops", "title": "Drops"}, &page); code != 201 {
		t.Fatalf("create page: %d", code)
	}
	if code := alice.do("PATCH", "/api/pages/"+page.ID, map[string]any{"published": true}, nil); code != 200 {
		t.Fatalf("publish: %d", code)
	}

	type blockResp struct {
		ID       string     `json:"id"`
		StartsAt *time.Time `json:"starts_at"`
		EndsAt   *time.Time `json:"ends_at"`
	}
	create := func(title string, sched map[string]any) blockResp {
		t.Helper()
		body := map[string]any{"type": "link", "data": map[string]any{"url": "https://example.com/" + strings.ToLower(title), "title": title}}
		for k, v := range sched {
			body[k] = v
		}
		var b blockResp
		if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", body, &b); code != 201 {
			t.Fatalf("create %s: %d", title, code)
		}
		return b
	}

	// Validation: end must follow start, times must be RFC 3339.
	bad := map[string]any{"type": "link", "data": map[string]any{"url": "https://example.com", "title": "x"}, "starts_at": ts(time.Hour), "ends_at": ts(time.Hour)}
	if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", bad, &ae); code != 422 || ae.Error.Field != "ends_at" {
		t.Fatalf("equal bounds: %d %+v", code, ae)
	}
	bad["starts_at"], bad["ends_at"] = "next friday", nil
	if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", bad, &ae); code != 422 || ae.Error.Field != "starts_at" {
		t.Fatalf("bad time: %d %+v", code, ae)
	}

	always := create("Always", nil)
	future := create("Release", map[string]any{"starts_at": ts(time.Hour)})
	window := create("Event", map[string]any{"starts_at": ts(-time.Hour), "ends_at": ts(30 * time.Second)})
	expired := create("Presale", map[string]any{"starts_at": nil, "ends_at": ts(-time.Minute)})
	var contact blockResp
	if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", map[string]any{
		"type": "contact", "data": map[string]any{"name": "Box Office", "email": "box@example.com"}, "starts_at": ts(time.Hour),
	}, &contact); code != 201 {
		t.Fatalf("create contact: %d", code)
	}
	if always.StartsAt != nil || always.EndsAt != nil || future.StartsAt == nil || !future.StartsAt.Equal(t0.Add(time.Hour)) || future.EndsAt != nil {
		t.Fatalf("schedule JSON: %+v %+v", always, future)
	}

	anon := e.client()
	public := func() (string, http.Header) {
		t.Helper()
		code, body, hdr := anon.get("/drops", "User-Agent", "Mozilla/5.0")
		if code != 200 {
			t.Fatalf("public page: %d", code)
		}
		return body, hdr
	}
	shows := func(body, title string) bool { return strings.Contains(body, "<strong>"+title+"</strong>") }

	// At t0: future and expired hidden publicly; the window ends in 30s.
	body, hdr := public()
	if !shows(body, "Always") || !shows(body, "Event") || shows(body, "Release") || shows(body, "Presale") || strings.Contains(body, "Box Office") {
		t.Fatalf("public page at t0:\n%s", body)
	}
	if strings.Contains(body, "schedule-badge") {
		t.Fatal("public page shows schedule badges")
	}
	if cc := hdr.Get("Cache-Control"); cc != "public, max-age=30" {
		t.Fatalf("cache-control near boundary: %q", cc)
	}

	// The owner preview shows every enabled block, with badges on hidden ones.
	_, prev, _ := alice.get("/preview/" + page.ID)
	for _, want := range []string{"<strong>Release</strong>", "<strong>Presale</strong>", "Box Office",
		`Scheduled: shows from <time datetime="2026-10-02T13:00:00Z" data-local-time>Oct 2, 2026 13:00 UTC</time>`,
		`Expired <time datetime="2026-10-02T11:59:00Z" data-local-time>Oct 2, 2026 11:59 UTC</time>`} {
		if !strings.Contains(prev, want) {
			t.Errorf("preview missing %q", want)
		}
	}
	if n := strings.Count(prev, `class="schedule-badge"`); n != 3 {
		t.Errorf("preview badges: got %d, want 3", n)
	}

	// Beacons and vCards for blocks outside their window are ignored.
	track := func(id string) {
		req, _ := http.NewRequest("POST", e.ts.URL+"/t/"+id, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 204 {
			t.Fatalf("track: %v %v", err, res)
		}
		res.Body.Close()
	}
	track(future.ID)
	track(expired.ID)
	track(always.ID)
	var st store.Stats
	alice.do("GET", "/api/pages/"+page.ID+"/stats?days=1", nil, &st)
	if len(st.Blocks) != 1 || st.Blocks[0].BlockID != always.ID {
		t.Fatalf("hidden blocks counted: %+v", st.Blocks)
	}
	if code, _, _ := anon.get("/vcard/" + contact.ID); code != 404 {
		t.Fatalf("scheduled vcard served publicly: %d", code)
	}
	if code, _, _ := alice.get("/vcard/" + contact.ID); code != 200 {
		t.Fatalf("owner vcard from preview: %d", code)
	}

	// Exactly at the window's end the block is gone, without any edit.
	clock.Set(t0.Add(30 * time.Second))
	body, hdr = public()
	if shows(body, "Event") || !shows(body, "Always") {
		t.Fatalf("window end edge:\n%s", body)
	}
	if cc := hdr.Get("Cache-Control"); cc != "public, max-age=60" {
		t.Fatalf("cache-control without near boundary: %q", cc)
	}
	track(window.ID)

	// Exactly at the start the release and its contact card appear.
	clock.Set(t0.Add(time.Hour))
	body, _ = public()
	if !shows(body, "Release") || !strings.Contains(body, "Box Office") {
		t.Fatalf("start edge:\n%s", body)
	}
	if code, vcf, _ := anon.get("/vcard/" + contact.ID); code != 200 || !strings.Contains(vcf, "FN:Box Office") {
		t.Fatalf("vcard at start: %d", code)
	}
	track(future.ID)
	alice.do("GET", "/api/pages/"+page.ID+"/stats?days=1", nil, &st)
	counted := map[string]int64{}
	for _, b := range st.Blocks {
		counted[b.BlockID] = b.Count
	}
	if counted[future.ID] != 1 || counted[window.ID] != 0 {
		t.Fatalf("counts after start: %+v", st.Blocks)
	}

	// PATCH: an omitted bound is kept, null clears, ordering uses merged values.
	var upd blockResp
	if code := alice.do("PATCH", fmt.Sprintf("/api/pages/%s/blocks/%s", page.ID, future.ID), map[string]any{"ends_at": ts(30 * time.Minute)}, &ae); code != 422 || ae.Error.Field != "ends_at" {
		t.Fatalf("patch end before kept start: %d %+v", code, ae)
	}
	if code := alice.do("PATCH", fmt.Sprintf("/api/pages/%s/blocks/%s", page.ID, future.ID), map[string]any{"ends_at": ts(2 * time.Hour)}, &upd); code != 200 ||
		upd.StartsAt == nil || !upd.StartsAt.Equal(t0.Add(time.Hour)) || upd.EndsAt == nil || !upd.EndsAt.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("patch end: %+v", upd)
	}
	if code := alice.do("PATCH", fmt.Sprintf("/api/pages/%s/blocks/%s", page.ID, expired.ID), map[string]any{"ends_at": nil}, &upd); code != 200 || upd.EndsAt != nil || upd.StartsAt != nil {
		t.Fatalf("clear end: %+v", upd)
	}
	body, _ = public()
	if !shows(body, "Presale") {
		t.Fatal("cleared schedule should show the block")
	}
	var raw map[string]any
	alice.do("GET", "/api/pages/"+page.ID, nil, &raw)
	first := raw["blocks"].([]any)[0].(map[string]any)
	if _, ok := first["starts_at"]; !ok {
		t.Fatalf("page JSON blocks lack starts_at: %v", first)
	}
}
