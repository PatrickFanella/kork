package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/billing"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// patreonStateTTL is how long a Connect Patreon link stays valid.
const patreonStateTTL = 15 * time.Minute

// patreonState makes the OAuth state for a session: a timestamp and nonce,
// with an HMAC over them and the session token keyed from ZELDA_SECRET_KEY.
// A callback is only accepted in the session that started it, which stops
// an attacker from linking their Patreon to someone else's account.
func (s *Server) patreonState(session string, now time.Time) string {
	raw := make([]byte, 8+16)
	binary.BigEndian.PutUint64(raw, uint64(now.Unix()))
	_, _ = rand.Read(raw[8:])
	p := base64.RawURLEncoding.EncodeToString(raw)
	return p + "." + s.patreonStateMAC(session, p)
}

func (s *Server) patreonStateMAC(session, payload string) string {
	k := hmac.New(sha256.New, s.cfg.SecretKey)
	k.Write([]byte("kork patreon oauth state v1"))
	m := hmac.New(sha256.New, k.Sum(nil))
	m.Write([]byte(session))
	m.Write([]byte{0})
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Server) patreonStateValid(session, state string, now time.Time) bool {
	p, mac, ok := strings.Cut(state, ".")
	if !ok || session == "" || !hmac.Equal([]byte(mac), []byte(s.patreonStateMAC(session, p))) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil || len(raw) != 24 {
		return false
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(raw)), 0)
	age := now.Sub(issued)
	return age >= -time.Minute && age <= patreonStateTTL
}

func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// handlePatreonConnect redirects the browser to Patreon to approve access.
func (s *Server) handlePatreonConnect(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.PatreonEnabled() {
		writeErr(w, http.StatusNotFound, "not_found", "no such endpoint", "")
		return
	}
	if !s.bill.limit.allow(userFrom(r).ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many billing requests, try again later", "")
		return
	}
	http.Redirect(w, r, s.bill.patreon.AuthorizeURL(s.patreonState(sessionToken(r), s.now())), http.StatusFound)
}

// handlePatreonCallback finishes the connection. The editor page at
// /app/patreon/callback posts the code and state Patreon sent it.
func (s *Server) handlePatreonCallback(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.PatreonEnabled() {
		writeErr(w, http.StatusNotFound, "not_found", "no such endpoint", "")
		return
	}
	u := userFrom(r)
	var body struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !s.patreonStateValid(sessionToken(r), body.State, s.now()) {
		writeErr(w, http.StatusBadRequest, "bad_state", "This Patreon link expired or was opened in another browser session. Use Connect Patreon again.", "")
		return
	}
	if body.Code == "" || len(body.Code) > 512 {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "missing authorization code", "code")
		return
	}
	ctx := r.Context()
	tok, err := s.bill.patreon.Exchange(ctx, body.Code)
	if errors.Is(err, billing.ErrPatreonRevoked) {
		writeErr(w, http.StatusBadRequest, "patreon_denied", "Patreon didn't accept this approval. Use Connect Patreon again.", "")
		return
	}
	if err != nil {
		s.patreonUnavailable(w, r, err)
		return
	}
	id, err := s.bill.patreon.Identity(ctx, tok.AccessToken)
	if err != nil {
		s.patreonUnavailable(w, r, err)
		return
	}
	entitled, status := billing.Entitled(id, s.cfg.PatreonCampaignID, s.cfg.PatreonTierIDs)
	now := s.now()
	err = s.store.UpdateBilling(ctx, u.ID, func(b *store.Billing) error {
		if b.PatreonUserID != id.UserID {
			b.ClearPatreon() // a different Patreon account replaces the old one
		}
		b.PatreonUserID = id.UserID
		applyPatreonCheck(b, entitled, status, now)
		b.PatreonRefreshToken = nil
		if tok.RefreshToken != "" {
			b.PatreonRefreshToken = s.bill.sealer.Seal([]byte(tok.RefreshToken), []byte(u.ID))
		}
		return nil
	})
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "patreon_in_use", "This Patreon account is already connected to another Kork account. Disconnect it there first.", "")
		return
	}
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	s.log.Info("patreon connected", "user", u.ID, "entitled", entitled, "patron_status", status)
	s.writeBillingView(w, r, u.ID)
}

// handlePatreonDisconnect removes the connection and the Pro it granted.
func (s *Server) handlePatreonDisconnect(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.PatreonEnabled() {
		writeErr(w, http.StatusNotFound, "not_found", "no such endpoint", "")
		return
	}
	u := userFrom(r)
	if err := s.store.UpdateBilling(r.Context(), u.ID, func(b *store.Billing) error { b.ClearPatreon(); return nil }); err != nil {
		s.serverErr(w, r, err)
		return
	}
	s.writeBillingView(w, r, u.ID)
}

func (s *Server) writeBillingView(w http.ResponseWriter, r *http.Request, userID string) {
	v, err := s.billingView(r.Context(), userID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) patreonUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("patreon", "path", r.URL.Path, "err", err)
	writeErr(w, http.StatusBadGateway, "billing_unavailable", "Patreon didn't respond. Try again in a few minutes.", "")
}

// applyPatreonCheck records a definite membership answer at now.
func applyPatreonCheck(b *store.Billing, entitled bool, status string, now time.Time) {
	next := billing.PatreonNext(billing.PatreonState{ProUntil: b.PatreonProUntil, LostAt: b.PatreonLostAt}, entitled, now)
	b.PatreonProUntil, b.PatreonLostAt = next.ProUntil, next.LostAt
	b.PatreonEntitled = entitled
	b.PatreonPatronStatus = status
	b.PatreonCheckedAt = &now
}

// CheckPatreonOnce re-checks every connected account not checked in the
// last day. A revoked authorization counts as no membership (grace, then
// Free); network or Patreon errors leave the state alone and are retried on
// the next run.
func (s *Server) CheckPatreonOnce(ctx context.Context) (checked, failed int) {
	// A little under a day, so a daily schedule doesn't drift past 24h.
	due, err := s.store.PatreonDue(ctx, s.now().Add(-billing.PatreonCheckEvery+time.Hour), 500)
	if err != nil {
		s.log.Warn("patreon due list", "err", err)
		return 0, 1
	}
	for _, l := range due {
		if ctx.Err() != nil {
			break
		}
		if err := s.checkPatreon(ctx, l); err != nil {
			failed++
			s.log.Warn("patreon re-check", "user", l.UserID, "err", err)
			continue
		}
		checked++
	}
	return checked, failed
}

func (s *Server) checkPatreon(ctx context.Context, l store.PatreonLink) error {
	entitled, status, newRefresh, revoked := false, "", "", false
	refresh, err := s.bill.sealer.Open(l.RefreshToken, []byte(l.UserID))
	if err != nil {
		revoked = true // missing token, or sealed under a different key
	} else {
		tok, err := s.bill.patreon.Refresh(ctx, string(refresh))
		switch {
		case errors.Is(err, billing.ErrPatreonRevoked):
			revoked = true
		case err != nil:
			return err
		default:
			newRefresh = tok.RefreshToken
			id, err := s.bill.patreon.Identity(ctx, tok.AccessToken)
			if err != nil && !errors.Is(err, billing.ErrPatreonRevoked) {
				// Keep the rotated refresh token even though the check failed.
				_ = s.store.UpdateBilling(ctx, l.UserID, func(b *store.Billing) error {
					if b.PatreonUserID != "" && newRefresh != "" {
						b.PatreonRefreshToken = s.bill.sealer.Seal([]byte(newRefresh), []byte(l.UserID))
					}
					return nil
				})
				return err
			}
			if err != nil {
				revoked = true
			} else {
				entitled, status = billing.Entitled(id, s.cfg.PatreonCampaignID, s.cfg.PatreonTierIDs)
			}
		}
	}
	now := s.now()
	return s.store.UpdateBilling(ctx, l.UserID, func(b *store.Billing) error {
		if b.PatreonUserID == "" {
			return nil // disconnected meanwhile
		}
		applyPatreonCheck(b, entitled, status, now)
		switch {
		case revoked:
			b.PatreonRefreshToken = nil
		case newRefresh != "":
			b.PatreonRefreshToken = s.bill.sealer.Seal([]byte(newRefresh), []byte(l.UserID))
		}
		return nil
	})
}
