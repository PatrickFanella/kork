package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/analytics"
	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

const (
	// MaxStatsDays is the longest range the stats API returns. Every account
	// gets 30 days for now; a paid plan can raise it, since rows are kept for
	// ZELDA_STATS_RETENTION_DAYS.
	MaxStatsDays = 30
	// statsTopN is how many referrers and countries the stats API lists.
	statsTopN = 20
)

// countView records a public page view as aggregates: the day, the referrer
// domain and the visitor's country from Cloudflare. Nothing about the visitor
// is stored.
func (s *Server) countView(r *http.Request, pageID string) {
	ref := analytics.ReferrerHost(r.Referer(), hostOnly(r.Host))
	country := analytics.Country(r.Header.Get("CF-IPCountry"))
	if err := s.store.CountView(r.Context(), pageID, ref, country); err != nil {
		s.log.Error("count view", "err", err)
	}
}

type blockStat struct {
	BlockID string  `json:"block_id"`
	Label   string  `json:"label"`
	Count   int64   `json:"count"`
	CTR     float64 `json:"ctr"`
}

type statsResponse struct {
	Days       int               `json:"days"`
	TotalViews int64             `json:"total_views"`
	Views      []store.DayCount  `json:"views"`
	Blocks     []blockStat       `json:"blocks"`
	Referrers  []store.NameCount `json:"referrers"`
	Countries  []store.NameCount `json:"countries"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 {
		days = MaxStatsDays
	}
	days = min(days, MaxStatsDays)
	st, err := s.store.Stats(r.Context(), p.ID, days)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	refs, countries, err := s.store.Breakdown(r.Context(), p.ID, days, statsTopN)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	out := statsResponse{Days: days, TotalViews: st.TotalViews, Views: st.Views, Blocks: make([]blockStat, len(st.Blocks)),
		Referrers: refs, Countries: countries}
	for i, b := range st.Blocks {
		out.Blocks[i] = blockStat{BlockID: b.BlockID, Label: blocks.Label(b.Type, b.Data), Count: b.Count,
			CTR: analytics.ClickThrough(b.Count, st.TotalViews)}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- account settings ----

func (s *Server) handleGetAccountSettings(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.AccountSettings(r.Context(), userFrom(r).ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleUpdateAccountSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WeeklyStatsEmail *bool `json:"weekly_stats_email"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	u := userFrom(r)
	if body.WeeklyStatsEmail != nil {
		if *body.WeeklyStatsEmail && !u.EmailVerified {
			writeErr(w, http.StatusForbidden, "email_unverified", "verify your email to get weekly stats", "weekly_stats_email")
			return
		}
		if err := s.store.SetWeeklyStatsEmail(r.Context(), u.ID, *body.WeeklyStatsEmail, analytics.WeekStart(time.Now())); err != nil {
			s.serverErr(w, r, err)
			return
		}
	}
	s.handleGetAccountSettings(w, r)
}

// handleUnsubscribeWeekly turns the weekly stats email off from a signed
// link. GET is the link in the email; POST is RFC 8058 one-click unsubscribe
// from mail clients.
func (s *Server) handleUnsubscribeWeekly(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page := render.Simple{AppName: s.cfg.AppName, BaseURL: s.cfg.BaseURL}
	if !s.trackLimit.allow("unsub:" + s.clientIP(r)) {
		page.Title, page.Heading, page.Message = "Try again later", "Too many requests", "Wait a minute and open the link again."
		s.writeSimple(w, http.StatusTooManyRequests, page)
		return
	}
	userID, err := s.unsubscribeUser(r)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("unsubscribe", "err", err)
		}
		page.Title, page.Heading = "Link not valid", "This link isn't valid"
		page.Message = "Log in and switch off weekly emails in the Stats tab of any page."
		s.writeSimple(w, http.StatusBadRequest, page)
		return
	}
	if err := s.store.SetWeeklyStatsEmail(r.Context(), userID, false, time.Now()); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("unsubscribe", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	page.Title, page.Heading = "Unsubscribed", "Weekly stats emails are off"
	page.Message = "You can turn them back on in the Stats tab of any page."
	s.writeSimple(w, http.StatusOK, page)
}

func (s *Server) unsubscribeUser(r *http.Request) (string, error) {
	key, err := s.store.SigningKey(r.Context(), analytics.UnsubscribeKeyName)
	if err != nil {
		return "", err
	}
	id, ok := analytics.VerifyUnsubscribe(key, r.URL.Query().Get("token"))
	if !ok || !uuidRe.MatchString(id) {
		return "", store.ErrNotFound
	}
	return id, nil
}

func (s *Server) writeSimple(w http.ResponseWriter, status int, page render.Simple) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = render.WriteSimple(w, page)
}
