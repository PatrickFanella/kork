package store

import (
	"context"
	"time"
)

// CloudflareState is a domain's Cloudflare for SaaS custom hostname as last
// read from the API. HostnameID is nil when the domain isn't registered with
// Cloudflare (DNS mode, or added before Cloudflare mode was enabled).
type CloudflareState struct {
	HostnameID *string
	Status     string
	SSLStatus  string
	// Details is JSON owned by the server package (records and messages to show).
	Details   []byte
	CheckedAt *time.Time
}

// SaveCloudflareState stores the Cloudflare state of a domain as read at
// time at and, when verified is true, marks it verified (keeping an earlier
// verification time).
func (s *Store) SaveCloudflareState(ctx context.Context, id string, st CloudflareState, verified bool, at time.Time) error {
	details := st.Details
	if len(details) == 0 {
		details = []byte("{}")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE domains SET cf_hostname_id = $2, cf_status = $3, cf_ssl_status = $4,
		cf_details = $5::jsonb, cf_checked_at = $7,
		verified_at = CASE WHEN $6 THEN coalesce(verified_at, $7) ELSE verified_at END
		WHERE id = $1`, id, st.HostnameID, st.Status, st.SSLStatus, string(details), verified, at)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// PendingCloudflareDomains returns up to limit unverified domains registered
// with Cloudflare that weren't checked since before, oldest check first.
// Domains Cloudflare no longer has (status "missing") are skipped.
func (s *Store) PendingCloudflareDomains(ctx context.Context, before time.Time, limit int) ([]*Domain, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+domainCols+` FROM domains
		WHERE cf_hostname_id IS NOT NULL AND verified_at IS NULL AND cf_status <> 'missing'
		  AND (cf_checked_at IS NULL OR cf_checked_at < $1)
		ORDER BY cf_checked_at NULLS FIRST LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CloudflareHostnameIDs returns the Cloudflare hostname IDs of the domains on
// a page (pageID set) or on all of a user's pages (userID set), so they can
// be removed from Cloudflare before the rows are deleted.
func (s *Store) CloudflareHostnameIDs(ctx context.Context, pageID, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.cf_hostname_id FROM domains d JOIN pages p ON p.id = d.page_id
		WHERE d.cf_hostname_id IS NOT NULL
		  AND (($1 <> '' AND d.page_id::text = $1) OR ($2 <> '' AND p.user_id::text = $2))`, pageID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
