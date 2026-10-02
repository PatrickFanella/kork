package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Email sign-up contacts and sealed Brevo keys (#11).

const (
	// SignupPendingTTL is how long a confirmation link works. Unconfirmed
	// addresses are deleted after it.
	SignupPendingTTL = 48 * time.Hour
	// MaxPendingSignups caps unconfirmed addresses per block.
	MaxPendingSignups = 1000
	// MaxSignupContacts caps confirmed addresses kept per block.
	MaxSignupContacts = 50000
)

// SignupContact is a confirmed address on a block's list.
type SignupContact struct {
	ID          string    `json:"id"`
	BlockID     string    `json:"block_id"`
	Email       string    `json:"email"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

// SignupSecret returns the sealed Brevo key for a block.
func (s *Store) SignupSecret(ctx context.Context, blockID string) ([]byte, error) {
	var box []byte
	err := s.pool.QueryRow(ctx, `SELECT brevo_key FROM signup_secrets WHERE block_id = $1`, blockID).Scan(&box)
	return box, notFound(err)
}

// SetSignupSecret stores a sealed Brevo key for a block.
func (s *Store) SetSignupSecret(ctx context.Context, blockID string, box []byte) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO signup_secrets (block_id, brevo_key) VALUES ($1, $2)
		ON CONFLICT (block_id) DO UPDATE SET brevo_key = excluded.brevo_key, updated_at = now()`, blockID, box)
	return err
}

// DeleteSignupSecret removes a block's Brevo key.
func (s *Store) DeleteSignupSecret(ctx context.Context, blockID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM signup_secrets WHERE block_id = $1`, blockID)
	return err
}

// RequestSignup records an unconfirmed address and returns the token for the
// confirmation link. Asking again before confirming replaces the token. An
// address already on the list returns ErrConflict and is left unchanged; a
// block with too many unconfirmed addresses returns ErrLimit.
func (s *Store) RequestSignup(ctx context.Context, blockID, email string, now time.Time) (string, error) {
	tok, hash := NewToken()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT 1 FROM blocks WHERE id = $1 FOR UPDATE`, blockID); err != nil {
		return "", err
	}
	var confirmed *time.Time
	err = tx.QueryRow(ctx, `SELECT confirmed_at FROM signup_contacts WHERE block_id = $1 AND lower(email) = lower($2)`,
		blockID, email).Scan(&confirmed)
	switch {
	case err == nil && confirmed != nil:
		return "", ErrConflict
	case err == nil:
		if _, err := tx.Exec(ctx, `UPDATE signup_contacts SET token_hash = $3, expires_at = $4
			WHERE block_id = $1 AND lower(email) = lower($2)`, blockID, email, hash, now.Add(SignupPendingTTL)); err != nil {
			return "", err
		}
	case errors.Is(err, pgx.ErrNoRows):
		var pending, total int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE confirmed_at IS NULL AND expires_at > $2), count(*)
			FROM signup_contacts WHERE block_id = $1`, blockID, now).Scan(&pending, &total); err != nil {
			return "", err
		}
		if pending >= MaxPendingSignups || total >= MaxSignupContacts {
			return "", ErrLimit
		}
		if _, err := tx.Exec(ctx, `INSERT INTO signup_contacts (block_id, email, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
			blockID, email, hash, now.Add(SignupPendingTTL)); err != nil {
			return "", err
		}
	default:
		return "", err
	}
	return tok, tx.Commit(ctx)
}

// ConfirmedSignup is a contact that has just confirmed.
type ConfirmedSignup struct {
	ID      string
	BlockID string
	Email   string
}

// ConfirmSignup marks the address behind tok as confirmed. Used and expired
// tokens return ErrNotFound.
func (s *Store) ConfirmSignup(ctx context.Context, tok string, now time.Time) (*ConfirmedSignup, error) {
	var c ConfirmedSignup
	err := s.pool.QueryRow(ctx, `UPDATE signup_contacts SET confirmed_at = $2, token_hash = NULL, expires_at = NULL
		WHERE token_hash = $1 AND confirmed_at IS NULL AND expires_at > $2 RETURNING id, block_id, email`,
		HashToken(tok), now).Scan(&c.ID, &c.BlockID, &c.Email)
	if err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

// SetSignupPendingPush marks a confirmed contact as waiting to be sent to Brevo.
func (s *Store) SetSignupPendingPush(ctx context.Context, id string, pending bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE signup_contacts SET pending_push = $2 WHERE id = $1`, id, pending)
	return err
}

// DeleteSignupContact removes one contact by ID.
func (s *Store) DeleteSignupContact(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM signup_contacts WHERE id = $1`, id)
	return err
}

// DeleteBlockContact removes a contact from a block's list.
func (s *Store) DeleteBlockContact(ctx context.Context, blockID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM signup_contacts WHERE block_id = $1 AND id = $2`, blockID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SignupCounts are the numbers shown beside a block's list.
type SignupCounts struct {
	// Pending addresses have not opened their confirmation link yet.
	Pending int `json:"pending"`
	// AwaitingPush are confirmed addresses Brevo has not accepted yet.
	AwaitingPush int `json:"awaiting_push"`
}

// SignupContacts lists a block's confirmed list (newest first), excluding
// addresses waiting to be sent to Brevo, with the other counts.
func (s *Store) SignupContacts(ctx context.Context, blockID string, now time.Time) ([]SignupContact, SignupCounts, error) {
	var counts SignupCounts
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE confirmed_at IS NULL AND expires_at > $2),
		count(*) FILTER (WHERE confirmed_at IS NOT NULL AND pending_push) FROM signup_contacts WHERE block_id = $1`,
		blockID, now).Scan(&counts.Pending, &counts.AwaitingPush); err != nil {
		return nil, counts, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, block_id, email, confirmed_at FROM signup_contacts
		WHERE block_id = $1 AND confirmed_at IS NOT NULL AND NOT pending_push ORDER BY confirmed_at DESC, id`, blockID)
	if err != nil {
		return nil, counts, err
	}
	list, err := pgx.CollectRows(rows, scanContact)
	if list == nil {
		list = []SignupContact{}
	}
	return list, counts, err
}

func scanContact(row pgx.CollectableRow) (SignupContact, error) {
	var c SignupContact
	err := row.Scan(&c.ID, &c.BlockID, &c.Email, &c.ConfirmedAt)
	return c, err
}

// PageSignupContacts lists the confirmed lists of every sign-up block on a
// page, for the account export.
func (s *Store) PageSignupContacts(ctx context.Context, pageID string) ([]SignupContact, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.block_id, c.email, c.confirmed_at FROM signup_contacts c
		JOIN blocks b ON b.id = c.block_id
		WHERE b.page_id = $1 AND c.confirmed_at IS NOT NULL AND NOT c.pending_push ORDER BY c.block_id, c.confirmed_at, c.id`, pageID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, scanContact)
	if list == nil {
		list = []SignupContact{}
	}
	return list, err
}

// PendingPush is a confirmed address still to be sent to Brevo.
type PendingPush struct {
	ConfirmedSignup
	ConfirmedAt time.Time
}

// PendingPushes lists confirmed contacts waiting for Brevo, oldest first.
func (s *Store) PendingPushes(ctx context.Context, limit int) ([]PendingPush, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, block_id, email, confirmed_at FROM signup_contacts
		WHERE pending_push AND confirmed_at IS NOT NULL ORDER BY confirmed_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (PendingPush, error) {
		var p PendingPush
		err := row.Scan(&p.ID, &p.BlockID, &p.Email, &p.ConfirmedAt)
		return p, err
	})
}

// PurgeExpiredSignups deletes addresses whose confirmation link expired.
func (s *Store) PurgeExpiredSignups(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM signup_contacts WHERE confirmed_at IS NULL AND expires_at <= $1`, now)
	return tag.RowsAffected(), err
}

// BlockByID loads a block and its page regardless of visibility.
func (s *Store) BlockByID(ctx context.Context, id string) (*Block, *Page, error) {
	var pageID string
	if err := s.pool.QueryRow(ctx, `SELECT page_id FROM blocks WHERE id = $1`, id).Scan(&pageID); err != nil {
		return nil, nil, notFound(err)
	}
	b, err := s.Block(ctx, pageID, id)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.PageByID(ctx, pageID)
	return b, p, err
}
