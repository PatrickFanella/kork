package plans

import "time"

// Grant is one source's claim to Pro: an admin grant, a Stripe subscription
// or a Patreon membership. Each source keeps its own grant in the billing
// table; Resolve combines them into the plan stored on users.
type Grant struct {
	Source Source
	// Until ends the grant; nil means it doesn't expire. An expired grant
	// still counts, so the account reports Pro-expired and gets the
	// custom-domain grace period.
	Until *time.Time
}

// sourceRank breaks ties between grants that end at the same time.
var sourceRank = map[Source]int{Manual: 3, Stripe: 2, Patreon: 1}

// Resolve computes the stored plan from every source's grant. The account is
// Pro if any grant is Pro; the grant that lasts longest decides the source and
// expiry, so a later Stripe period never shortens a manual grant and vice
// versa. No grants means Free.
func Resolve(grants []Grant) Account {
	var best *Grant
	for i := range grants {
		g := &grants[i]
		if best == nil || later(g, best) {
			best = g
		}
	}
	if best == nil {
		return Account{Plan: Free}
	}
	a := Account{Plan: Pro, Source: best.Source}
	if best.Until != nil {
		t := *best.Until
		a.ExpiresAt = &t
	}
	return a
}

// later reports whether a outlasts b: no expiry beats any expiry, then the
// later expiry, then the source rank.
func later(a, b *Grant) bool {
	switch {
	case a.Until == nil && b.Until == nil:
		return sourceRank[a.Source] > sourceRank[b.Source]
	case a.Until == nil:
		return true
	case b.Until == nil:
		return false
	case !a.Until.Equal(*b.Until):
		return a.Until.After(*b.Until)
	}
	return sourceRank[a.Source] > sourceRank[b.Source]
}
