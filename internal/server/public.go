package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

var botUA = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|preview|facebookexternalhit|embedly|curl|wget|python|go-http`)

const frameSources = "https://www.youtube-nocookie.com https://player.vimeo.com https://open.spotify.com " +
	"https://w.soundcloud.com https://player.twitch.tv https://clips.twitch.tv https://embed.music.apple.com"

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = render.WriteSimple(w, render.Simple{AppName: s.cfg.AppName, BaseURL: s.cfg.BaseURL, Title: s.cfg.AppName + ": every link, one board", Landing: true})
}

func (s *Server) notFoundPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_ = render.WriteSimple(w, render.Simple{AppName: s.cfg.AppName, BaseURL: s.cfg.BaseURL, Title: "Not found",
		Heading: "Nothing here", Message: "This page doesn't exist or isn't public."})
}

func (s *Server) handlePublicPage(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(r.PathValue("slug"))
	if !slugRe.MatchString(slug) {
		s.notFoundPage(w)
		return
	}
	if slug != r.PathValue("slug") {
		http.Redirect(w, r, "/"+slug, http.StatusMovedPermanently)
		return
	}
	p, err := s.store.PageBySlug(r.Context(), slug)
	if err != nil || !p.Live() {
		s.notFoundPage(w)
		return
	}
	s.renderPage(w, r, p, false, s.pageURL(p.Slug))
}

func (s *Server) handleCustomDomainPage(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(hostOnly(r.Host))
	p, err := s.store.PageByDomain(r.Context(), host)
	if err != nil || !p.Live() {
		s.notFoundPage(w)
		return
	}
	s.renderPage(w, r, p, false, "https://"+host+"/")
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var u *store.User
	if c, err := r.Cookie(sessionCookie); err == nil {
		u, _ = s.store.UserBySession(r.Context(), c.Value)
		u = s.withRole(u)
	}
	if u == nil || !uuidRe.MatchString(id) {
		s.notFoundPage(w)
		return
	}
	p, err := s.store.PageByID(r.Context(), id)
	if err != nil || (p.UserID != u.ID && !u.IsAdmin) {
		s.notFoundPage(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.renderPage(w, r, p, true, s.pageURL(p.Slug))
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, p *store.Page, preview bool, canonical string) {
	stored, err := s.store.Blocks(r.Context(), p.ID)
	if err != nil {
		s.log.Error("load blocks", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	list := make([]blocks.Block, 0, len(stored))
	for _, b := range stored {
		if b.Enabled {
			list = append(list, blocks.Block{ID: b.ID, Type: b.Type, Data: b.Data})
		}
	}
	theme := themeOf(p)
	ctx := blocks.Context{
		Host:      hostOnly(r.Host),
		UploadURL: uploadURL,
		QRFG:      "#000000",
		QRBG:      "#ffffff",
		VCardURL:  func(id string) string { return "/vcard/" + id },
	}
	n := nonce()
	view := render.Page{
		AppName: s.cfg.AppName, BaseURL: s.cfg.BaseURL, Slug: p.Slug, Title: p.Title, Bio: p.Bio,
		CanonicalURL: canonical, Theme: theme, Items: blocks.Build(ctx, list),
		Preview: preview, Live: p.Live(), Nonce: n,
	}
	if p.AvatarUploadID != nil {
		view.Avatar = uploadURL(*p.AvatarUploadID)
		view.AvatarAbs = s.cfg.BaseURL + view.Avatar
	}
	var bgImage string
	if theme.Background.Kind == "image" && theme.Background.ImageUploadID != "" {
		bgImage = uploadURL(theme.Background.ImageUploadID)
	}
	var buf bytes.Buffer
	if err := render.WritePage(&buf, view, bgImage); err != nil {
		s.log.Error("render page", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self' 'nonce-"+n+"'; script-src 'self'; img-src 'self' data:; "+
		"frame-src "+frameSources+"; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'self'")
	if !preview {
		h.Set("Cache-Control", "public, max-age=60")
		if !botUA.MatchString(r.UserAgent()) {
			_ = s.store.Count(r.Context(), p.ID, store.ZeroBlockID)
		}
	}
	w.Write(buf.Bytes()) //nolint:errcheck
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) || !s.trackLimit.allow(s.clientIP(r)) || botUA.MatchString(r.UserAgent()) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if b, p, err := s.store.LiveBlock(r.Context(), id); err == nil {
		_ = s.store.Count(r.Context(), p.ID, b.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVCard serves a contact block as a vCard: public for live pages, and
// for the owner while previewing a draft.
func (s *Server) handleVCard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		s.notFoundPage(w)
		return
	}
	b, _, err := s.store.LiveBlock(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		b = s.ownerDraftBlock(r, id)
	}
	if b == nil || b.Type != "contact" {
		s.notFoundPage(w)
		return
	}
	var c blocks.Contact
	if json.Unmarshal(b.Data, &c) != nil {
		s.notFoundPage(w)
		return
	}
	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(c.Name)+`.vcf"`)
	w.Write([]byte(blocks.VCard(c))) //nolint:errcheck
}

func (s *Server) ownerDraftBlock(r *http.Request, id string) *store.Block {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	u, err := s.store.UserBySession(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	pages, err := s.store.PagesByUser(r.Context(), u.ID)
	if err != nil {
		return nil
	}
	for _, p := range pages {
		if b, err := s.store.Block(r.Context(), p.ID, id); err == nil {
			return b
		}
	}
	return nil
}

func safeFilename(name string) string {
	out := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		if r == ' ' {
			return '-'
		}
		return -1
	}, name)
	if out == "" {
		return "contact"
	}
	return out
}

var reportReasons = map[string]bool{"spam": true, "scam": true, "impersonation": true, "phishing": true, "illegal": true, "other": true}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if !s.reportLimit.allow(s.clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many reports, try again later", "")
		return
	}
	var body struct {
		Slug    string `json:"slug"`
		Reason  string `json:"reason"`
		Details string `json:"details"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !reportReasons[body.Reason] {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "choose a reason", "reason")
		return
	}
	if len(body.Details) > 1000 {
		body.Details = body.Details[:1000]
	}
	p, err := s.store.PageBySlug(r.Context(), strings.ToLower(body.Slug))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "page not found", "slug")
		return
	}
	if err := s.store.CreateReport(r.Context(), p.ID, body.Reason, strings.TrimSpace(body.Details)); err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func hostOnly(h string) string {
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}
