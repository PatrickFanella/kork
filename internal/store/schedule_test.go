package store

import (
	"testing"
	"time"
)

func at(t time.Time) *time.Time { return &t }

func TestScheduleVisibleAt(t *testing.T) {
	start := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	cases := []struct {
		name  string
		sched Schedule
		now   time.Time
		want  bool
	}{
		{"no bounds", Schedule{}, start, true},
		{"before start", Schedule{StartsAt: at(start)}, start.Add(-time.Nanosecond), false},
		{"exactly at start", Schedule{StartsAt: at(start)}, start, true},
		{"after start, open end", Schedule{StartsAt: at(start)}, start.Add(100 * 24 * time.Hour), true},
		{"open start, before end", Schedule{EndsAt: at(end)}, end.Add(-time.Nanosecond), true},
		{"exactly at end", Schedule{EndsAt: at(end)}, end, false},
		{"after end", Schedule{EndsAt: at(end)}, end.Add(time.Second), false},
		{"inside window", Schedule{StartsAt: at(start), EndsAt: at(end)}, start.Add(time.Hour), true},
		{"window start edge", Schedule{StartsAt: at(start), EndsAt: at(end)}, start, true},
		{"window end edge", Schedule{StartsAt: at(start), EndsAt: at(end)}, end, false},
		{"other zone, same instant", Schedule{StartsAt: at(start)}, start.In(time.FixedZone("CDT", -5*3600)), true},
	}
	for _, c := range cases {
		if got := c.sched.VisibleAt(c.now); got != c.want {
			t.Errorf("%s: VisibleAt = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestScheduleOrdered(t *testing.T) {
	start := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	if !(Schedule{}).Ordered() || !(Schedule{StartsAt: at(start)}).Ordered() || !(Schedule{EndsAt: at(start)}).Ordered() {
		t.Error("open bounds must be ordered")
	}
	if (Schedule{StartsAt: at(start), EndsAt: at(start)}).Ordered() {
		t.Error("equal bounds must be rejected")
	}
	if (Schedule{StartsAt: at(start), EndsAt: at(start.Add(-time.Second))}).Ordered() {
		t.Error("end before start must be rejected")
	}
	if !(Schedule{StartsAt: at(start), EndsAt: at(start.Add(time.Second))}).Ordered() {
		t.Error("end after start must be accepted")
	}
}

func TestScheduleNextBoundary(t *testing.T) {
	start := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	s := Schedule{StartsAt: at(start), EndsAt: at(end)}
	if got, ok := s.NextBoundary(start.Add(-time.Minute)); !ok || !got.Equal(start) {
		t.Errorf("before start: %v %v", got, ok)
	}
	if got, ok := s.NextBoundary(start); !ok || !got.Equal(end) {
		t.Errorf("at start: %v %v", got, ok)
	}
	if _, ok := s.NextBoundary(end); ok {
		t.Error("at end there is no later boundary")
	}
	if _, ok := (Schedule{}).NextBoundary(start); ok {
		t.Error("open schedule has no boundary")
	}
}
