package server

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// publicMaxAge is the Cache-Control max-age for public pages.
const publicMaxAge = 60 * time.Second

// SetClock replaces the clock used for block schedules (tests).
func (s *Server) SetClock(now func() time.Time) { s.now = now }

// optTime records whether a JSON field was present, so PATCH can tell an
// omitted bound (keep) from null (clear).
type optTime struct {
	set bool
	raw json.RawMessage
}

func (o *optTime) UnmarshalJSON(b []byte) error {
	o.set = true
	o.raw = append(o.raw[:0], b...)
	return nil
}

// value parses an RFC 3339 timestamp or null.
func (o optTime) value() (*time.Time, bool) {
	if bytes.Equal(o.raw, []byte("null")) {
		return nil, true
	}
	var str string
	if json.Unmarshal(o.raw, &str) != nil {
		return nil, false
	}
	t, err := time.Parse(time.RFC3339, str)
	if err != nil {
		return nil, false
	}
	t = t.UTC()
	return &t, true
}

// scheduleInput is embedded in block create and update bodies.
type scheduleInput struct {
	StartsAt optTime `json:"starts_at"`
	EndsAt   optTime `json:"ends_at"`
}

// applyTo merges the fields that were sent into sched and validates the
// result, writing a 422 and returning false when it is invalid.
func (in scheduleInput) applyTo(w http.ResponseWriter, sched *store.Schedule) bool {
	next := *sched
	for _, f := range []struct {
		name string
		in   optTime
		dst  **time.Time
	}{{"starts_at", in.StartsAt, &next.StartsAt}, {"ends_at", in.EndsAt, &next.EndsAt}} {
		if !f.in.set {
			continue
		}
		v, ok := f.in.value()
		if !ok {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "use an RFC 3339 time such as 2026-10-02T18:00:00Z", f.name)
			return false
		}
		*f.dst = v
	}
	if !next.Ordered() {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "end time must be after the start time", "ends_at")
		return false
	}
	*sched = next
	return true
}

// scheduleBadge describes, for the owner preview, why visitors don't see a
// block right now. It is empty when the block is visible.
func scheduleBadge(sched store.Schedule, now time.Time) (text string, at time.Time) {
	switch {
	case sched.StartsAt != nil && now.Before(*sched.StartsAt):
		return "Scheduled: shows from", *sched.StartsAt
	case sched.EndsAt != nil && !now.Before(*sched.EndsAt):
		return "Expired", *sched.EndsAt
	}
	return "", time.Time{}
}

// publicCacheSeconds is the max-age for a public page rendered at now. It
// shrinks when an enabled block starts or ends sooner than publicMaxAge, so a
// shared cache never serves a page past a schedule boundary.
func publicCacheSeconds(list []*store.Block, now time.Time) int {
	ttl := publicMaxAge
	for _, b := range list {
		if !b.Enabled {
			continue
		}
		if next, ok := b.NextBoundary(now); ok && next.Sub(now) < ttl {
			ttl = next.Sub(now)
		}
	}
	// Round down so the cached copy expires at or before the boundary.
	return int(math.Floor(ttl.Seconds()))
}
