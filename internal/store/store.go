// Package store holds the Postgres queries.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("already exists")
	ErrLimit      = errors.New("limit reached")
	ErrBadOrder   = errors.New("order must list every block exactly once")
	ZeroBlockID   = "00000000-0000-0000-0000-000000000000"
	MaxPages      = 5
	MaxBlocks     = 100
	SessionTTL    = 30 * 24 * time.Hour
	TokenTTL      = 24 * time.Hour
	ResetTokenTTL = time.Hour
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// NewToken returns a random URL-safe token and its SHA-256 hash for storage.
func NewToken() (string, []byte) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	return tok, HashToken(tok)
}

func HashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

// ---- users & sessions ----

type User struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	PasswordHash  string    `json:"-"`
	EmailVerified bool      `json:"email_verified"`
	IsAdmin       bool      `json:"is_admin"`
	Suspended     bool      `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
}

const userCols = `id, email, password_hash, email_verified_at IS NOT NULL, is_admin, suspended_at IS NOT NULL, created_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.EmailVerified, &u.IsAdmin, &u.Suspended, &u.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

// CreateUser inserts a user. Admin rights come from ZELDA_ADMIN_EMAILS or the
// is_admin column, never from signup order.
func (s *Store) CreateUser(ctx context.Context, email, hash string) (*User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash)
		VALUES ($1, $2) RETURNING `+userCols, email, hash))
	if err != nil && isUnique(err) {
		return nil, ErrConflict
	}
	return u, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE lower(email) = lower($1)`, email))
}

func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (s *Store) CreateSession(ctx context.Context, userID string) (string, error) {
	tok, hash := NewToken()
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hash, userID, time.Now().Add(SessionTTL))
	return tok, err
}

func (s *Store) UserBySession(ctx context.Context, tok string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+prefixed("u.")+` FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now() AND u.suspended_at IS NULL`, HashToken(tok)))
}

func prefixed(p string) string {
	return p + `id, ` + p + `email, ` + p + `password_hash, ` + p + `email_verified_at IS NOT NULL, ` + p + `is_admin, ` + p + `suspended_at IS NOT NULL, ` + p + `created_at`
}

func (s *Store) DeleteSession(ctx context.Context, tok string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, HashToken(tok))
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (s *Store) CreateUserToken(ctx context.Context, userID, purpose string, ttl time.Duration) (string, error) {
	tok, hash := NewToken()
	_, err := s.pool.Exec(ctx, `INSERT INTO user_tokens (token_hash, user_id, purpose, expires_at) VALUES ($1, $2, $3, $4)`,
		hash, userID, purpose, time.Now().Add(ttl))
	return tok, err
}

// UseUserToken marks a token used and returns its user ID.
func (s *Store) UseUserToken(ctx context.Context, tok, purpose string) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx, `UPDATE user_tokens SET used_at = now()
		WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > now() RETURNING user_id`,
		HashToken(tok), purpose).Scan(&userID)
	return userID, notFound(err)
}

func (s *Store) MarkEmailVerified(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = $1`, userID)
	return err
}

func (s *Store) SetPassword(ctx context.Context, userID, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash)
	return err
}

func (s *Store) SetUserSuspended(ctx context.Context, userID string, suspended bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET suspended_at = CASE WHEN $2 THEN now() ELSE NULL END WHERE id = $1`, userID, suspended)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err == nil && suspended {
		err = s.DeleteUserSessions(ctx, userID)
	}
	return err
}

// ---- uploads ----

type Upload struct {
	ID          string `json:"id"`
	UserID      string `json:"-"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

func (s *Store) CreateUpload(ctx context.Context, userID, contentType string, size int) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `INSERT INTO uploads (user_id, content_type, size_bytes) VALUES ($1, $2, $3) RETURNING id`,
		userID, contentType, size).Scan(&id)
	return id, err
}

func (s *Store) DeleteUpload(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM uploads WHERE id = $1`, id)
	return err
}

func (s *Store) Upload(ctx context.Context, id string) (*Upload, error) {
	var u Upload
	err := s.pool.QueryRow(ctx, `SELECT id, user_id, content_type, size_bytes FROM uploads WHERE id = $1`, id).
		Scan(&u.ID, &u.UserID, &u.ContentType, &u.Size)
	if err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

func (s *Store) OwnsUpload(ctx context.Context, userID, id string) bool {
	var ok bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM uploads WHERE id = $1 AND user_id = $2)`, id, userID).Scan(&ok)
	return ok
}

// ---- pages ----

type Page struct {
	ID             string          `json:"id"`
	UserID         string          `json:"-"`
	Slug           string          `json:"slug"`
	Title          string          `json:"title"`
	Bio            string          `json:"bio"`
	AvatarUploadID *string         `json:"avatar_upload_id"`
	Theme          json.RawMessage `json:"theme"`
	Published      bool            `json:"published"`
	Suspended      bool            `json:"suspended"`
	OwnerSuspended bool            `json:"-"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

const pageCols = `p.id, p.user_id, p.slug, p.title, p.bio, p.avatar_upload_id, p.theme, p.published,
	p.suspended_at IS NOT NULL, u.suspended_at IS NOT NULL, p.created_at, p.updated_at`

func scanPage(row pgx.Row) (*Page, error) {
	var p Page
	err := row.Scan(&p.ID, &p.UserID, &p.Slug, &p.Title, &p.Bio, &p.AvatarUploadID, &p.Theme, &p.Published,
		&p.Suspended, &p.OwnerSuspended, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

// Live reports whether the public may see the page.
func (p *Page) Live() bool { return p.Published && !p.Suspended && !p.OwnerSuspended }

// CreatePage inserts a page within the default MaxPages limit. Handlers use
// CreatePageLimit with the owner's plan limit.
func (s *Store) CreatePage(ctx context.Context, userID, slug, title string, theme json.RawMessage) (*Page, error) {
	return s.CreatePageLimit(ctx, userID, slug, title, theme, MaxPages)
}

func (s *Store) PageByID(ctx context.Context, id string) (*Page, error) {
	return scanPage(s.pool.QueryRow(ctx, `SELECT `+pageCols+` FROM pages p JOIN users u ON u.id = p.user_id WHERE p.id = $1`, id))
}

func (s *Store) PageBySlug(ctx context.Context, slug string) (*Page, error) {
	return scanPage(s.pool.QueryRow(ctx, `SELECT `+pageCols+` FROM pages p JOIN users u ON u.id = p.user_id WHERE p.slug = $1`, slug))
}

func (s *Store) PageByDomain(ctx context.Context, host string) (*Page, error) {
	return scanPage(s.pool.QueryRow(ctx, `SELECT `+pageCols+` FROM domains d JOIN pages p ON p.id = d.page_id
		JOIN users u ON u.id = p.user_id WHERE d.hostname = $1 AND d.verified_at IS NOT NULL`, host))
}

func (s *Store) PagesByUser(ctx context.Context, userID string) ([]*Page, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+pageCols+` FROM pages p JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $1 ORDER BY p.created_at`, userID)
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

func (s *Store) SlugTaken(ctx context.Context, slug string) (bool, error) {
	var taken bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pages WHERE slug = $1)`, slug).Scan(&taken)
	return taken, err
}

// UpdatePage writes all mutable page fields.
func (s *Store) UpdatePage(ctx context.Context, p *Page) error {
	_, err := s.pool.Exec(ctx, `UPDATE pages SET slug = $2, title = $3, bio = $4, avatar_upload_id = $5,
		theme = $6, published = $7, updated_at = now() WHERE id = $1`,
		p.ID, p.Slug, p.Title, p.Bio, p.AvatarUploadID, p.Theme, p.Published)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) TouchPage(ctx context.Context, id string) {
	_, _ = s.pool.Exec(ctx, `UPDATE pages SET updated_at = now() WHERE id = $1`, id)
}

func (s *Store) DeletePage(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM pages WHERE id = $1`, id)
	return err
}

func (s *Store) SetPageSuspended(ctx context.Context, id string, suspended bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE pages SET suspended_at = CASE WHEN $2 THEN now() ELSE NULL END WHERE id = $1`, id, suspended)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---- blocks ----

type Block struct {
	ID       string          `json:"id"`
	PageID   string          `json:"-"`
	Type     string          `json:"type"`
	Data     json.RawMessage `json:"data"`
	Enabled  bool            `json:"enabled"`
	Position int             `json:"position"`
	Label    string          `json:"label"`
	Schedule
}

const blockCols = `id, page_id, type, data, enabled, position, starts_at, ends_at`

func scanBlock(row pgx.Row) (*Block, error) {
	var b Block
	if err := row.Scan(&b.ID, &b.PageID, &b.Type, &b.Data, &b.Enabled, &b.Position, &b.StartsAt, &b.EndsAt); err != nil {
		return nil, notFound(err)
	}
	return &b, nil
}

func (s *Store) Blocks(ctx context.Context, pageID string) ([]*Block, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+blockCols+` FROM blocks WHERE page_id = $1 ORDER BY position, created_at`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Block{}
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) Block(ctx context.Context, pageID, id string) (*Block, error) {
	return scanBlock(s.pool.QueryRow(ctx, `SELECT `+blockCols+` FROM blocks WHERE page_id = $1 AND id = $2`, pageID, id))
}

// LiveBlock finds a block that visitors can see at now: enabled, inside its
// schedule window, and on a live page.
func (s *Store) LiveBlock(ctx context.Context, id string, now time.Time) (*Block, *Page, error) {
	var pageID string
	if err := s.pool.QueryRow(ctx, `SELECT page_id FROM blocks WHERE id = $1 AND enabled`, id).Scan(&pageID); err != nil {
		return nil, nil, notFound(err)
	}
	p, err := s.PageByID(ctx, pageID)
	if err != nil {
		return nil, nil, err
	}
	if !p.Live() {
		return nil, nil, ErrNotFound
	}
	b, err := s.Block(ctx, pageID, id)
	if err != nil {
		return nil, nil, err
	}
	if !b.VisibleAt(now) {
		return nil, nil, ErrNotFound
	}
	return b, p, nil
}

func (s *Store) CreateBlock(ctx context.Context, pageID, typ string, data json.RawMessage, enabled bool, sched Schedule) (*Block, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT 1 FROM pages WHERE id = $1 FOR UPDATE`, pageID); err != nil {
		return nil, err
	}
	var n, next int
	if err := tx.QueryRow(ctx, `SELECT count(*), COALESCE(max(position) + 1, 0) FROM blocks WHERE page_id = $1`, pageID).Scan(&n, &next); err != nil {
		return nil, err
	}
	if n >= MaxBlocks {
		return nil, ErrLimit
	}
	b, err := scanBlock(tx.QueryRow(ctx, `INSERT INTO blocks (page_id, position, type, data, enabled, starts_at, ends_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+blockCols, pageID, next, typ, data, enabled, sched.StartsAt, sched.EndsAt))
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE pages SET updated_at = now() WHERE id = $1`, pageID); err != nil {
		return nil, err
	}
	return b, tx.Commit(ctx)
}

func (s *Store) UpdateBlock(ctx context.Context, b *Block) error {
	_, err := s.pool.Exec(ctx, `UPDATE blocks SET data = $3, enabled = $4, starts_at = $5, ends_at = $6, updated_at = now()
		WHERE page_id = $1 AND id = $2`, b.PageID, b.ID, b.Data, b.Enabled, b.StartsAt, b.EndsAt)
	if err == nil {
		s.TouchPage(ctx, b.PageID)
	}
	return err
}

func (s *Store) DeleteBlock(ctx context.Context, pageID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM blocks WHERE page_id = $1 AND id = $2`, pageID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err == nil {
		s.TouchPage(ctx, pageID)
	}
	return err
}

// ReorderBlocks sets positions from ids, which must be exactly the page's blocks.
func (s *Store) ReorderBlocks(ctx context.Context, pageID string, ids []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT 1 FROM pages WHERE id = $1 FOR UPDATE`, pageID); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM blocks WHERE page_id = $1`, pageID).Scan(&count); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if len(ids) != count || len(seen) != count {
		return ErrBadOrder
	}
	tag, err := tx.Exec(ctx, `UPDATE blocks b SET position = o.ord - 1, updated_at = now()
		FROM unnest($2::uuid[]) WITH ORDINALITY AS o(id, ord) WHERE b.page_id = $1 AND b.id = o.id`, pageID, ids)
	if err != nil {
		return ErrBadOrder
	}
	if int(tag.RowsAffected()) != count {
		return ErrBadOrder
	}
	if _, err := tx.Exec(ctx, `UPDATE pages SET updated_at = now() WHERE id = $1`, pageID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---- domains ----

type Domain struct {
	ID          string    `json:"id"`
	PageID      string    `json:"-"`
	Hostname    string    `json:"hostname"`
	VerifyToken string    `json:"-"`
	Verified    bool      `json:"verified"`
	CreatedAt   time.Time `json:"created_at"`
}

const domainCols = `id, page_id, hostname, verify_token, verified_at IS NOT NULL, created_at`

func scanDomain(row pgx.Row) (*Domain, error) {
	var d Domain
	if err := row.Scan(&d.ID, &d.PageID, &d.Hostname, &d.VerifyToken, &d.Verified, &d.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

func (s *Store) Domains(ctx context.Context, pageID string) ([]*Domain, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+domainCols+` FROM domains WHERE page_id = $1 ORDER BY created_at`, pageID)
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

func (s *Store) Domain(ctx context.Context, pageID, id string) (*Domain, error) {
	return scanDomain(s.pool.QueryRow(ctx, `SELECT `+domainCols+` FROM domains WHERE page_id = $1 AND id = $2`, pageID, id))
}

func (s *Store) CreateDomain(ctx context.Context, pageID, host string) (*Domain, error) {
	tok, _ := NewToken()
	d, err := scanDomain(s.pool.QueryRow(ctx, `INSERT INTO domains (page_id, hostname, verify_token)
		SELECT $1, $2, $3 WHERE (SELECT count(*) FROM domains WHERE page_id = $1) < 3 RETURNING `+domainCols, pageID, host, tok[:24]))
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		if errors.Is(err, ErrNotFound) {
			return nil, ErrLimit
		}
	}
	return d, err
}

func (s *Store) MarkDomainVerified(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE domains SET verified_at = now() WHERE id = $1`, id)
	return err
}

func (s *Store) DeleteDomain(ctx context.Context, pageID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM domains WHERE page_id = $1 AND id = $2`, pageID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DomainAllowed reports whether TLS may be issued for host.
func (s *Store) DomainAllowed(ctx context.Context, host string) bool {
	var ok bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM domains WHERE hostname = $1 AND verified_at IS NOT NULL)`, host).Scan(&ok)
	return ok
}

// ---- stats ----

func (s *Store) Count(ctx context.Context, pageID, blockID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO stats_daily (page_id, block_id, day, count) VALUES ($1, $2, current_date, 1)
		ON CONFLICT (page_id, block_id, day) DO UPDATE SET count = stats_daily.count + 1`, pageID, blockID)
	return err
}

type DayCount struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

type BlockCount struct {
	BlockID string          `json:"block_id"`
	Label   string          `json:"label"`
	Count   int64           `json:"count"`
	Type    string          `json:"-"`
	Data    json.RawMessage `json:"-"`
}

type Stats struct {
	TotalViews int64        `json:"total_views"`
	Views      []DayCount   `json:"views"`
	Blocks     []BlockCount `json:"blocks"`
}

// Stats returns daily views (zero-filled) and per-block clicks for the last days.
func (s *Store) Stats(ctx context.Context, pageID string, days int) (*Stats, error) {
	st := &Stats{Views: []DayCount{}, Blocks: []BlockCount{}}
	rows, err := s.pool.Query(ctx, `SELECT to_char(d, 'YYYY-MM-DD'), COALESCE(sd.count, 0)
		FROM generate_series(current_date - ($2::int - 1), current_date, interval '1 day') AS d
		LEFT JOIN stats_daily sd ON sd.page_id = $1 AND sd.block_id = $3 AND sd.day = d::date ORDER BY d`,
		pageID, days, ZeroBlockID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var dc DayCount
		if err := rows.Scan(&dc.Day, &dc.Count); err != nil {
			rows.Close()
			return nil, err
		}
		st.TotalViews += dc.Count
		st.Views = append(st.Views, dc)
	}
	rows.Close()
	rows, err = s.pool.Query(ctx, `SELECT b.id, b.type, b.data, SUM(sd.count) FROM stats_daily sd
		JOIN blocks b ON b.id = sd.block_id
		WHERE sd.page_id = $1 AND sd.day > current_date - $2::int GROUP BY b.id ORDER BY 4 DESC`, pageID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bc BlockCount
		if err := rows.Scan(&bc.BlockID, &bc.Type, &bc.Data, &bc.Count); err != nil {
			return nil, err
		}
		st.Blocks = append(st.Blocks, bc)
	}
	return st, rows.Err()
}

// ---- reports ----

type Report struct {
	ID        string    `json:"id"`
	PageID    string    `json:"page_id"`
	Slug      string    `json:"slug"`
	Reason    string    `json:"reason"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
	Resolved  bool      `json:"resolved"`
	Suspended bool      `json:"page_suspended"`
}

func (s *Store) CreateReport(ctx context.Context, pageID, reason, details string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO reports (page_id, reason, details) VALUES ($1, $2, $3)`, pageID, reason, details)
	return err
}

func (s *Store) Reports(ctx context.Context) ([]Report, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.page_id, p.slug, r.reason, r.details, r.created_at,
		r.resolved_at IS NOT NULL, p.suspended_at IS NOT NULL
		FROM reports r JOIN pages p ON p.id = r.page_id ORDER BY r.resolved_at IS NOT NULL, r.created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.PageID, &r.Slug, &r.Reason, &r.Details, &r.CreatedAt, &r.Resolved, &r.Suspended); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ResolveReport(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE reports SET resolved_at = now() WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
