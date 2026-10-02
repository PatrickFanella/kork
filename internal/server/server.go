// Package server wires HTTP routes for the API, editor, and public pages.
package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/config"
	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/screen"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

const sessionCookie = "zelda_session"

// Resolver is the DNS subset used for custom domain verification.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type Server struct {
	cfg      *config.Config
	store    *store.Store
	mail     mail.Sender
	log      *slog.Logger
	resolver Resolver
	editor   fs.FS            // built SPA (web/dist)
	now      func() time.Time // clock for block schedules; replaced in tests

	// screener checks link destinations; nil means screening is off.
	screener  screen.Screener
	safeTTL   time.Duration // how long a safe verdict is cached; 0 disables
	unsafeTTL time.Duration
	fetcher   *fetch.Client

	signupLimit  *limiter
	loginIPLimit *limiter
	loginLimit   *limiter
	mailLimit    *limiter
	reportLimit  *limiter
	trackLimit   *limiter
	uploadLimit  *limiter
	verifyLimit  *limiter
	importLimit  *limiter
	previewLimit *limiter
	acct         accountLimits
}

// New builds a server. editor may be nil when the SPA has not been built.
func New(cfg *config.Config, st *store.Store, sender mail.Sender, log *slog.Logger, editor fs.FS) *Server {
	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		log.Error("create upload dir", "err", err)
	}
	return &Server{
		cfg: cfg, store: st, mail: sender, log: log, resolver: net.DefaultResolver, editor: editor, now: time.Now,
		safeTTL: 24 * time.Hour, unsafeTTL: 7 * 24 * time.Hour,
		signupLimit:  newLimiter(5, time.Hour),
		loginIPLimit: newLimiter(30, 15*time.Minute),
		loginLimit:   newLimiter(10, 15*time.Minute),
		mailLimit:    newLimiter(3, time.Hour),
		reportLimit:  newLimiter(5, time.Hour),
		trackLimit:   newLimiter(120, time.Minute),
		uploadLimit:  newLimiter(60, time.Hour),
		verifyLimit:  newLimiter(20, time.Hour),
		importLimit:  newLimiter(20, time.Hour),
		previewLimit: newLimiter(60, time.Hour),
		fetcher:      fetch.New(fetch.Options{}),
		acct:         newAccountLimits(),
	}
}

// SetResolver replaces the DNS resolver (tests).
func (s *Server) SetResolver(r Resolver) { s.resolver = r }

// Handler returns the root handler, routing by Host.
func (s *Server) Handler() http.Handler {
	main := s.mainRoutes()
	custom := s.customDomainRoutes()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Host, s.cfg.BaseHost) || r.URL.Path == "/internal/tls-ask" || r.URL.Path == "/healthz" {
			main.ServeHTTP(w, r)
			return
		}
		custom.ServeHTTP(w, r)
	})
	return s.recoverer(s.logRequests(securityHeaders(h)))
}

func (s *Server) mainRoutes() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", s.handleHealth)
	m.HandleFunc("GET /internal/tls-ask", s.handleTLSAsk)

	// Auth
	m.HandleFunc("POST /api/auth/signup", s.api(s.handleSignup))
	m.HandleFunc("POST /api/auth/login", s.api(s.handleLogin))
	m.HandleFunc("POST /api/auth/logout", s.api(s.handleLogout))
	m.HandleFunc("GET /api/auth/me", s.api(s.authed(s.handleMe)))
	m.HandleFunc("POST /api/auth/verify", s.api(s.handleVerify))
	m.HandleFunc("POST /api/auth/resend-verification", s.api(s.authed(s.handleResend)))
	m.HandleFunc("POST /api/auth/forgot", s.api(s.handleForgot))
	m.HandleFunc("POST /api/auth/reset", s.api(s.handleReset))

	// Account settings, export and deletion
	m.HandleFunc("GET /api/account", s.api(s.authed(s.handleAccount)))
	m.HandleFunc("POST /api/account/password", s.api(s.authed(s.handleChangePassword)))
	m.HandleFunc("POST /api/account/email", s.api(s.authed(s.handleChangeEmail)))
	m.HandleFunc("POST /api/account/email/confirm", s.api(s.handleConfirmEmail))
	m.HandleFunc("GET /api/account/export", s.api(s.authed(s.handleExport)))
	m.HandleFunc("DELETE /api/account", s.api(s.authed(s.handleDeleteAccount)))

	// Catalog, pages, blocks
	m.HandleFunc("GET /api/catalog", s.api(s.handleCatalog))
	m.HandleFunc("GET /api/slugs/{slug}", s.api(s.authed(s.handleSlugCheck)))
	m.HandleFunc("GET /api/pages", s.api(s.authed(s.handleListPages)))
	m.HandleFunc("POST /api/pages", s.api(s.authed(s.handleCreatePage)))
	m.HandleFunc("GET /api/pages/{id}", s.api(s.authed(s.handleGetPage)))
	m.HandleFunc("PATCH /api/pages/{id}", s.api(s.authed(s.handleUpdatePage)))
	m.HandleFunc("DELETE /api/pages/{id}", s.api(s.authed(s.handleDeletePage)))
	m.HandleFunc("GET /api/pages/{id}/qr.svg", s.api(s.authed(s.handlePageQR)))
	m.HandleFunc("GET /api/pages/{id}/stats", s.api(s.authed(s.handleStats)))
	m.HandleFunc("GET /api/pages/{id}/stats.csv", s.api(s.authed(s.handleStatsCSV)))
	m.HandleFunc("PUT /api/pages/{id}/css", s.api(s.authed(s.handleSetPageCSS)))
	m.HandleFunc("POST /api/pages/{id}/blocks", s.api(s.authed(s.handleCreateBlock)))
	m.HandleFunc("PUT /api/pages/{id}/blocks/order", s.api(s.authed(s.handleReorderBlocks)))
	m.HandleFunc("PATCH /api/pages/{id}/blocks/{blockID}", s.api(s.authed(s.handleUpdateBlock)))
	m.HandleFunc("DELETE /api/pages/{id}/blocks/{blockID}", s.api(s.authed(s.handleDeleteBlock)))

	// Domains and uploads
	m.HandleFunc("POST /api/pages/{id}/domains", s.api(s.authed(s.handleCreateDomain)))
	m.HandleFunc("POST /api/pages/{id}/domains/{domainID}/verify", s.api(s.authed(s.handleVerifyDomain)))
	m.HandleFunc("DELETE /api/pages/{id}/domains/{domainID}", s.api(s.authed(s.handleDeleteDomain)))
	m.HandleFunc("POST /api/uploads", s.api(s.authed(s.handleUpload)))
	m.HandleFunc("POST /api/uploads/font", s.api(s.authed(s.handleFontUpload)))

	// Outbound fetching: link-page import and link previews
	m.HandleFunc("POST /api/import/preview", s.api(s.authed(s.handleImportPreview)))
	m.HandleFunc("POST /api/import/image", s.api(s.authed(s.handleImportImage)))
	m.HandleFunc("POST /api/link-preview", s.api(s.authed(s.handleLinkPreview)))

	// Reports and admin
	m.HandleFunc("POST /api/report", s.api(s.handleReport))
	m.HandleFunc("GET /api/admin/reports", s.api(s.admin(s.handleAdminReports)))
	m.HandleFunc("POST /api/admin/reports/{id}/resolve", s.api(s.admin(s.handleAdminResolve)))
	m.HandleFunc("POST /api/admin/pages/{id}/suspend", s.api(s.admin(s.handleAdminSuspendPage)))
	m.HandleFunc("POST /api/admin/users/{id}/suspend", s.api(s.admin(s.handleAdminSuspendUser)))
	m.HandleFunc("GET /api/admin/users/pending", s.api(s.admin(s.handleAdminPendingUsers)))
	m.HandleFunc("POST /api/admin/users/{id}/trust", s.api(s.admin(s.handleAdminTrustUser)))
	m.HandleFunc("GET /api/admin/users", s.api(s.admin(s.handleAdminUsers)))
	m.HandleFunc("POST /api/admin/users/{id}/plan", s.api(s.admin(s.handleAdminSetPlan)))

	// Account settings
	m.HandleFunc("GET /api/account/settings", s.api(s.authed(s.handleGetAccountSettings)))
	m.HandleFunc("PATCH /api/account/settings", s.api(s.authed(s.handleUpdateAccountSettings)))
	m.HandleFunc("/api/", s.api(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not_found", "no such endpoint", "")
	}))

	// Editor SPA
	m.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/app/", http.StatusFound) })
	m.HandleFunc("GET /app/", s.handleEditor)

	// Public
	m.Handle("GET /static/", staticHandler())
	m.HandleFunc("GET /u/{id}", s.handleServeUpload)
	m.HandleFunc("POST /t/{id}", s.handleTrack)
	m.HandleFunc("GET /vcard/{id}", s.handleVCard)
	m.HandleFunc("GET /unsubscribe/weekly-stats", s.handleUnsubscribeWeekly)
	m.HandleFunc("POST /unsubscribe/weekly-stats", s.handleUnsubscribeWeekly)
	m.HandleFunc("GET /preview/{id}", s.handlePreview)
	m.HandleFunc("GET /terms", s.handleTerms)
	m.HandleFunc("GET /privacy", s.handlePrivacy)
	m.HandleFunc("GET /{slug}", s.handlePublicPage)
	m.HandleFunc("GET /{$}", s.handleLanding)
	return m
}

func (s *Server) customDomainRoutes() *http.ServeMux {
	m := http.NewServeMux()
	m.Handle("GET /static/", staticHandler())
	m.HandleFunc("GET /u/{id}", s.handleServeUpload)
	m.HandleFunc("POST /t/{id}", s.handleTrack)
	m.HandleFunc("GET /vcard/{id}", s.handleVCard)
	m.HandleFunc("POST /api/report", s.api(s.handleReport))
	m.HandleFunc("GET /{$}", s.handleCustomDomainPage)
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.notFoundPage(w) })
	return m
}

func staticHandler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(render.Static()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok")) //nolint:errcheck
}

// handleEditor serves the SPA: real files when present, index.html otherwise.
func (s *Server) handleEditor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; frame-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	if s.editor == nil {
		http.Error(w, "editor not built: run `make web`", http.StatusServiceUnavailable)
		return
	}
	name := strings.TrimPrefix(path.Clean(strings.TrimPrefix(r.URL.Path, "/app")), "/")
	if name != "" && name != "index.html" {
		if f, err := s.editor.Open(name); err == nil {
			st, _ := f.Stat()
			f.Close()
			if st != nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				http.ServeFileFS(w, r, s.editor, name)
				return
			}
		}
	}
	index, err := fs.ReadFile(s.editor, "index.html")
	if err != nil {
		http.Error(w, "editor not built: run `make web`", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(index) //nolint:errcheck
}

// ---- middleware ----

type ctxKey int

const userKey ctxKey = 1

// withRole grants admin to verified users listed in ZELDA_ADMIN_EMAILS.
func (s *Server) withRole(u *store.User) *store.User {
	if u != nil && u.EmailVerified && s.cfg.AdminEmails[strings.ToLower(u.Email)] {
		u.IsAdmin = true
	}
	return u
}

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

// api applies JSON API rules: CSRF header on writes, body size limit, session lookup.
func (s *Server) api(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Requested-With") != "zelda" {
			writeErr(w, http.StatusForbidden, "csrf", "missing X-Requested-With header", "")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			if u, err := s.store.UserBySession(r.Context(), c.Value); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), userKey, s.withRole(u)))
			}
		}
		next(w, r)
	}
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r) == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "log in to continue", "")
			return
		}
		next(w, r)
	}
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).IsAdmin {
			writeErr(w, http.StatusForbidden, "forbidden", "admins only", "")
			return
		}
		next(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.log.Info("request", "method", r.Method, "host", r.Host, "path", r.URL.Path, "status", sw.status,
			"dur_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "err", v, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the visitor address, trusting X-Forwarded-For only behind a proxy.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- helpers ----

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg, field string) {
	writeJSON(w, status, map[string]apiError{"error": {code, msg, field}})
}

func (s *Server) serverErr(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("handler error", "path", r.URL.Path, "err", err)
	writeErr(w, http.StatusInternalServerError, "internal", "something went wrong", "")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large", "")
		} else {
			writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body", "")
		}
		return false
	}
	return true
}

func nonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
