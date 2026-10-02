package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/plans"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// policy applies the plan table with this deployment's settings.
func (s *Server) policy() plans.Policy { return plans.Policy{FreePages: s.cfg.FreePageLimit} }

// entitlements loads the user's plan and what it allows right now.
func (s *Server) entitlements(ctx context.Context, userID string) (plans.Limits, plans.Account, error) {
	a, err := s.store.PlanOf(ctx, userID)
	if err != nil {
		return plans.Limits{}, a, err
	}
	return s.policy().Entitlements(a, s.now()), a, nil
}

// limitsFor is entitlements for the signed-in user, writing a 500 on failure.
func (s *Server) limitsFor(w http.ResponseWriter, r *http.Request) (plans.Limits, bool) {
	l, _, err := s.entitlements(r.Context(), userFrom(r).ID)
	if err != nil {
		s.serverErr(w, r, err)
		return l, false
	}
	return l, true
}

// requireFeature writes 403 upgrade_required unless the user's plan has f.
func (s *Server) requireFeature(w http.ResponseWriter, r *http.Request, f plans.Feature, field string) bool {
	l, ok := s.limitsFor(w, r)
	if !ok {
		return false
	}
	if !l.Features[f] {
		writeUpgrade(w, string(f), upgradeMessage(f), field)
		return false
	}
	return true
}

func upgradeMessage(f plans.Feature) string {
	switch f {
	case plans.CustomDomains:
		return "Custom domains are part of Pro. Upgrade to add one."
	case plans.RemoveBranding:
		return "Hiding the footer credit is part of Pro."
	}
	return plans.Names[f] + " is part of Pro."
}

// writeUpgrade writes 403 {error: {code: "upgrade_required", message, field, feature}}.
func writeUpgrade(w http.ResponseWriter, feature, msg, field string) {
	writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]string{
		"code": "upgrade_required", "message": msg, "field": field, "feature": feature,
	}})
}

// planView is the plan part of GET /api/auth/me.
type planView struct {
	plans.Limits
	// Stored is the granted plan, which can differ from Plan when Pro expired.
	Stored    plans.Tier   `json:"stored_plan"`
	Source    plans.Source `json:"source,omitempty"`
	ExpiresAt *time.Time   `json:"expires_at"`
	// DomainsGraceUntil is set while custom domains still serve after Pro expired.
	DomainsGraceUntil *time.Time   `json:"domains_grace_until"`
	Pro               plans.Limits `json:"pro"`
}

func (s *Server) planView(ctx context.Context, userID string) (*planView, error) {
	l, a, err := s.entitlements(ctx, userID)
	if err != nil {
		return nil, err
	}
	stored := a.Plan
	if !stored.Valid() {
		stored = plans.Free
	}
	return &planView{Limits: l, Stored: stored, Source: a.Source, ExpiresAt: a.ExpiresAt,
		DomainsGraceUntil: a.GraceEndsAt(s.now()), Pro: plans.ProLimits()}, nil
}

// domainServed reports whether a verified custom domain may serve its page:
// domains from before plans always do; others while the owner has Pro and
// for plans.DomainGrace after it expires.
func (s *Server) domainServed(ctx context.Context, host string) bool {
	grandfathered, owner, err := s.store.VerifiedDomainOwner(ctx, host)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("domain owner", "host", host, "err", err)
		}
		return false
	}
	return grandfathered || s.policy().ServesDomains(owner, s.now())
}

// ---- admin: plans ----

type adminUser struct {
	store.PlanUser
	EffectivePlan     plans.Tier `json:"effective_plan"`
	DomainsGraceUntil *time.Time `json:"domains_grace_until"`
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 254 {
		q = q[:254]
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > 200 {
		limit = 50
	}
	users, err := s.store.PlanUsers(r.Context(), q, limit)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	now := s.now()
	out := make([]adminUser, len(users))
	for i, u := range users {
		a := u.Account()
		out[i] = adminUser{PlanUser: u, EffectivePlan: a.Effective(now), DomainsGraceUntil: a.GraceEndsAt(now)}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAdminSetPlan grants or revokes Pro by hand.
func (s *Server) handleAdminSetPlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "user not found", "")
		return
	}
	var body struct {
		Plan      string  `json:"plan"`
		ExpiresAt optTime `json:"expires_at"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	a := plans.Account{Plan: plans.Tier(body.Plan), Source: plans.Manual}
	if !a.Plan.Valid() {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "plan must be free or pro", "plan")
		return
	}
	if body.ExpiresAt.set && a.Plan == plans.Pro {
		t, ok := body.ExpiresAt.value()
		if !ok {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "use an RFC 3339 time such as 2026-10-02T18:00:00Z", "expires_at")
			return
		}
		if t != nil && !t.After(s.now()) {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "expiry must be in the future", "expires_at")
			return
		}
		a.ExpiresAt = t
	}
	if err := s.store.SetPlan(r.Context(), id, a); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "user not found", "")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	s.log.Info("plan changed", "user", id, "plan", a.Plan, "expires_at", a.ExpiresAt, "by", userFrom(r).ID)
	v, err := s.planView(r.Context(), id)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// domainsServed returns a check for whether each of p's domains is served
// under the owner's current plan.
func (s *Server) domainsServed(ctx context.Context, p *store.Page) (func(domainID string) bool, error) {
	old, err := s.store.GrandfatheredDomains(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	owner, err := s.store.PlanOf(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	all := s.policy().ServesDomains(owner, s.now())
	return func(id string) bool { return all || old[id] }, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ownerCan reports whether a page owner's plan has f now. Lookup errors
// count as not entitled, so a database hiccup shows the default page.
func (s *Server) ownerCan(ctx context.Context, userID string, f plans.Feature) bool {
	a, err := s.store.PlanOf(ctx, userID)
	if err != nil {
		s.log.Error("owner plan", "user", userID, "err", err)
		return false
	}
	return s.policy().Can(a, f, s.now())
}

// setsBound returns the first schedule field the request sets to a time.
// Clearing a bound with null is allowed on every plan.
func (in scheduleInput) setsBound() (string, bool) {
	for _, f := range []struct {
		name string
		in   optTime
	}{{"starts_at", in.StartsAt}, {"ends_at", in.EndsAt}} {
		if f.in.set && string(f.in.raw) != "null" {
			return f.name, true
		}
	}
	return "", false
}
