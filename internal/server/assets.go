package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/fonts"
	"git.subcult.tv/subculture-collective/zelda/internal/plans"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

const maxUpload = 5 << 20

var allowedImages = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func (s *Server) uploadPath(id string) string { return filepath.Join(s.cfg.UploadDir, id) }

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.uploadLimit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "upload limit reached, try again later", "")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "attach an image in the file field", "file")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "could not read upload", "file")
		return
	}
	if len(data) > maxUpload {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "images must be 5 MB or smaller", "file")
		return
	}
	// Trust the bytes, not the client's declared type; SVG is never accepted.
	ct := http.DetectContentType(data)
	if !allowedImages[ct] {
		writeErr(w, http.StatusUnprocessableEntity, "unsupported", "use a PNG, JPEG, GIF or WebP image", "file")
		return
	}
	id, err := s.store.CreateUpload(r.Context(), u.ID, ct, len(data))
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if err := os.WriteFile(s.uploadPath(id), data, 0o640); err != nil {
		_ = s.store.DeleteUpload(r.Context(), id)
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "url": uploadURL(id), "content_type": ct, "size": len(data)})
}

func (s *Server) handleServeUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	up, err := s.store.Upload(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(s.uploadPath(id))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", up.ContentType)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if fonts.IsFont(up.ContentType) {
		// Fonts load only from pages on the same host: no CORS header, and
		// other sites can't embed them.
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
	}
	http.ServeContent(w, r, "", time.Time{}, f)
}

// ---- custom domains ----

var hostnameRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

func (s *Server) hostnameProblem(h string) string {
	base := strings.ToLower(hostOnly(s.cfg.BaseHost))
	switch {
	case len(h) > 253 || !hostnameRe.MatchString(h):
		return "enter a hostname like links.example.com (no https:// or path)"
	case net.ParseIP(h) != nil:
		return "use a hostname, not an IP address"
	case h == base || strings.HasSuffix(h, "."+base):
		return "that hostname belongs to this service"
	}
	return ""
}

func (s *Server) domainsDisabled(w http.ResponseWriter) bool {
	if !s.cfg.CustomDomains {
		writeErr(w, http.StatusNotFound, "custom_domains_disabled", "custom domains are not enabled on this server", "")
		return true
	}
	return false
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	if s.domainsDisabled(w) {
		return
	}
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	var body struct {
		Hostname string `json:"hostname"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(body.Hostname)), ".")
	h = strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://")
	if prob := s.hostnameProblem(h); prob != "" {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", prob, "hostname")
		return
	}
	limits, ok := s.limitsFor(w, r)
	if !ok {
		return
	}
	if !limits.Features[plans.CustomDomains] || limits.CustomDomains < 1 {
		writeUpgrade(w, string(plans.CustomDomains), upgradeMessage(plans.CustomDomains), "hostname")
		return
	}
	d, err := s.store.CreateDomainLimit(r.Context(), p.UserID, p.ID, h, limits.CustomDomains)
	switch {
	case errors.Is(err, store.ErrAccountLimit):
		writeErr(w, http.StatusUnprocessableEntity, "limit", fmt.Sprintf("your plan includes %d custom domain%s; remove one to add another",
			limits.CustomDomains, plural(limits.CustomDomains)), "hostname")
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "domain_taken", "that hostname is already attached to a page", "hostname")
	case errors.Is(err, store.ErrLimit):
		writeErr(w, http.StatusUnprocessableEntity, "limit", "a page can have up to 3 domains", "hostname")
	case err != nil:
		s.serverErr(w, r, err)
	default:
		writeJSON(w, http.StatusCreated, s.domainView(d))
	}
}

func (s *Server) ownedDomain(w http.ResponseWriter, r *http.Request) *store.Domain {
	p := s.ownedPage(w, r)
	if p == nil {
		return nil
	}
	id := r.PathValue("domainID")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "domain not found", "")
		return nil
	}
	d, err := s.store.Domain(r.Context(), p.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "domain not found", "")
		return nil
	} else if err != nil {
		s.serverErr(w, r, err)
		return nil
	}
	return d
}

// handleVerifyDomain checks the TXT ownership record and that the hostname
// resolves to the same addresses as the service's DNS target.
func (s *Server) handleVerifyDomain(w http.ResponseWriter, r *http.Request) {
	if s.domainsDisabled(w) {
		return
	}
	d := s.ownedDomain(w, r)
	if d == nil {
		return
	}
	if d.Verified {
		writeJSON(w, http.StatusOK, s.domainView(d))
		return
	}
	// Domains from before plans can still be verified on any plan.
	old, err := s.store.GrandfatheredDomains(r.Context(), d.PageID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if !old[d.ID] && !s.requireFeature(w, r, plans.CustomDomains, "") {
		return
	}
	if !s.verifyLimit.allow("domain:" + d.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many checks; DNS changes can take a while, try again later", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if reason := s.checkDomainDNS(ctx, d); reason != "" {
		writeErr(w, http.StatusUnprocessableEntity, "dns_not_ready", reason, "")
		return
	}
	if err := s.store.MarkDomainVerified(r.Context(), d.ID); err != nil {
		s.serverErr(w, r, err)
		return
	}
	d.Verified = true
	writeJSON(w, http.StatusOK, s.domainView(d))
}

func (s *Server) checkDomainDNS(ctx context.Context, d *store.Domain) string {
	want := "zelda-verify=" + d.VerifyToken
	txts, err := s.resolver.LookupTXT(ctx, "_zelda."+d.Hostname)
	found := false
	for _, t := range txts {
		if strings.TrimSpace(t) == want {
			found = true
		}
	}
	if err != nil || !found {
		return "TXT record _zelda." + d.Hostname + " with value " + want + " not found yet"
	}
	got, err := s.resolver.LookupIPAddr(ctx, d.Hostname)
	if err != nil || len(got) == 0 {
		return d.Hostname + " doesn't resolve yet; add a CNAME to " + s.cfg.DomainTarget
	}
	target, err := s.resolver.LookupIPAddr(ctx, s.cfg.DomainTarget)
	if err != nil {
		return "couldn't resolve " + s.cfg.DomainTarget + "; try again shortly"
	}
	targets := map[string]bool{}
	for _, a := range target {
		targets[a.IP.String()] = true
	}
	for _, a := range got {
		if targets[a.IP.String()] {
			return ""
		}
	}
	return d.Hostname + " doesn't point at " + s.cfg.DomainTarget + " yet"
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	d := s.ownedDomain(w, r)
	if d == nil {
		return
	}
	if err := s.store.DeleteDomain(r.Context(), d.PageID, d.ID); err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTLSAsk is Caddy's on_demand_tls "ask" endpoint. It answers only
// private-network callers so the public cannot enumerate domains.
func (s *Server) handleTLSAsk(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if ip == nil || !(ip.IsLoopback() || ip.IsPrivate()) {
		http.NotFound(w, r)
		return
	}
	domain := strings.ToLower(r.URL.Query().Get("domain"))
	if domain == hostOnly(s.cfg.BaseHost) || s.domainServed(r.Context(), domain) {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.NotFound(w, r)
}

// ---- admin ----

func (s *Server) handleAdminReports(w http.ResponseWriter, r *http.Request) {
	reports, err := s.store.Reports(r.Context())
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) handleAdminResolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "report not found", "")
		return
	}
	if err := s.store.ResolveReport(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "report not found", "")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminSuspend(w http.ResponseWriter, r *http.Request, set func(id string, v bool) error) {
	id := r.PathValue("id")
	var body struct {
		Suspended bool `json:"suspended"`
	}
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "not found", "")
		return
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := set(id, body.Suspended); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "not found", "")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminSuspendPage(w http.ResponseWriter, r *http.Request) {
	s.adminSuspend(w, r, func(id string, v bool) error { return s.store.SetPageSuspended(r.Context(), id, v) })
}

func (s *Server) handleAdminSuspendUser(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == userFrom(r).ID {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "you can't suspend yourself", "")
		return
	}
	s.adminSuspend(w, r, func(id string, v bool) error { return s.store.SetUserSuspended(r.Context(), id, v) })
}
