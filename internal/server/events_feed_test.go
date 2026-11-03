package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
)

// An events block can read its dates from a public calendar feed (#39).
func TestEventsFromCalendarFeed(t *testing.T) {
	e := setup(t)
	clock := &testClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	e.srv.SetClock(clock.Now)
	var mu sync.Mutex
	fail := false
	cal := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n"
	for _, ev := range []struct{ start, summary string }{
		{"20261020T200000Z", "Five"}, {"20261005T200000Z", "Two"}, {"20260901T200000Z", "Past one"},
		{"20261012T200000Z", "Four"}, {"20261003T200000Z", "One"}, {"20260915T200000Z", "Past two"},
		{"20261010T200000Z", "Three"},
	} {
		cal += "BEGIN:VEVENT\r\nDTSTART:" + ev.start + "\r\nSUMMARY:" + ev.summary + "\r\nEND:VEVENT\r\n"
	}
	cal += "END:VCALENDAR\r\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/not.ics":
			w.Write([]byte("<html>no</html>"))
		case fail:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "text/calendar")
			w.Write([]byte(cal))
		}
	}))
	defer ts.Close()
	e.srv.SetFetcher(fetch.New(fetch.Options{AllowAddr: func(ip netip.Addr) bool { return ip.IsLoopback() }, AllowPort: func(int) bool { return true }}))
	alice, pageID := livePage(t, e, "alice@example.com", "alice")

	// Addresses that aren't calendars, or that the fetcher refuses, are reported.
	for _, c := range []struct{ url, want string }{
		{ts.URL + "/not.ics", "doesn't return a calendar"},
		{"http://10.0.0.1/tour.ics", "can't be fetched"},
	} {
		code, _, ae := alice.createBlock(pageID, "events", map[string]any{"source": "ics", "ics_url": c.url})
		if code != 422 || ae.Error.Field != "ics_url" || !strings.Contains(ae.Error.Message, c.want) {
			t.Errorf("%s: %d %+v", c.url, code, ae)
		}
	}
	code, blk, ae := alice.createBlock(pageID, "events", map[string]any{"source": "ics", "ics_url": ts.URL + "/tour.ics"})
	if code != 201 {
		t.Fatalf("create: %d %+v", code, ae)
	}

	order := func() []string {
		_, body, _ := e.client().get("/alice")
		var got []string
		for _, name := range []string{"One", "Two", "Three", "Four", "Five", "Past one", "Past two"} {
			if i := strings.Index(body, ">"+name+"<"); i >= 0 {
				got = append(got, name)
			}
		}
		// Sort by position in the page.
		pos := func(n string) int { return strings.Index(body, ">"+n+"<") }
		for i := range got {
			for j := i + 1; j < len(got); j++ {
				if pos(got[j]) < pos(got[i]) {
					got[i], got[j] = got[j], got[i]
				}
			}
		}
		return got
	}
	if got := strings.Join(order(), ","); got != "One,Two,Three,Four,Five" {
		t.Fatalf("public page shows %s", got)
	}
	status := func() map[string]any {
		var p struct {
			Blocks []struct {
				ID   string         `json:"id"`
				Feed map[string]any `json:"feed"`
			} `json:"blocks"`
		}
		alice.do("GET", "/api/pages/"+pageID, nil, &p)
		for _, b := range p.Blocks {
			if b.ID == blk.ID {
				return b.Feed
			}
		}
		return nil
	}
	if f := status(); f == nil || f["found"] != float64(5) || f["error"] != "" {
		t.Fatalf("editor status: %+v", f)
	}

	// A failed refresh keeps the last good dates and tells the owner.
	mu.Lock()
	fail = true
	mu.Unlock()
	clock.Set(clock.Now().Add(2 * time.Hour))
	e.client().get("/alice") // starts the background refresh
	e.srv.WaitFeedRefreshes()
	if got := strings.Join(order(), ","); got != "One,Two,Three,Four,Five" {
		t.Fatalf("after a failed refresh the page shows %s", got)
	}
	if f := status(); f == nil || f["found"] != float64(5) || !strings.Contains(f["error"].(string), "didn't return the calendar") {
		t.Fatalf("editor status after failure: %+v", f)
	}
}
