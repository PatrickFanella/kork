package store

import (
	"context"
	"time"
)

// ---- publish gate ----

// UserTrustedAt returns when an admin approved the account (or when the
// account was grandfathered in), or nil if it has not been approved.
func (s *Store) UserTrustedAt(ctx context.Context, userID string) (*time.Time, error) {
	var t *time.Time
	err := s.pool.QueryRow(ctx, `SELECT trusted_at FROM users WHERE id = $1`, userID).Scan(&t)
	return t, notFound(err)
}

// TrustUser lets an account publish regardless of its age.
func (s *Store) TrustUser(ctx context.Context, userID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET trusted_at = COALESCE(trusted_at, now()) WHERE id = $1`, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// PendingUser is an account still inside the new-account publish gate.
type PendingUser struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	UnlocksAt     time.Time `json:"publish_unlocks_at"`
	Pages         []string  `json:"pages"`
}

// PendingUsers lists untrusted, unsuspended, non-admin accounts younger than
// minAge, newest first.
func (s *Store) PendingUsers(ctx context.Context, minAge time.Duration) ([]PendingUser, error) {
	out := []PendingUser{}
	if minAge <= 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.email, u.email_verified_at IS NOT NULL, u.created_at,
			COALESCE(array_agg(p.slug ORDER BY p.created_at) FILTER (WHERE p.id IS NOT NULL), '{}')
		FROM users u LEFT JOIN pages p ON p.user_id = u.id
		WHERE u.trusted_at IS NULL AND u.suspended_at IS NULL AND NOT u.is_admin
			AND u.created_at > now() - make_interval(secs => $1)
		GROUP BY u.id ORDER BY u.created_at DESC LIMIT 200`, minAge.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u PendingUser
		if err := rows.Scan(&u.ID, &u.Email, &u.EmailVerified, &u.CreatedAt, &u.Pages); err != nil {
			return nil, err
		}
		u.UnlocksAt = u.CreatedAt.Add(minAge)
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---- destination screening ----

// Verdict is a cached screening result for one URL.
type Verdict struct {
	URL    string
	Unsafe bool
	Threat string
	Source string
}

// CachedVerdicts returns unexpired verdicts for the given URLs.
func (s *Store) CachedVerdicts(ctx context.Context, urls []string) (map[string]Verdict, error) {
	out := map[string]Verdict{}
	if len(urls) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT url, unsafe, threat, source FROM screen_verdicts
		WHERE url = ANY($1) AND expires_at > now()`, urls)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Verdict
		if err := rows.Scan(&v.URL, &v.Unsafe, &v.Threat, &v.Source); err != nil {
			return nil, err
		}
		out[v.URL] = v
	}
	return out, rows.Err()
}

// CacheVerdict stores a verdict until ttl from now.
func (s *Store) CacheVerdict(ctx context.Context, v Verdict, ttl time.Duration) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO screen_verdicts (url, unsafe, threat, source, checked_at, expires_at)
		VALUES ($1, $2, $3, $4, now(), now() + make_interval(secs => $5))
		ON CONFLICT (url) DO UPDATE SET unsafe = EXCLUDED.unsafe, threat = EXCLUDED.threat,
			source = EXCLUDED.source, checked_at = EXCLUDED.checked_at, expires_at = EXCLUDED.expires_at`,
		v.URL, v.Unsafe, v.Threat, v.Source, ttl.Seconds())
	return err
}

// PurgeExpiredVerdicts deletes stale cache rows.
func (s *Store) PurgeExpiredVerdicts(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM screen_verdicts WHERE expires_at <= now()`)
	return err
}

// PagesToRescreen returns up to limit live pages, least recently screened first.
func (s *Store) PagesToRescreen(ctx context.Context, limit int) ([]*Page, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+pageCols+` FROM pages p JOIN users u ON u.id = p.user_id
		LEFT JOIN page_screenings ps ON ps.page_id = p.id
		WHERE p.published AND p.suspended_at IS NULL AND u.suspended_at IS NULL
		ORDER BY ps.checked_at NULLS FIRST, p.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Page
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkPageScreened records that a page's destinations were just checked.
func (s *Store) MarkPageScreened(ctx context.Context, pageID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO page_screenings (page_id, checked_at) VALUES ($1, now())
		ON CONFLICT (page_id) DO UPDATE SET checked_at = now()`, pageID)
	return err
}

// UnpublishForAbuse takes a page offline and files a report for admins, in
// one transaction. It reports false if the page was already unpublished.
func (s *Store) UnpublishForAbuse(ctx context.Context, pageID, reason, details string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `UPDATE pages SET published = false, updated_at = now() WHERE id = $1 AND published`, pageID)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO reports (page_id, reason, details) VALUES ($1, $2, $3)`, pageID, reason, details); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
