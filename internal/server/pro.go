package server

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
	"git.subcult.tv/subculture-collective/zelda/internal/customcss"
	"git.subcult.tv/subculture-collective/zelda/internal/fonts"
	"git.subcult.tv/subculture-collective/zelda/internal/plans"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// ---- custom CSS ----

type cssResponse struct {
	CSS string `json:"css"`
	// Warnings lists what the sanitizer removed, with line numbers.
	Warnings []string `json:"warnings"`
	// Applied is false when the owner's plan doesn't include custom CSS, so
	// the stored CSS isn't shown on the page.
	Applied bool `json:"applied"`
}

// handleSetPageCSS sanitizes and stores a page's custom CSS. Setting CSS
// needs Pro; clearing it works on every plan.
func (s *Server) handleSetPageCSS(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	var body struct {
		CSS *string `json:"css"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.CSS == nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "send css as a string; use \"\" to remove it", "css")
		return
	}
	if len(*body.CSS) > customcss.MaxInput {
		writeErr(w, http.StatusUnprocessableEntity, "too_large",
			fmt.Sprintf("custom CSS must be %d KB or smaller", customcss.MaxInput>>10), "css")
		return
	}
	limits, ok := s.limitsFor(w, r)
	if !ok {
		return
	}
	entitled := limits.Features[plans.CustomCSS]
	if strings.TrimSpace(*body.CSS) != "" && !entitled {
		writeUpgrade(w, string(plans.CustomCSS), upgradeMessage(plans.CustomCSS), "css")
		return
	}
	res := customcss.Sanitize(*body.CSS, customcss.UploadChecker(s.uploadChecker(r)))
	if len(res.CSS) > customcss.MaxOutput {
		writeErr(w, http.StatusUnprocessableEntity, "too_large", "custom CSS is too large after scoping; shorten it", "css")
		return
	}
	if err := s.store.SetPageCSS(r.Context(), p.ID, res.CSS); err != nil {
		s.serverErr(w, r, err)
		return
	}
	if res.Warnings == nil {
		res.Warnings = []string{}
	}
	writeJSON(w, http.StatusOK, cssResponse{CSS: res.CSS, Warnings: res.Warnings, Applied: entitled && res.CSS != ""})
}

// ---- fonts ----

// handleFontUpload stores a WOFF2, WOFF, TTF or OTF font (Pro).
func (s *Server) handleFontUpload(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.requireFeature(w, r, plans.CustomFonts, "file") {
		return
	}
	if !s.uploadLimit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "upload limit reached, try again later", "")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "attach a font in the file field", "file")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, fonts.MaxSize+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "could not read upload", "file")
		return
	}
	if len(data) > fonts.MaxSize {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "fonts must be 1 MB or smaller", "file")
		return
	}
	ct := fonts.Sniff(data)
	if ct == "" {
		writeErr(w, http.StatusUnprocessableEntity, "unsupported", "use a WOFF2, WOFF, TTF or OTF font file", "file")
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

// checkThemeFonts validates theme.font_uploads on a theme update: each ID
// must be a font the user uploaded, and choosing a new font needs Pro.
// Keeping or clearing the stored fonts works on every plan.
func (s *Server) checkThemeFonts(w http.ResponseWriter, r *http.Request, next, prev catalog.Theme) bool {
	if next.FontUploads == nil {
		return true
	}
	var old catalog.FontUploads
	if prev.FontUploads != nil {
		old = *prev.FontUploads
	}
	changed := false
	for _, f := range []struct{ id, old string }{{next.FontUploads.Body, old.Body}, {next.FontUploads.Display, old.Display}} {
		if f.id == "" {
			continue
		}
		if !uuidRe.MatchString(f.id) {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "unknown font", "font_uploads")
			return false
		}
		up, err := s.store.Upload(r.Context(), f.id)
		if errors.Is(err, store.ErrNotFound) || err == nil && (up.UserID != userFrom(r).ID || !fonts.IsFont(up.ContentType)) {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "unknown font", "font_uploads")
			return false
		} else if err != nil {
			s.serverErr(w, r, err)
			return false
		}
		if f.id != f.old {
			changed = true
		}
	}
	return !changed || s.requireFeature(w, r, plans.CustomFonts, "font_uploads")
}

// ---- rendering ----

// proStyle is what a page's owner plan adds to the rendered page.
type proStyle struct {
	CSS         string
	FontBody    string
	FontDisplay string
}

// proStyleFor loads custom CSS and fonts for p, keeping only what the
// owner's plan includes now. Errors render the page without them.
func (s *Server) proStyleFor(ctx context.Context, p *store.Page, theme catalog.Theme) proStyle {
	var ps proStyle
	a, err := s.store.PlanOf(ctx, p.UserID)
	if err != nil {
		s.log.Error("owner plan", "user", p.UserID, "err", err)
		return ps
	}
	pol, now := s.policy(), s.now()
	if f := theme.FontUploads; f != nil && pol.Can(a, plans.CustomFonts, now) {
		if uuidRe.MatchString(f.Body) {
			ps.FontBody = uploadURL(f.Body)
		}
		if uuidRe.MatchString(f.Display) {
			ps.FontDisplay = uploadURL(f.Display)
		}
	}
	if pol.Can(a, plans.CustomCSS, now) {
		css, err := s.store.PageCSS(ctx, p.ID)
		if err != nil {
			s.log.Error("page css", "page", p.ID, "err", err)
		}
		ps.CSS = css
	}
	return ps
}

// ---- stats export ----

// handleStatsCSV streams the page's daily aggregates as one CSV table with
// the columns date, metric, key, label, count. metric is views, clicks,
// referrer or country. Needs the stats_export feature; days is capped like
// GET /stats.
func (s *Server) handleStatsCSV(w http.ResponseWriter, r *http.Request) {
	p := s.ownedPage(w, r)
	if p == nil {
		return
	}
	limits, ok := s.limitsFor(w, r)
	if !ok {
		return
	}
	if !limits.Features[plans.StatsExport] {
		writeUpgrade(w, string(plans.StatsExport), upgradeMessage(plans.StatsExport), "")
		return
	}
	maxDays := limits.StatsDays
	if s.cfg.StatsRetentionDays > 0 {
		maxDays = min(maxDays, s.cfg.StatsRetentionDays)
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 {
		days = DefaultStatsDays
	}
	days = min(days, maxDays)
	rows, err := s.store.StatsExport(r.Context(), p.ID, days)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	name := safeFilename(p.Slug + "-stats-" + strconv.Itoa(days) + "d-" + s.now().UTC().Format(time.DateOnly))
	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"date", "metric", "key", "label", "count"})
	for _, row := range rows {
		label := ""
		if row.Metric == "clicks" {
			label = "Deleted block"
			if row.BlockType != "" {
				label = blocks.Label(row.BlockType, row.BlockData)
			}
		}
		_ = cw.Write([]string{row.Day, row.Metric, csvCell(row.Key), csvCell(label), strconv.FormatInt(row.Count, 10)})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		s.log.Error("stats csv", "err", err)
	}
}
