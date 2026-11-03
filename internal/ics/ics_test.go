package ics

import (
	"errors"
	"os"
	"testing"
	"time"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// google.ics follows Google Calendar's export format: CRLF lines, a
// VTIMEZONE, TZID and all-day dates, folded lines and escaped commas.
func TestParseGoogleExport(t *testing.T) {
	evs, skipped, err := Parse(load(t, "google.ics"))
	if err != nil || len(evs) != 3 || skipped != 1 {
		t.Fatalf("got %d events, %d skipped, %v", len(evs), skipped, err)
	}
	so36 := evs[0]
	if so36.Summary != "Live at SO36" || so36.Location != "SO36, Oranienstraße 190, Berlin" || so36.URL != "https://tickets.example/so36" || so36.AllDay {
		t.Fatalf("first event: %+v", so36)
	}
	if so36.Start.Location().String() != "Europe/Berlin" || so36.Start.Format("2006-01-02 15:04") != "2026-10-09 20:00" {
		t.Fatalf("TZID time: %v", so36.Start)
	}
	if fest := evs[1]; !fest.AllDay || fest.Start.Format("2006-01-02") != "2026-10-12" || fest.Summary != "Festival day in Leipzig" {
		t.Fatalf("all-day folded event: %+v", fest)
	}
	if radio := evs[2]; !radio.Recurring || !radio.Start.Equal(time.Date(2026, 10, 15, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("UTC recurring event: %+v", radio)
	}
}

// gancio.ics follows Gancio's feed format: LF lines and a quoted TZID.
func TestParseGancioFeed(t *testing.T) {
	evs, _, err := Parse(load(t, "gancio.ics"))
	if err != nil || len(evs) != 1 {
		t.Fatalf("%d %v", len(evs), err)
	}
	e := evs[0]
	if e.Start.Location().String() != "America/Chicago" || e.Start.Format("15:04") != "21:00" || e.Location != "The Hall" {
		t.Fatalf("%+v", e)
	}
}

func TestParseRejectsNonCalendar(t *testing.T) {
	if _, _, err := Parse([]byte("<html>not a calendar</html>")); !errors.Is(err, ErrNotCalendar) {
		t.Fatalf("got %v", err)
	}
}
