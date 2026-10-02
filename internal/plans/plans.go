// Package plans decides what an account may use. Every limit and Pro feature
// is defined in the table below; handlers ask Entitlements or Can instead of
// checking plan names themselves.
package plans

import (
	"maps"
	"time"
)

// Tier is a plan name stored on users.plan.
type Tier string

const (
	Free Tier = "free"
	Pro  Tier = "pro"
)

// Valid reports whether t is a known plan.
func (t Tier) Valid() bool { return t == Free || t == Pro }

// Source records who granted the plan: an admin by hand, or a billing
// provider once billing exists.
type Source string

const (
	Manual  Source = "manual"
	Patreon Source = "patreon"
	Stripe  Source = "stripe"
)

// Valid reports whether s is a known source.
func (s Source) Valid() bool { return s == Manual || s == Patreon || s == Stripe }

// Feature is an on/off entitlement.
type Feature string

const (
	CustomDomains  Feature = "custom_domains"
	RemoveBranding Feature = "remove_branding" // hide the "Made with Kork" footer link
	CustomCSS      Feature = "custom_css"      // defined now; implemented in #14
	CustomFonts    Feature = "custom_fonts"    // font uploads; defined now, implemented in #14
	ScheduledLinks Feature = "scheduled_links"
)

// Features lists every feature key in display order.
var Features = []Feature{CustomDomains, RemoveBranding, CustomCSS, CustomFonts, ScheduledLinks}

// Names are the labels the editor and error messages use.
var Names = map[Feature]string{
	CustomDomains:  "Custom domains",
	RemoveBranding: "Hide the footer credit",
	CustomCSS:      "Custom CSS",
	CustomFonts:    "Font uploads",
	ScheduledLinks: "Scheduled links",
}

// Limit keys name numeric limits in upgrade_required errors.
const (
	LimitPages     = "pages"
	LimitStatsDays = "stats_days"
)

// DefaultFreePages is the free page limit when ZELDA_FREE_PAGE_LIMIT is unset.
const DefaultFreePages = 5

// DomainGrace is how long custom domains keep serving after Pro expires.
// After that the domain stays stored but the page is not served on it until
// the owner has Pro again.
const DomainGrace = 7 * 24 * time.Hour

// Limits is what one account may use right now.
type Limits struct {
	// Plan is the effective plan: an expired Pro grant reports Free.
	Plan          Tier             `json:"plan"`
	Pages         int              `json:"pages"`
	StatsDays     int              `json:"stats_days"`
	CustomDomains int              `json:"custom_domains"`
	Features      map[Feature]bool `json:"features"`
}

// table holds every plan's limits. The values are placeholders until the
// owner decides prices and tiers (Gitea #12, #14, #15); change them here
// only. The free page limit is overridden by ZELDA_FREE_PAGE_LIMIT. To make
// scheduled links Pro-only, set ScheduledLinks to false under Free.
var table = map[Tier]Limits{
	Free: {
		Pages:         DefaultFreePages,
		StatsDays:     30,
		CustomDomains: 0,
		Features: map[Feature]bool{
			CustomDomains:  false,
			RemoveBranding: false,
			CustomCSS:      false,
			CustomFonts:    false,
			ScheduledLinks: true,
		},
	},
	Pro: {
		Pages:         25,
		StatsDays:     365,
		CustomDomains: 1,
		Features: map[Feature]bool{
			CustomDomains:  true,
			RemoveBranding: true,
			CustomCSS:      true,
			CustomFonts:    true,
			ScheduledLinks: true,
		},
	},
}

// Account is the plan stored on a user.
type Account struct {
	Plan   Tier
	Source Source // empty for accounts that never had a plan granted
	// ExpiresAt ends a Pro grant; nil means it doesn't expire.
	ExpiresAt *time.Time
}

// Effective is the plan in force at now. Pro past its expiry acts as Free.
func (a Account) Effective(now time.Time) Tier {
	if a.Plan == Pro && (a.ExpiresAt == nil || now.Before(*a.ExpiresAt)) {
		return Pro
	}
	return Free
}

// InGrace reports whether a Pro grant expired less than DomainGrace ago.
func (a Account) InGrace(now time.Time) bool {
	return a.Plan == Pro && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt) && now.Before(a.ExpiresAt.Add(DomainGrace))
}

// GraceEndsAt is when custom domains stop serving after an expired Pro
// grant, or nil when the account is not in the grace period.
func (a Account) GraceEndsAt(now time.Time) *time.Time {
	if !a.InGrace(now) {
		return nil
	}
	t := a.ExpiresAt.Add(DomainGrace)
	return &t
}

// Policy applies the table with deployment settings.
type Policy struct {
	// FreePages overrides the free page limit; 0 keeps DefaultFreePages.
	FreePages int
}

// Entitlements returns what the account may use at now.
func (p Policy) Entitlements(a Account, now time.Time) Limits {
	tier := a.Effective(now)
	l := table[tier]
	l.Plan = tier
	l.Features = maps.Clone(l.Features)
	if tier == Free && p.FreePages > 0 {
		l.Pages = p.FreePages
	}
	return l
}

// Can reports whether the account may use f at now.
func (p Policy) Can(a Account, f Feature, now time.Time) bool {
	return p.Entitlements(a, now).Features[f]
}

// ServesDomains reports whether the owner's Pro-only custom domains are
// served at now: while entitled, and for DomainGrace after Pro expires.
func (p Policy) ServesDomains(a Account, now time.Time) bool {
	return p.Can(a, CustomDomains, now) || a.InGrace(now)
}

// ProLimits returns the Pro plan's limits, for upgrade messages.
func ProLimits() Limits {
	l := table[Pro]
	l.Plan = Pro
	l.Features = maps.Clone(l.Features)
	return l
}
