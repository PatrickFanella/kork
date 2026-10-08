package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

var emailRe = regexp.MustCompile(`^[^\s@<>"',;]+@[^\s@<>"',;]+\.[^\s@<>"',;]{2,}$`)

type credentials struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	AcceptTerms bool   `json:"accept_terms"` // signup only
}

func validPassword(w http.ResponseWriter, pw string) bool { return validPasswordFor(w, pw, "password") }

// validPasswordFor checks a new password and reports problems against field.
func validPasswordFor(w http.ResponseWriter, pw, field string) bool {
	n := utf8.RuneCountInString(pw)
	if n < 10 {
		writeErr(w, http.StatusUnprocessableEntity, "weak_password", "use at least 10 characters", field)
		return false
	}
	if len(pw) > 256 {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "password is too long", field)
		return false
	}
	return true
}

func (s *Server) setSession(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: int(store.SessionTTL.Seconds()),
	})
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !s.signupLimit.allow(s.clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many signups from this network, try again later", "")
		return
	}
	var c credentials
	if !decodeJSON(w, r, &c) {
		return
	}
	c.Email = strings.TrimSpace(c.Email)
	if len(c.Email) > 254 || !emailRe.MatchString(c.Email) {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "enter a valid email address", "email")
		return
	}
	if !validPassword(w, c.Password) {
		return
	}
	if !c.AcceptTerms {
		writeErr(w, http.StatusUnprocessableEntity, "terms_required", "confirm you are at least 18 and agree to the Terms and Privacy Policy to continue", "accept_terms")
		return
	}
	hash, err := hashPassword(c.Password)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	u, err := s.store.CreateUserAccepting(r.Context(), c.Email, hash, render.TermsVersion)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "email_taken", "an account with that email already exists", "email")
		return
	}
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	tok, err := s.store.CreateSession(r.Context(), u.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	s.setSession(w, tok)
	s.sendVerification(r.Context(), u)
	writeJSON(w, http.StatusCreated, map[string]any{"user": u})
}

func (s *Server) sendVerification(ctx context.Context, u *store.User) {
	tok, err := s.store.CreateUserToken(ctx, u.ID, "verify", store.TokenTTL)
	if err != nil {
		s.log.Error("create verify token", "err", err)
		return
	}
	link := s.cfg.BaseURL + "/app/verify?token=" + tok
	s.sendMail(mail.Message{To: u.Email, Subject: "Confirm your " + s.cfg.AppName + " email",
		Body: "This is the email check for your new " + s.cfg.AppName + " account. Open the link to confirm this address:\n\n" + link +
			"\n\nThe link expires in 24 hours. You can keep building your page in the meantime; publishing needs a confirmed address." +
			"\n\nIf you didn't sign up, ignore this message.\n"})
}

// sendMail delivers in the background so slow SMTP never blocks a request
// or reveals whether an address exists.
func (s *Server) sendMail(m mail.Message) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.mail.Send(ctx, m); err != nil {
			s.log.Error("send mail", "subject", m.Subject, "err", err)
		}
	}()
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decodeJSON(w, r, &c) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if !s.loginIPLimit.allow(s.clientIP(r)) || !s.loginLimit.allow(email) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, wait 15 minutes", "")
		return
	}
	u, err := s.store.UserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverErr(w, r, err)
		return
	}
	hash := dummyHash
	if u != nil {
		hash = u.PasswordHash
	}
	ok, _ := checkPassword(hash, c.Password)
	if u == nil || !ok {
		writeErr(w, http.StatusUnauthorized, "invalid_credentials", "wrong email or password", "")
		return
	}
	if u.Suspended {
		writeErr(w, http.StatusForbidden, "suspended", "this account is suspended", "")
		return
	}
	tok, err := s.store.CreateSession(r.Context(), u.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	s.setSession(w, tok)
	writeJSON(w, http.StatusOK, map[string]any{"user": s.withRole(u)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), c.Value)
	}
	s.clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	plan, err := s.planView(r.Context(), u.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "plan": plan})
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
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
	userID, err := s.store.UseUserToken(r.Context(), body.Token, "verify")
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusBadRequest, "invalid_token", "this link is invalid or expired", "")
		return
	}
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if err := s.store.MarkEmailVerified(r.Context(), userID); err != nil {
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

func (s *Server) handleResend(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.EmailVerified {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.mailLimit.allow("verify:" + u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "we've sent a few already; check your spam folder", "")
		return
	}
	s.sendVerification(r.Context(), u)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleForgot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if s.mailLimit.allow("reset:"+email) && s.mailLimit.allow("reset-ip:"+s.clientIP(r)) {
		if u, err := s.store.UserByEmail(r.Context(), email); err == nil && !u.Suspended {
			if tok, err := s.store.CreateUserToken(r.Context(), u.ID, "reset", store.ResetTokenTTL); err == nil {
				link := s.cfg.BaseURL + "/app/reset?token=" + tok
				s.sendMail(mail.Message{To: u.Email, Subject: "Reset your " + s.cfg.AppName + " password",
					Body: "Someone asked to reset the password for your " + s.cfg.AppName + " account. Choose a new one here:\n\n" + link +
						"\n\nThe link expires in 1 hour. If that wasn't you, ignore this message and your password stays as it is.\n"})
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if !s.verifyLimit.allow(s.clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later", "")
		return
	}
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) || !validPassword(w, body.Password) {
		return
	}
	userID, err := s.store.UseUserToken(r.Context(), body.Token, "reset")
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusBadRequest, "invalid_token", "this link is invalid or expired", "")
		return
	}
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if err := s.store.SetPassword(r.Context(), userID, hash); err != nil {
		s.serverErr(w, r, err)
		return
	}
	// Proving control of the inbox also verifies it; drop every old session.
	_ = s.store.MarkEmailVerified(r.Context(), userID)
	_ = s.store.DeleteUserSessions(r.Context(), userID)
	w.WriteHeader(http.StatusNoContent)
}
