package server

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// accountLimits throttle password checks and exports per account.
type accountLimits struct {
	password *limiter
	export   *limiter
}

func newAccountLimits() accountLimits {
	return accountLimits{password: newLimiter(10, 15*time.Minute), export: newLimiter(5, time.Hour)}
}

func (s *Server) handleTerms(w http.ResponseWriter, r *http.Request)   { s.writeLegal(w, "terms") }
func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) { s.writeLegal(w, "privacy") }

func (s *Server) writeLegal(w http.ResponseWriter, doc string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = render.WriteLegal(w, render.Legal{AppName: s.cfg.AppName, BaseURL: s.cfg.BaseURL, ContactEmail: s.cfg.ContactEmail, Doc: doc})
}

// confirmPassword checks the signed-in user's password, rate limited per
// account. It writes the error response and returns false on failure.
func (s *Server) confirmPassword(w http.ResponseWriter, u *store.User, pw, field string) bool {
	if !s.acct.password.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, wait 15 minutes", "")
		return false
	}
	if ok, _ := checkPassword(u.PasswordHash, pw); !ok {
		writeErr(w, http.StatusUnprocessableEntity, "wrong_password", "that password isn't right", field)
		return false
	}
	return true
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.Account(r.Context(), userFrom(r))
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &body) || !s.confirmPassword(w, u, body.CurrentPassword, "current_password") {
		return
	}
	if !validPasswordFor(w, body.NewPassword, "new_password") {
		return
	}
	hash, err := hashPassword(body.NewPassword)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if err := s.store.SetPassword(r.Context(), u.ID, hash); err != nil {
		s.serverErr(w, r, err)
		return
	}
	c, _ := r.Cookie(sessionCookie)
	if err := s.store.DeleteOtherSessions(r.Context(), u.ID, c.Value); err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChangeEmail(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) || !s.confirmPassword(w, u, body.Password, "password") {
		return
	}
	email := strings.TrimSpace(body.Email)
	if len(email) > 254 || !emailRe.MatchString(email) {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "enter a valid email address", "email")
		return
	}
	if strings.EqualFold(email, u.Email) {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "that's already your email address", "email")
		return
	}
	if used, err := s.store.EmailInUse(r.Context(), email); err != nil {
		s.serverErr(w, r, err)
		return
	} else if used {
		writeErr(w, http.StatusConflict, "email_taken", "an account with that email already exists", "email")
		return
	}
	if !s.mailLimit.allow("email-change:" + u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "we've sent a few already; check your spam folder", "")
		return
	}
	tok, err := s.store.CreateEmailChange(r.Context(), u.ID, email)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	link := s.cfg.BaseURL + "/app/confirm-email?token=" + tok
	s.sendMail(mail.Message{To: email, Subject: "Confirm your new " + s.cfg.AppName + " email",
		Body: "Confirm this address to use it for your " + s.cfg.AppName + " account:\n\n" + link +
			"\n\nThe link expires in 24 hours. Until then your account keeps using " + u.Email +
			". If you didn't ask for this, ignore this message.\n"})
	s.sendMail(mail.Message{To: u.Email, Subject: "Email change requested on " + s.cfg.AppName,
		Body: "Someone signed in to your " + s.cfg.AppName + " account asked to change its email to " + email +
			". Nothing changes until that address is confirmed.\n\nIf this wasn't you, change your password: " +
			s.cfg.BaseURL + "/app/settings\n"})
	a, err := s.store.Account(r.Context(), u)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, a)
}

func (s *Server) handleConfirmEmail(w http.ResponseWriter, r *http.Request) {
	if !s.verifyLimit.allow(s.clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later", "")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	userID, err := s.store.ConfirmEmailChange(r.Context(), body.Token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusBadRequest, "invalid_token", "this link is invalid or expired", "")
		return
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "email_taken", "another account now uses that email address", "")
		return
	case err != nil:
		s.serverErr(w, r, err)
		return
	}
	u, err := s.store.UserByID(r.Context(), userID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": s.withRole(u)})
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) || !s.confirmPassword(w, u, body.Password, "password") {
		return
	}
	s.domains.Release(r.Context(), "", u.ID)
	uploads, err := s.store.DeleteUser(r.Context(), u.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	for _, id := range uploads {
		if err := os.Remove(s.uploadPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Error("remove upload of deleted account", "upload", id, "err", err)
		}
	}
	s.clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

// ---- export ----

type exportPage struct {
	*store.Page
	Blocks  []*store.Block  `json:"blocks"`
	Domains []*exportDomain `json:"domains"`
	Stats   []store.StatRow `json:"stats"`
}

type exportDomain struct {
	Hostname  string    `json:"hostname"`
	Verified  bool      `json:"verified"`
	CreatedAt time.Time `json:"created_at"`
}

type exportUpload struct {
	ID          string `json:"id"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	File        string `json:"file"`
}

type exportAccount struct {
	ExportedAt      time.Time       `json:"exported_at"`
	Email           string          `json:"email"`
	EmailVerified   bool            `json:"email_verified"`
	CreatedAt       time.Time       `json:"created_at"`
	TermsVersion    *string         `json:"terms_version"`
	TermsAcceptedAt *time.Time      `json:"terms_accepted_at"`
	Pages           []*exportPage   `json:"pages"`
	Uploads         []*exportUpload `json:"uploads"`
}

func uploadExt(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
		return exts[0]
	}
	return ""
}

// handleExport streams a ZIP of account.json and the user's uploaded images.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.acct.export.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "you can download your data 5 times an hour", "")
		return
	}
	ctx := r.Context()
	acct, err := s.store.Account(ctx, u)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	out := exportAccount{ExportedAt: time.Now().UTC(), Email: u.Email, EmailVerified: u.EmailVerified, CreatedAt: u.CreatedAt,
		TermsVersion: acct.TermsVersion, TermsAcceptedAt: acct.TermsAcceptedAt, Pages: []*exportPage{}, Uploads: []*exportUpload{}}
	pages, err := s.store.PagesByUser(ctx, u.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	for _, p := range pages {
		ep := &exportPage{Page: p, Domains: []*exportDomain{}}
		if ep.Blocks, err = s.store.Blocks(ctx, p.ID); err != nil {
			s.serverErr(w, r, err)
			return
		}
		doms, err := s.store.Domains(ctx, p.ID)
		if err != nil {
			s.serverErr(w, r, err)
			return
		}
		for _, d := range doms {
			ep.Domains = append(ep.Domains, &exportDomain{Hostname: d.Hostname, Verified: d.Verified, CreatedAt: d.CreatedAt})
		}
		if ep.Stats, err = s.store.StatRows(ctx, p.ID); err != nil {
			s.serverErr(w, r, err)
			return
		}
		out.Pages = append(out.Pages, ep)
	}
	uploads, err := s.store.UploadsByUser(ctx, u.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	for _, up := range uploads {
		out.Uploads = append(out.Uploads, &exportUpload{ID: up.ID, ContentType: up.ContentType, Size: up.Size,
			File: "uploads/" + up.ID + uploadExt(up.ContentType)})
	}
	manifest, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		s.serverErr(w, r, err)
		return
	}

	name := safeFilename(strings.ToLower(s.cfg.AppName) + "-export-" + out.ExportedAt.Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.zip"`)
	zw := zip.NewWriter(w)
	if err := writeZipFile(zw, "account.json", manifest, zip.Deflate); err != nil {
		s.log.Error("export", "err", err)
		return
	}
	for _, up := range out.Uploads {
		data, err := os.ReadFile(s.uploadPath(up.ID))
		if err != nil {
			s.log.Error("export: read upload", "upload", up.ID, "err", err)
			continue
		}
		if err := writeZipFile(zw, up.File, data, zip.Store); err != nil {
			s.log.Error("export", "err", err)
			return
		}
	}
	if err := zw.Close(); err != nil {
		s.log.Error("export", "err", err)
	}
}

func writeZipFile(zw *zip.Writer, name string, data []byte, method uint16) error {
	f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now().UTC()})
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}
