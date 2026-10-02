package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
	"git.subcult.tv/subculture-collective/zelda/internal/icons"
	"git.subcult.tv/subculture-collective/zelda/internal/plans"
	"git.subcult.tv/subculture-collective/zelda/internal/qr"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

var (
	slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)
	uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// reserved slugs collide with routes or invite impersonation.
var reserved = map[string]bool{}

func init() {
	for _, s := range strings.Fields(`app api go u t static internal admin administrator root login logout signup
		register preview vcard healthz help support about terms privacy legal abuse report security settings
		account dashboard billing pricing blog docs status www mail email null undefined favicon.ico robots.txt
		sitemap.xml zelda subcult official staff moderator mod team new edit stripe patreon upgrade`) {
		reserved[s] = true
	}
}

func slugProblem(slug string) string {
	switch {
	case !slugRe.MatchString(slug):
		return "use 3–32 lowercase letters, numbers, - or _"
	case reserved[slug]:
		return "that name is reserved"
	}
	return ""
}

type catalogService struct {
	catalog.Service
	IconSVG string `json:"icon_svg"`
}

var catalogServices = func() []catalogService {
	list := catalog.Services()
	out := make([]catalogService, len(list))
	for i, svc := range list {
		out[i] = catalogService{svc, string(icons.For(svc.Icon, svc.Category))}
	}
	return out
}()

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"services":        catalogServices,
		"categories":      catalog.Categories,
		"block_types":     catalog.BlockTypes,
		"themes":          catalog.Presets,
		"fonts":           catalog.Fonts,
		"button_styles":   catalog.ButtonStyles,
		"button_shapes":   catalog.ButtonShapes,
		"layouts":         catalog.Layouts,
		"embed_providers": catalog.EmbedProviders,
		"features":        map[string]bool{"custom_domains": s.cfg.CustomDomains, "brevo_signup": s.x.sealer != nil},
		"domain_mode":     s.domains.Mode(),
		"domain_target":   s.cfg.DomainTarget,
	})
}

func (s *Server) handleSlugCheck(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(r.PathValue("slug"))
	if p := slugProblem(slug); p != "" {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": p})
		return
	}
	taken, err := s.store.SlugTaken(r.Context(), slug)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if taken {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": "already taken"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true})
}

func (s *Server) pageURL(slug string) string { return s.cfg.BaseURL + "/" + slug }

func uploadURL(id string) string { return "/u/" + id }

type pageSummary struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Published bool   `json:"published"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	pages, err := s.store.PagesByUser(r.Context(), userFrom(r).ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	out := make([]pageSummary, 0, len(pages))
	for _, p := range pages {
		out = append(out, pageSummary{p.ID, p.Slug, p.Title, p.Published, s.pageURL(p.Slug), p.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	writeJSON(w, http.StatusOK, out)
}

type domainView struct {
	*store.Domain
	// TXTName and TXTValue are the ownership TXT record: _zelda.<host> in
	// DNS mode, Cloudflare's ownership_verification record (or empty) in
	// Cloudflare mode.
	TXTName  string `json:"txt_name"`
	TXTValue string `json:"txt_value"`
	Target   string `json:"target"`
	// Active is false when the owner's plan no longer covers the domain;
	// it stays attached but the page isn't served on it.
	Active bool `json:"active"`
	// Mode is "dns" or "cloudflare"; Status is "pending", "active" or "error".
	Mode      string      `json:"mode"`
	Status    string      `json:"status"`
	Records   []dnsRecord `json:"records"`
	Errors    []string    `json:"errors"`
	CheckedAt *time.Time  `json:"checked_at"`
}

func (s *Server) domainView(d *store.Domain) domainView {
	v := domainView{Domain: d, Target: s.cfg.DomainTarget, Active: true, Mode: s.domains.Mode(), Errors: []string{}}
	s.domains.Describe(d, &v)
	return v
}

type pageView struct {
	*store.Page
	Theme     catalog.Theme  `json:"theme"`
	URL       string         `json:"url"`
	AvatarURL *string        `json:"avatar_url"`
	Blocks    []*store.Block `json:"blocks"`
	Domains   []domainView   `json:"domains"`
	// CustomCSS is the stored, sanitized custom CSS (Pro).
	CustomCSS string `json:"custom_css"`
}

func (s *Server) writePage(w http.ResponseWriter, r *http.Request, status int, p *store.Page) {
	bl, err := s.store.Blocks(r.Context(), p.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	for _, b := range bl {
		b.Label = blocks.Label(b.Type, b.Data)
	}
	ds, err := s.store.Domains(r.Context(), p.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	v := pageView{Page: p, Theme: themeOf(p), URL: s.pageURL(p.Slug), Blocks: bl, Domains: make([]domainView, 0, len(ds))}
	if v.CustomCSS, err = s.store.PageCSS(r.Context(), p.ID); err != nil {
		s.serverErr(w, r, err)
		return
	}
	if p.AvatarUploadID != nil {
		u := uploadURL(*p.AvatarUploadID)
		v.AvatarURL = &u
	}
	served, err := s.domainsServed(r.Context(), p)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	for _, d := range ds {
		dv := s.domainView(d)
		dv.Active = served(d.ID)
		v.Domains = append(v.Domains, dv)
	}
	writeJSON(w, status, v)
}

func themeOf(p *store.Page) catalog.Theme {
	var t catalog.Theme
	_ = json.Unmarshal(p.Theme, &t)
	t, _, err := t.Normalize()
	if err != nil {
		return catalog.DefaultTheme()
	}
	return t
}

// ownedPage loads the {id} page if the current user owns it, else writes 404.
func (s *Server) ownedPage(w http.ResponseWriter, r *http.Request) *store.Page {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "page not found", "")
		return nil
	}
	p, err := s.store.PageByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && p.UserID != userFrom(r).ID) {
		writeErr(w, http.StatusNotFound, "not_found", "page not found", "")
		return nil
	}
	if err != nil {
		s.serverErr(w, r, err)
		return nil
	}
	return p
}

func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Slug = strings.ToLower(strings.TrimSpace(body.Slug))
	if p := slugProblem(body.Slug); p != "" {
		writeErr(w, http.StatusUnprocessableEntity, "invalid_slug", p, "slug")
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = body.Slug
	}
	if utf8.RuneCountInString(title) > 80 {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "title must be 80 characters or fewer", "title")
		return
	}
	limits, ok := s.limitsFor(w, r)
	if !ok {
		return
	}
	theme, _ := json.Marshal(catalog.DefaultTheme())
	p, err := s.store.CreatePageLimit(r.Context(), userFrom(r).ID, body.Slug, title, theme, limits.Pages)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "slug_taken", "that name is already taken", "slug")
	case errors.Is(err, store.ErrLimit) && plans.ProLimits().Pages > limits.Pages:
		writeUpgrade(w, plans.LimitPages, fmt.Sprintf("Free accounts can have up to %d pages. Pro allows %d.",
			limits.Pages, plans.ProLimits().Pages), "")
	case errors.Is(err, store.ErrLimit):
		writeErr(w, http.StatusUnprocessableEntity, "limit", fmt.Sprintf("you can have up to %d pages", limits.Pages), "")
	case err != nil:
		s.serverErr(w, r, err)
	default:
		s.writePage(w, r, http.StatusCreated, p)
	}
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	if p := s.ownedPage(w, r); p != nil {
		s.writePage(w, r, http.StatusOK, p)
	}
}

func (s *Server) handleUpdatePage(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	var body struct {
		Slug           *string          `json:"slug"`
		Title          *string          `json:"title"`
		Bio            *string          `json:"bio"`
		AvatarUploadID json.RawMessage  `json:"avatar_upload_id"`
		Theme          *json.RawMessage `json:"theme"`
		Published      *bool            `json:"published"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	u := userFrom(r)
	if body.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*body.Slug))
		if slug != p.Slug {
			if prob := slugProblem(slug); prob != "" {
				writeErr(w, http.StatusUnprocessableEntity, "invalid_slug", prob, "slug")
				return
			}
			p.Slug = slug
		}
	}
	if body.Title != nil {
		t := strings.Join(strings.Fields(*body.Title), " ")
		if t == "" || utf8.RuneCountInString(t) > 80 {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "title must be 1–80 characters", "title")
			return
		}
		p.Title = t
	}
	if body.Bio != nil {
		b := strings.TrimSpace(*body.Bio)
		if utf8.RuneCountInString(b) > 300 {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "bio must be 300 characters or fewer", "bio")
			return
		}
		p.Bio = b
	}
	if len(body.AvatarUploadID) > 0 {
		if string(body.AvatarUploadID) == "null" {
			p.AvatarUploadID = nil
		} else {
			var id string
			if json.Unmarshal(body.AvatarUploadID, &id) != nil || !uuidRe.MatchString(id) || !s.store.OwnsUpload(r.Context(), u.ID, id) {
				writeErr(w, http.StatusUnprocessableEntity, "invalid", "unknown upload", "avatar_upload_id")
				return
			}
			p.AvatarUploadID = &id
		}
	}
	if body.Theme != nil {
		var t catalog.Theme
		if json.Unmarshal(*body.Theme, &t) != nil {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "invalid theme", "theme")
			return
		}
		t, field, err := t.Normalize()
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), field)
			return
		}
		if id := t.Background.ImageUploadID; id != "" && (!uuidRe.MatchString(id) || !s.store.OwnsUpload(r.Context(), u.ID, id)) {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "unknown background image", "background")
			return
		}
		if t.Background.Kind != "image" {
			t.Background.ImageUploadID = ""
		}
		// Turning the footer credit off needs Pro. A lapsed Pro page keeps
		// the stored flag (rendering ignores it), so saving other theme
		// changes still works.
		if t.HideBranding && !themeOf(p).HideBranding && !s.requireFeature(w, r, plans.RemoveBranding, "hide_branding") {
			return
		}
		if !s.checkThemeFonts(w, r, t, themeOf(p)) {
			return
		}
		p.Theme, _ = json.Marshal(t)
	}
	if body.Published != nil {
		if *body.Published && !u.EmailVerified {
			writeErr(w, http.StatusForbidden, "email_unverified", "confirm your email address before publishing", "published")
			return
		}
		if *body.Published && !p.Published && !s.allowPublish(w, r, u, p) {
			return
		}
		p.Published = *body.Published
	}
	if err := s.store.UpdatePage(r.Context(), p); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "slug_taken", "that name is already taken", "slug")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	p, err := s.store.PageByID(r.Context(), p.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	s.writePage(w, r, http.StatusOK, p)
}

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	s.domains.Release(r.Context(), p.ID, "")
	if err := s.store.DeletePage(r.Context(), p.ID); err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePageQR(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	svg, err := qr.SVG(s.pageURL(p.Slug), "#000000", "#ffffff")
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Disposition", `inline; filename="`+p.Slug+`-qr.svg"`)
	w.Write([]byte(svg)) //nolint:errcheck
}

// ---- blocks ----

func (s *Server) writeBlockErr(w http.ResponseWriter, err error) {
	var fe *blocks.FieldError
	if errors.As(err, &fe) {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", fe.Error(), fe.Field)
		return
	}
	writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), "")
}

func (s *Server) uploadChecker(r *http.Request) blocks.UploadChecker {
	userID := userFrom(r).ID
	return func(id string) bool { return s.store.OwnsUpload(r.Context(), userID, id) }
}

func (s *Server) handleCreateBlock(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	var body struct {
		Type    string          `json:"type"`
		Data    json.RawMessage `json:"data"`
		Enabled *bool           `json:"enabled"`
		scheduleInput
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !catalog.IsBlockType(body.Type) {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "unknown block type", "type")
		return
	}
	raw, secrets, err := blocks.TakeSecrets(body.Type, body.Data)
	if err != nil {
		s.writeBlockErr(w, err)
		return
	}
	data, err := blocks.Validate(body.Type, raw, s.uploadChecker(r))
	if err != nil {
		s.writeBlockErr(w, err)
		return
	}
	if s.rejectBlocked(w, r, body.Type, data) {
		return
	}
	if f, ok := body.setsBound(); ok && !s.requireFeature(w, r, plans.ScheduledLinks, f) {
		return
	}
	var sched store.Schedule
	if !body.applyTo(w, &sched) {
		return
	}
	data, after, ok := s.prepareExtras(w, r, nil, body.Type, data, secrets)
	if !ok {
		return
	}
	enabled := body.Enabled == nil || *body.Enabled
	b, err := s.store.CreateBlock(r.Context(), p.ID, body.Type, data, enabled, sched)
	if errors.Is(err, store.ErrLimit) {
		writeErr(w, http.StatusUnprocessableEntity, "limit", "a page can have up to 100 blocks", "")
		return
	}
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if after != nil {
		if err := after(r.Context(), b); err != nil {
			_ = s.store.DeleteBlock(r.Context(), p.ID, b.ID)
			s.serverErr(w, r, err)
			return
		}
	}
	b.Label = blocks.Label(b.Type, b.Data)
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) handleUpdateBlock(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	id := r.PathValue("blockID")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "block not found", "")
		return
	}
	b, err := s.store.Block(r.Context(), p.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "block not found", "")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	var body struct {
		Data    json.RawMessage `json:"data"`
		Enabled *bool           `json:"enabled"`
		scheduleInput
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if f, ok := body.setsBound(); ok && !s.requireFeature(w, r, plans.ScheduledLinks, f) {
		return
	}
	if !body.applyTo(w, &b.Schedule) {
		return
	}
	var after func(context.Context, *store.Block) error
	if len(body.Data) > 0 {
		raw, secrets, err := blocks.TakeSecrets(b.Type, body.Data)
		if err != nil {
			s.writeBlockErr(w, err)
			return
		}
		data, err := blocks.Validate(b.Type, raw, s.uploadChecker(r))
		if err != nil {
			s.writeBlockErr(w, err)
			return
		}
		if s.rejectBlocked(w, r, b.Type, data) {
			return
		}
		var ok bool
		if data, after, ok = s.prepareExtras(w, r, b, b.Type, data, secrets); !ok {
			return
		}
		b.Data = data
	}
	if body.Enabled != nil {
		b.Enabled = *body.Enabled
	}
	if err := s.store.UpdateBlock(r.Context(), b); err != nil {
		s.serverErr(w, r, err)
		return
	}
	if after != nil {
		if err := after(r.Context(), b); err != nil {
			s.serverErr(w, r, err)
			return
		}
	}
	b.Label = blocks.Label(b.Type, b.Data)
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleDeleteBlock(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	id := r.PathValue("blockID")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "block not found", "")
		return
	}
	cleanup := s.blockDeleteCleanup(r.Context(), id)
	if err := s.store.DeleteBlock(r.Context(), p.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "block not found", "")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	cleanup()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderBlocks(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	for _, id := range body.IDs {
		if !uuidRe.MatchString(id) {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", store.ErrBadOrder.Error(), "ids")
			return
		}
	}
	if err := s.store.ReorderBlocks(r.Context(), p.ID, body.IDs); errors.Is(err, store.ErrBadOrder) {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), "ids")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
