package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"git.subcult.tv/subculture-collective/zelda/internal/plans"
)

// ---- billing ----

// ErrNoBillingAccount means a provider event names no known Kork account.
var ErrNoBillingAccount = errors.New("no account for billing event")

// Billing is one account's per-source plan state (migration 0011).
type Billing struct {
	UserID string

	ManualPro       bool
	ManualExpiresAt *time.Time

	StripeCustomerID     string
	StripeSubscriptionID string
	StripeStatus         string
	StripePriceID        string
	StripePeriodEnd      *time.Time
	StripeCancelAtEnd    bool
	StripePaymentFailed  *time.Time
	StripeEventAt        *time.Time
	StripeProUntil       *time.Time

	PatreonUserID       string
	PatreonRefreshToken []byte // sealed
	PatreonPatronStatus string
	PatreonEntitled     bool
	PatreonCheckedAt    *time.Time
	PatreonLostAt       *time.Time
	PatreonProUntil     *time.Time
}

// Grants lists the Pro grants this state holds, for plans.Resolve.
func (b *Billing) Grants() []plans.Grant {
	var g []plans.Grant
	if b.ManualPro {
		g = append(g, plans.Grant{Source: plans.Manual, Until: b.ManualExpiresAt})
	}
	if b.StripeProUntil != nil {
		g = append(g, plans.Grant{Source: plans.Stripe, Until: b.StripeProUntil})
	}
	if b.PatreonProUntil != nil {
		g = append(g, plans.Grant{Source: plans.Patreon, Until: b.PatreonProUntil})
	}
	return g
}

// ClearPatreon removes the Patreon connection and its grant.
func (b *Billing) ClearPatreon() {
	b.PatreonUserID, b.PatreonRefreshToken, b.PatreonPatronStatus = "", nil, ""
	b.PatreonEntitled = false
	b.PatreonCheckedAt, b.PatreonLostAt, b.PatreonProUntil = nil, nil, nil
}

const billingCols = `user_id, manual_pro, manual_expires_at,
	coalesce(stripe_customer_id, ''), coalesce(stripe_subscription_id, ''), coalesce(stripe_status, ''), coalesce(stripe_price_id, ''),
	stripe_current_period_end, stripe_cancel_at_period_end, stripe_payment_failed_at, stripe_event_at, stripe_pro_until,
	coalesce(patreon_user_id, ''), patreon_refresh_token, coalesce(patreon_patron_status, ''), patreon_entitled,
	patreon_checked_at, patreon_lost_at, patreon_pro_until`

func scanBilling(row pgx.Row) (*Billing, error) {
	var b Billing
	err := row.Scan(&b.UserID, &b.ManualPro, &b.ManualExpiresAt,
		&b.StripeCustomerID, &b.StripeSubscriptionID, &b.StripeStatus, &b.StripePriceID,
		&b.StripePeriodEnd, &b.StripeCancelAtEnd, &b.StripePaymentFailed, &b.StripeEventAt, &b.StripeProUntil,
		&b.PatreonUserID, &b.PatreonRefreshToken, &b.PatreonPatronStatus, &b.PatreonEntitled,
		&b.PatreonCheckedAt, &b.PatreonLostAt, &b.PatreonProUntil)
	if err != nil {
		return nil, notFound(err)
	}
	return &b, nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// BillingOf returns an account's billing state; an account without a row
// gets empty state.
func (s *Store) BillingOf(ctx context.Context, userID string) (*Billing, error) {
	b, err := scanBilling(s.pool.QueryRow(ctx, `SELECT `+billingCols+` FROM billing WHERE user_id = $1`, userID))
	if errors.Is(err, ErrNotFound) {
		if _, err := s.PlanOf(ctx, userID); err != nil {
			return nil, err
		}
		return &Billing{UserID: userID}, nil
	}
	return b, err
}

// lockBilling locks the user and their billing row (creating it) in tx.
// Every billing change takes the user lock first, so plan recomputation
// never interleaves.
func lockBilling(ctx context.Context, tx pgx.Tx, userID string) (*Billing, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO billing (user_id) SELECT id FROM users WHERE id = $1 ON CONFLICT DO NOTHING`, userID); err != nil {
		return nil, err
	}
	return scanBilling(tx.QueryRow(ctx, `SELECT `+billingCols+` FROM billing WHERE user_id = $1 FOR UPDATE`, userID))
}

// saveBilling writes b and recomputes the user's plan from every source.
func saveBilling(ctx context.Context, tx pgx.Tx, b *Billing) error {
	_, err := tx.Exec(ctx, `UPDATE billing SET manual_pro = $2, manual_expires_at = $3,
			stripe_customer_id = $4, stripe_subscription_id = $5, stripe_status = $6, stripe_price_id = $7,
			stripe_current_period_end = $8, stripe_cancel_at_period_end = $9, stripe_payment_failed_at = $10,
			stripe_event_at = $11, stripe_pro_until = $12,
			patreon_user_id = $13, patreon_refresh_token = $14, patreon_patron_status = $15, patreon_entitled = $16,
			patreon_checked_at = $17, patreon_lost_at = $18, patreon_pro_until = $19, updated_at = now()
		WHERE user_id = $1`,
		b.UserID, b.ManualPro, b.ManualExpiresAt,
		nullable(b.StripeCustomerID), nullable(b.StripeSubscriptionID), nullable(b.StripeStatus), nullable(b.StripePriceID),
		b.StripePeriodEnd, b.StripeCancelAtEnd, b.StripePaymentFailed, b.StripeEventAt, b.StripeProUntil,
		nullable(b.PatreonUserID), b.PatreonRefreshToken, nullable(b.PatreonPatronStatus), b.PatreonEntitled,
		b.PatreonCheckedAt, b.PatreonLostAt, b.PatreonProUntil)
	if err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	a := plans.Resolve(b.Grants())
	var source *string
	if a.Source != "" {
		source = nullable(string(a.Source))
	}
	_, err = tx.Exec(ctx, `UPDATE users SET plan = $2, plan_source = $3, plan_expires_at = $4, plan_updated_at = now()
		WHERE id = $1 AND (plan, plan_source, plan_expires_at) IS DISTINCT FROM ($2, $3, $4)`,
		b.UserID, string(a.Plan), source, a.ExpiresAt)
	return err
}

// UpdateBilling changes an account's billing state with fn and recomputes
// its plan, in one transaction. ErrNotFound if the user doesn't exist;
// ErrConflict if a provider account is already linked to another user.
func (s *Store) UpdateBilling(ctx context.Context, userID string, fn func(*Billing) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	b, err := lockBilling(ctx, tx, userID)
	if err != nil {
		return err
	}
	if err := fn(b); err != nil {
		return err
	}
	if err := saveBilling(ctx, tx, b); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetManualPlan sets or clears the admin grant. Other sources are kept, so
// the resulting plan can still be Pro.
func (s *Store) SetManualPlan(ctx context.Context, userID string, pro bool, expires *time.Time) error {
	return s.UpdateBilling(ctx, userID, func(b *Billing) error {
		b.ManualPro = pro
		b.ManualExpiresAt = nil
		if pro {
			b.ManualExpiresAt = expires
		}
		return nil
	})
}

// ---- Stripe ----

// StripeEventSeen reports whether a Stripe event was already handled.
func (s *Store) StripeEventSeen(ctx context.Context, id string) (bool, error) {
	var seen bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stripe_events WHERE id = $1)`, id).Scan(&seen)
	return seen, err
}

// StripeTarget names the account a Stripe event is for, by any of the
// identifiers the event carries. UserID comes from client_reference_id or
// metadata and is tried first.
type StripeTarget struct {
	UserID         string
	CustomerID     string
	SubscriptionID string
}

// ApplyStripeEvent records the event and applies fn to the target account's
// billing state, in one transaction. A duplicate event is skipped and
// returns applied=false. An event for no known account is recorded and
// returns ErrNoBillingAccount.
func (s *Store) ApplyStripeEvent(ctx context.Context, eventID, eventType string, t StripeTarget, fn func(*Billing) error) (applied bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `INSERT INTO stripe_events (id, type) VALUES ($1, $2) ON CONFLICT DO NOTHING`, eventID, eventType)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	userID, err := stripeUser(ctx, tx, t)
	if errors.Is(err, ErrNotFound) {
		return true, errors.Join(ErrNoBillingAccount, tx.Commit(ctx))
	}
	if err != nil {
		return false, err
	}
	b, err := lockBilling(ctx, tx, userID)
	if err != nil {
		return false, err
	}
	if err := fn(b); err != nil {
		return false, err
	}
	if err := saveBilling(ctx, tx, b); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func stripeUser(ctx context.Context, tx pgx.Tx, t StripeTarget) (string, error) {
	if uuidLike(t.UserID) {
		var id string
		err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1`, t.UserID).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	for _, q := range []struct{ col, val string }{{"stripe_customer_id", t.CustomerID}, {"stripe_subscription_id", t.SubscriptionID}} {
		if q.val == "" {
			continue
		}
		var id string
		err := tx.QueryRow(ctx, `SELECT user_id FROM billing WHERE `+q.col+` = $1`, q.val).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	return "", ErrNotFound
}

func uuidLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
		default:
			return false
		}
	}
	return true
}

// PruneStripeEvents forgets handled event IDs older than before. Stripe
// stops retrying an event after three days.
func (s *Store) PruneStripeEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM stripe_events WHERE processed_at < $1`, before)
	return tag.RowsAffected(), err
}

// ---- Patreon ----

// PatreonLink is a connected account due for a membership check.
type PatreonLink struct {
	UserID       string
	RefreshToken []byte // sealed
}

// PatreonDue lists connected accounts last checked before the given time
// (or never), oldest first.
func (s *Store) PatreonDue(ctx context.Context, before time.Time, limit int) ([]PatreonLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id, patreon_refresh_token FROM billing
		WHERE patreon_user_id IS NOT NULL AND (patreon_checked_at IS NULL OR patreon_checked_at < $1)
		ORDER BY patreon_checked_at NULLS FIRST LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PatreonLink
	for rows.Next() {
		var l PatreonLink
		if err := rows.Scan(&l.UserID, &l.RefreshToken); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
