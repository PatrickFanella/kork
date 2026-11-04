package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
	"git.subcult.tv/subculture-collective/zelda/internal/plans"
	"git.subcult.tv/subculture-collective/zelda/internal/qr"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

var botUA = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|preview|facebookexternalhit|embedly|curl|wget|python|go-http`)

const frameSources = "https://www.youtube-nocookie.com https://player.vimeo.com https://open.spotify.com " +
	"https://w.soundcloud.com https://player.twitch.tv https://clips.twitch.tv https://embed.music.apple.com"

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = render.WriteSimple(w, render.Simple{AppName: s.cfg.AppName, BaseURL: s.cfg.BaseURL, Title: s.cfg.AppName + ": a free link page by SUBCULT", Landing: true, ContactEmail: s.cfg.ContactEmail})
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
	if !s.domainServed(r.Context(), host) {
		s.notFoundPage(w)
		return
	}
	p, err := s.store.PageByDomain(r.Context(), host)
	if err != nil || !p.Live() {
		s.notFoundPage(w)
		return
	}
	s.renderPage(w, r, p, false, "https://"+host+"/")
}

// handleAtprotoDID answers Bluesky's HTTPS handle check on a page's custom
// domain with the page's DID as plain text. It only answers for a served
// domain on a live page with a DID; everything else is 404.
// https://atproto.com/specs/handle
func (s *Server) handleAtprotoDID(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(hostOnly(r.Host))
	if !s.domainServed(r.Context(), host) {
		http.NotFound(w, r)
		return
	}
	p, err := s.store.PageByDomain(r.Context(), host)
	if err != nil || !p.Live() || p.AtprotoDID == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(p.AtprotoDID))
}

// themeQRColors returns the theme's text and card (or page) colors for QR
// codes when they are safe to scan, and black on white otherwise.
func themeQRColors(t catalog.Theme) (fg, bg string) {
	c := t.Colors
	return qr.Pick([2]string{c.Text, c.Surface}, [2]string{c.Text, c.BG})
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
	now := s.now()
	list := make([]blocks.Block, 0, len(stored))
	var badges map[string]render.Badge
	for _, b := range stored {
		if !b.Enabled {
			continue
		}
		if !b.VisibleAt(now) {
			if !preview {
				continue
			}
			if badges == nil {
				badges = map[string]render.Badge{}
			}
			text, at := scheduleBadge(b.Schedule, now)
			badges[b.ID] = render.NewBadge(text, at)
		}
		list = append(list, blocks.Block{ID: b.ID, Type: b.Type, Data: b.Data})
	}
	theme := themeOf(p)
	qrFG, qrBG := themeQRColors(theme)
	origin := strings.TrimSuffix(s.cfg.BaseURL, "/")
	if u, err := url.Parse(canonical); err == nil && u.Scheme != "" && u.Host != "" {
		origin = u.Scheme + "://" + u.Host
	}
	ctx := blocks.Context{
		Host:      hostOnly(r.Host),
		UploadURL: uploadURL,
		QRFG:      qrFG,
		QRBG:      qrBG,
		VCardURL:  func(id string) string { return "/vcard/" + id },
		AbsURL:    func(path string) string { return origin + path },
		Now:       now,
		Latest:    s.latestItems(r.Context(), p, list),
		EventFeed: s.eventFeeds(r.Context(), list),
	}
	n := nonce()
	view := render.Page{
		AppName: s.cfg.AppName, BaseURL: s.cfg.BaseURL, Slug: p.Slug, Title: p.Title, Bio: p.Bio,
		CanonicalURL: canonical, Theme: theme, Items: blocks.Build(ctx, list),
		Preview: preview, Live: p.Live(), Nonce: n, Badges: badges,
		HideBranding: theme.HideBranding && s.ownerCan(r.Context(), p.UserID, plans.RemoveBranding),
	}
	if !preview && p.Live() {
		view.MonetizationWallet = p.MonetizationWallet
	}
	pro := s.proStyleFor(r.Context(), p, theme)
	view.CustomCSS, view.FontBody, view.FontDisplay = pro.CSS, pro.FontBody, pro.FontDisplay
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
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self' 'nonce-"+n+"'; script-src 'self'; img-src 'self' data:; font-src 'self'; "+
		"frame-src "+frameSources+"; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'self'")
	if !preview {
		h.Set("Cache-Control", "public, max-age="+strconv.Itoa(publicCacheSeconds(stored, now)))
		if !botUA.MatchString(r.UserAgent()) {
			s.countView(r, p.ID)
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
	if b, p, err := s.store.LiveBlock(r.Context(), id, s.now()); err == nil {
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
	b, _, err := s.store.LiveBlock(r.Context(), id, s.now())
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
	body.Details = truncateRunes(body.Details, 1000)
	// Only pages visitors can see are reportable; a draft or suspended page
	// looks the same as a missing one, as it does at GET /{slug}.
	p, err := s.store.PageBySlug(r.Context(), strings.ToLower(body.Slug))
	if err != nil || !p.Live() {
		writeErr(w, http.StatusNotFound, "not_found", "page not found", "slug")
		return
	}
	if err := s.store.CreateReport(r.Context(), p.ID, body.Reason, strings.TrimSpace(body.Details)); err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// truncateRunes keeps at most n characters of s without cutting a multi-byte
// character in half, which Postgres would reject as invalid UTF-8.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for range n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

func hostOnly(h string) string {
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}
