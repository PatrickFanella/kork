package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// EmailChangeTTL is how long a link to confirm a new email address works.
const EmailChangeTTL = 24 * time.Hour

// CreateUserAccepting inserts a user and records the terms version they agreed to.
func (s *Store) CreateUserAccepting(ctx context.Context, email, hash, termsVersion string) (*User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, terms_version, terms_accepted_at)
		VALUES ($1, $2, $3, now()) RETURNING `+userCols, email, hash, termsVersion))
	if err != nil && isUnique(err) {
		return nil, ErrConflict
	}
	return u, err
}

// Account is the settings view of a user.
type Account struct {
	User            *User      `json:"user"`
	PendingEmail    *string    `json:"pending_email"`
	TermsVersion    *string    `json:"terms_version"`
	TermsAcceptedAt *time.Time `json:"terms_accepted_at"`
}

func (s *Store) Account(ctx context.Context, u *User) (*Account, error) {
	a := &Account{User: u}
	if err := s.pool.QueryRow(ctx, `SELECT terms_version, terms_accepted_at FROM users WHERE id = $1`, u.ID).
		Scan(&a.TermsVersion, &a.TermsAcceptedAt); err != nil {
		return nil, notFound(err)
	}
	var pending string
	err := s.pool.QueryRow(ctx, `SELECT new_email FROM email_changes
		WHERE user_id = $1 AND used_at IS NULL AND expires_at > now() ORDER BY created_at DESC LIMIT 1`, u.ID).Scan(&pending)
	if err == nil {
		a.PendingEmail = &pending
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return a, nil
}

// DeleteOtherSessions signs a user out everywhere except the session keepTok.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID, keepTok string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, userID, HashToken(keepTok))
	return err
}

// EmailInUse reports whether another account already uses email.
func (s *Store) EmailInUse(ctx context.Context, email string) (bool, error) {
	var used bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = lower($1))`, email).Scan(&used)
	return used, err
}

// CreateEmailChange replaces any pending change for the user and returns the
// token to email to the new address.
func (s *Store) CreateEmailChange(ctx context.Context, userID, newEmail string) (string, error) {
	tok, hash := NewToken()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM email_changes WHERE user_id = $1`, userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO email_changes (token_hash, user_id, new_email, expires_at) VALUES ($1, $2, $3, $4)`,
		hash, userID, newEmail, time.Now().Add(EmailChangeTTL)); err != nil {
		return "", err
	}
	return tok, tx.Commit(ctx)
}

// ConfirmEmailChange switches the account to the new address and marks it
// verified. It returns the user ID and ErrConflict if another account took
// the address in the meantime.
func (s *Store) ConfirmEmailChange(ctx context.Context, tok string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var userID, email string
	err = tx.QueryRow(ctx, `UPDATE email_changes SET used_at = now()
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING user_id, new_email`,
		HashToken(tok)).Scan(&userID, &email)
	if err != nil {
		return "", notFound(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET email = $2, email_verified_at = now() WHERE id = $1`, userID, email); err != nil {
		if isUnique(err) {
			return "", ErrConflict
		}
		return "", err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM email_changes WHERE user_id = $1`, userID); err != nil {
		return "", err
	}
	return userID, tx.Commit(ctx)
}

// UploadsByUser lists a user's uploads, oldest first.
func (s *Store) UploadsByUser(ctx context.Context, userID string) ([]*Upload, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, content_type, size_bytes FROM uploads
		WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Upload{}
	for rows.Next() {
		var u Upload
		if err := rows.Scan(&u.ID, &u.UserID, &u.ContentType, &u.Size); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

// StatRow is one stored daily count. BlockID is ZeroBlockID for page views.
type StatRow struct {
	BlockID string `json:"block_id"`
	Day     string `json:"day"`
	Count   int64  `json:"count"`
}

// StatRows returns every stored daily count for a page.
func (s *Store) StatRows(ctx context.Context, pageID string) ([]StatRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT block_id, to_char(day, 'YYYY-MM-DD'), count FROM stats_daily
		WHERE page_id = $1 ORDER BY day, block_id`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatRow{}
	for rows.Next() {
		var r StatRow
		if err := rows.Scan(&r.BlockID, &r.Day, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteUser removes the account and everything that cascades from it:
// sessions, tokens, email changes, uploads rows, pages, blocks, domains,
// reports about its pages and stats. It returns the upload IDs so the caller
// can remove the files.
func (s *Store) DeleteUser(ctx context.Context, userID string) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `SELECT id FROM uploads WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return ids, tx.Commit(ctx)
}
