package plans

import (
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	day := 24 * time.Hour

	cases := []struct {
		name   string
		grants []Grant
		plan   Tier
		source Source
		until  *time.Time
		eff    Tier // effective at now
	}{
		{"no sources", nil, Free, "", nil, Free},
		{"manual forever", []Grant{{Manual, nil}}, Pro, Manual, nil, Pro},
		{"stripe only", []Grant{{Stripe, at(30 * day)}}, Pro, Stripe, at(30 * day), Pro},
		{"manual later than stripe", []Grant{{Stripe, at(30 * day)}, {Manual, at(90 * day)}}, Pro, Manual, at(90 * day), Pro},
		{"stripe later than manual", []Grant{{Manual, at(10 * day)}, {Stripe, at(30 * day)}}, Pro, Stripe, at(30 * day), Pro},
		{"manual forever beats stripe", []Grant{{Stripe, at(300 * day)}, {Manual, nil}}, Pro, Manual, nil, Pro},
		{"patreon outlasts expired stripe", []Grant{{Stripe, at(-2 * day)}, {Patreon, at(8 * day)}}, Pro, Patreon, at(8 * day), Pro},
		{"all expired keeps latest expiry", []Grant{{Stripe, at(-2 * day)}, {Patreon, at(-5 * day)}}, Pro, Stripe, at(-2 * day), Free},
		{"tie prefers manual then stripe", []Grant{{Patreon, at(day)}, {Stripe, at(day)}, {Manual, at(day)}}, Pro, Manual, at(day), Pro},
		{"tie stripe over patreon", []Grant{{Patreon, at(day)}, {Stripe, at(day)}}, Pro, Stripe, at(day), Pro},
	}
	for _, c := range cases {
		a := Resolve(c.grants)
		if a.Plan != c.plan || a.Source != c.source || !sameTime(a.ExpiresAt, c.until) || a.Effective(now) != c.eff {
			t.Errorf("%s: got %+v (effective %s)", c.name, a, a.Effective(now))
		}
	}

	// Resolve copies the expiry instead of aliasing the grant's time.
	g := []Grant{{Stripe, at(day)}}
	a := Resolve(g)
	*g[0].Until = now
	if a.ExpiresAt.Equal(now) {
		t.Fatal("Resolve aliased the grant's expiry")
	}
	// An expired Pro grant gets the custom-domain grace period.
	if a := Resolve([]Grant{{Stripe, at(-day)}}); !a.InGrace(now) {
		t.Fatal("recently expired grant should be in grace")
	}
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
