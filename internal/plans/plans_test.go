package plans

import (
	"testing"
	"time"
)

func TestEntitlements(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	longAgo := now.Add(-8 * 24 * time.Hour)
	future := now.Add(time.Hour)
	p := Policy{}

	cases := []struct {
		name      string
		acct      Account
		plan      Tier
		pages     int
		statsDays int
		domains   int
		branding  bool
		serves    bool
		grace     bool
	}{
		{"free", Account{Plan: Free}, Free, 5, 30, 0, false, false, false},
		{"zero value", Account{}, Free, 5, 30, 0, false, false, false},
		{"pro forever", Account{Plan: Pro, Source: Manual}, Pro, 25, 365, 1, true, true, false},
		{"pro until later", Account{Plan: Pro, ExpiresAt: &future}, Pro, 25, 365, 1, true, true, false},
		{"pro expired, in grace", Account{Plan: Pro, ExpiresAt: &past}, Free, 5, 30, 0, false, true, true},
		{"pro expired, grace over", Account{Plan: Pro, ExpiresAt: &longAgo}, Free, 5, 30, 0, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := p.Entitlements(c.acct, now)
			if l.Plan != c.plan || l.Pages != c.pages || l.StatsDays != c.statsDays || l.CustomDomains != c.domains {
				t.Fatalf("got %+v", l)
			}
			if got := p.Can(c.acct, RemoveBranding, now); got != c.branding {
				t.Errorf("Can(RemoveBranding) = %v", got)
			}
			if got := p.Can(c.acct, CustomDomains, now); got != (c.domains > 0) {
				t.Errorf("Can(CustomDomains) = %v", got)
			}
			if !p.Can(c.acct, ScheduledLinks, now) {
				t.Error("scheduled links should be allowed on every plan")
			}
			if got := p.ServesDomains(c.acct, now); got != c.serves {
				t.Errorf("ServesDomains = %v", got)
			}
			if got := c.acct.InGrace(now); got != c.grace {
				t.Errorf("InGrace = %v", got)
			}
			if got := c.acct.GraceEndsAt(now) != nil; got != c.grace {
				t.Errorf("GraceEndsAt set = %v", got)
			}
		})
	}
}

func TestExpiryBoundaries(t *testing.T) {
	exp := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	a := Account{Plan: Pro, ExpiresAt: &exp}
	if a.Effective(exp.Add(-time.Nanosecond)) != Pro || a.Effective(exp) != Free {
		t.Fatal("Pro should end exactly at expires_at")
	}
	end := exp.Add(DomainGrace)
	if !a.InGrace(end.Add(-time.Nanosecond)) || a.InGrace(end) {
		t.Fatal("grace should end exactly DomainGrace after expiry")
	}
}

func TestFreePageOverride(t *testing.T) {
	now := time.Now()
	p := Policy{FreePages: 2}
	if got := p.Entitlements(Account{Plan: Free}, now).Pages; got != 2 {
		t.Fatalf("free pages = %d", got)
	}
	if got := p.Entitlements(Account{Plan: Pro}, now).Pages; got != 25 {
		t.Fatalf("override must not change Pro: %d", got)
	}
}

func TestEntitlementsAreCopies(t *testing.T) {
	now := time.Now()
	l := Policy{}.Entitlements(Account{Plan: Free}, now)
	l.Features[RemoveBranding] = true
	if (Policy{}).Can(Account{Plan: Free}, RemoveBranding, now) {
		t.Fatal("mutating returned limits changed the table")
	}
}

func TestEveryFeatureInTable(t *testing.T) {
	for _, f := range Features {
		if Names[f] == "" {
			t.Errorf("%s has no name", f)
		}
		for tier, l := range table {
			if _, ok := l.Features[f]; !ok {
				t.Errorf("%s missing from %s", f, tier)
			}
		}
	}
}
