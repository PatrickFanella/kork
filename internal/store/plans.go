package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/plans"
)

// ---- plans ----

const planCols = `plan, plan_source, plan_expires_at`

func scanPlan(plan string, source *string, expires *time.Time) plans.Account {
	a := plans.Account{Plan: plans.Tier(plan), ExpiresAt: expires}
	if source != nil {
		a.Source = plans.Source(*source)
	}
	return a
}

// PlanOf returns the plan stored on a user.
func (s *Store) PlanOf(ctx context.Context, userID string) (plans.Account, error) {
	var (
		plan    string
		source  *string
		expires *time.Time
	)
	err := s.pool.QueryRow(ctx, `SELECT `+planCols+` FROM users WHERE id = $1`, userID).Scan(&plan, &source, &expires)
	if err != nil {
		return plans.Account{}, notFound(err)
	}
	return scanPlan(plan, source, expires), nil
}

// SetPlan stores a user's plan. A manual (or unspecified) source sets the
// admin grant and recomputes the plan from all billing sources, so it never
// cancels a Stripe or Patreon grant. Other sources write users directly; a
// Free plan clears the source and expiry.
func (s *Store) SetPlan(ctx context.Context, userID string, a plans.Account) error {
	if a.Source == plans.Manual || a.Source == "" {
		return s.SetManualPlan(ctx, userID, a.Plan == plans.Pro, a.ExpiresAt)
	}
	var source *string
	if a.Plan == plans.Free {
		a.ExpiresAt = nil
	} else if a.Source != "" {
		v := string(a.Source)
		source = &v
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET plan = $2, plan_source = $3, plan_expires_at = $4, plan_updated_at = now()
		WHERE id = $1`, userID, string(a.Plan), source, a.ExpiresAt)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// PlanUser is a row in the admin user list.
type PlanUser struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	EmailVerified bool       `json:"email_verified"`
	Suspended     bool       `json:"suspended"`
	CreatedAt     time.Time  `json:"created_at"`
	Plan          string     `json:"plan"`
	PlanSource    *string    `json:"plan_source"`
	PlanExpiresAt *time.Time `json:"plan_expires_at"`
	Pages         int        `json:"pages"`
}

// Account returns the row's stored plan.
func (u PlanUser) Account() plans.Account { return scanPlan(u.Plan, u.PlanSource, u.PlanExpiresAt) }

// PlanUsers lists accounts whose email contains query (all accounts when it
// is empty): Pro accounts first, then newest first.
func (s *Store) PlanUsers(ctx context.Context, query string, limit int) ([]PlanUser, error) {
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(query)) + "%"
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.email, u.email_verified_at IS NOT NULL, u.suspended_at IS NOT NULL, u.created_at,
			u.plan, u.plan_source, u.plan_expires_at, (SELECT count(*) FROM pages p WHERE p.user_id = u.id)
		FROM users u WHERE lower(u.email) LIKE $1
		ORDER BY u.plan = 'pro' DESC, u.created_at DESC LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanUser{}
	for rows.Next() {
		var u PlanUser
		if err := rows.Scan(&u.ID, &u.Email, &u.EmailVerified, &u.Suspended, &u.CreatedAt,
			&u.Plan, &u.PlanSource, &u.PlanExpiresAt, &u.Pages); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreatePageLimit inserts a page unless the user already has max pages.
func (s *Store) CreatePageLimit(ctx context.Context, userID, slug, title string, theme json.RawMessage, max int) (*Page, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Serialize page creation per user so the limit cannot be raced.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pages WHERE user_id = $1`, userID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= max {
		return nil, ErrLimit
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO pages (user_id, slug, title, theme) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, slug, title, theme).Scan(&id)
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.PageByID(ctx, id)
}

// ---- custom domains and plans ----

// ErrAccountLimit means the account already has as many domains as its plan allows.
var ErrAccountLimit = errors.New("account limit reached")

// CreateDomainLimit attaches a domain unless the owner already has max
// domains across all pages (ErrAccountLimit) or the page has 3 (ErrLimit).
func (s *Store) CreateDomainLimit(ctx context.Context, userID, pageID, host string, max int) (*Domain, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM domains d JOIN pages p ON p.id = d.page_id WHERE p.user_id = $1`,
		userID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= max {
		return nil, ErrAccountLimit
	}
	tok, _ := NewToken()
	d, err := scanDomain(tx.QueryRow(ctx, `INSERT INTO domains (page_id, hostname, verify_token)
		SELECT $1, $2, $3 WHERE (SELECT count(*) FROM domains WHERE page_id = $1) < 3 RETURNING `+domainCols, pageID, host, tok[:24]))
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		if errors.Is(err, ErrNotFound) {
			return nil, ErrLimit
		}
		return nil, err
	}
	return d, tx.Commit(ctx)
}

// GrandfatheredDomains returns the IDs of a page's domains that predate
// plans and are served on any plan.
func (s *Store) GrandfatheredDomains(ctx context.Context, pageID string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM domains WHERE page_id = $1 AND grandfathered`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// VerifiedDomainOwner returns, for a verified hostname, whether the domain
// predates plans and its page owner's plan.
func (s *Store) VerifiedDomainOwner(ctx context.Context, host string) (grandfathered bool, owner plans.Account, err error) {
	var (
		plan    string
		source  *string
		expires *time.Time
	)
	err = s.pool.QueryRow(ctx, `SELECT d.grandfathered, u.plan, u.plan_source, u.plan_expires_at
		FROM domains d JOIN pages p ON p.id = d.page_id JOIN users u ON u.id = p.user_id
		WHERE d.hostname = $1 AND d.verified_at IS NOT NULL`, host).Scan(&grandfathered, &plan, &source, &expires)
	if err != nil {
		return false, plans.Account{}, notFound(err)
	}
	return grandfathered, scanPlan(plan, source, expires), nil
}
