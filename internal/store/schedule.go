package store

import "time"

// Schedule is a block's optional visibility window. A nil bound is open. The
// window includes StartsAt and excludes EndsAt, so a block ending at 18:00 is
// gone at 18:00:00.
type Schedule struct {
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
}

// VisibleAt reports whether t falls inside the window.
func (s Schedule) VisibleAt(t time.Time) bool {
	if s.StartsAt != nil && t.Before(*s.StartsAt) {
		return false
	}
	if s.EndsAt != nil && !t.Before(*s.EndsAt) {
		return false
	}
	return true
}

// Ordered reports whether EndsAt is after StartsAt when both are set.
func (s Schedule) Ordered() bool {
	return s.StartsAt == nil || s.EndsAt == nil || s.EndsAt.After(*s.StartsAt)
}

// NextBoundary returns the earliest bound strictly after t, when there is one.
func (s Schedule) NextBoundary(t time.Time) (time.Time, bool) {
	var next time.Time
	found := false
	for _, b := range []*time.Time{s.StartsAt, s.EndsAt} {
		if b != nil && b.After(t) && (!found || b.Before(next)) {
			next, found = *b, true
		}
	}
	return next, found
}
