package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/screen"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// RecheckBatch is how many published pages one RecheckOnce pass screens.
const RecheckBatch = 200

// SetScreener turns on destination screening. nil turns it off.
func (s *Server) SetScreener(sc screen.Screener) { s.screener = sc }

// blockedHit is an unsafe destination found in a block.
type blockedHit struct {
	Field   string
	URL     string
	Verdict store.Verdict
	Block   *store.Block
}

// unsafeURLs screens urls, using and filling the verdict cache, and returns
// the unsafe ones. When the screener fails, unsafe results it did return are
// still used and cached; safe results are not cached so the URL is retried.
func (s *Server) unsafeURLs(ctx context.Context, urls []string) (map[string]store.Verdict, error) {
	out := map[string]store.Verdict{}
	if s.screener == nil || len(urls) == 0 {
		return out, nil
	}
	seen := map[string]bool{}
	var uniq []string
	for _, u := range urls {
		if !seen[u] {
			seen[u] = true
			uniq = append(uniq, u)
		}
	}
	cached, err := s.store.CachedVerdicts(ctx, uniq)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, u := range uniq {
		if v, ok := cached[u]; !ok {
			missing = append(missing, u)
		} else if v.Unsafe {
			out[u] = v
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, checkErr := s.screener.Check(checkCtx, missing)
	for _, u := range missing {
		r, ok := res[u]
		v := store.Verdict{URL: u, Unsafe: r.Unsafe, Threat: r.Threat, Source: r.Source}
		ttl := s.safeTTL
		switch {
		case v.Unsafe:
			out[u] = v
			ttl = s.unsafeTTL
		case !ok || checkErr != nil:
			continue
		}
		if ttl > 0 {
			if err := s.store.CacheVerdict(ctx, v, ttl); err != nil {
				s.log.Warn("cache screening verdict", "err", err)
			}
		}
	}
	return out, checkErr
}

// blockHits returns the unsafe destinations among the given blocks.
func (s *Server) blockHits(ctx context.Context, list []*store.Block) ([]blockedHit, error) {
	type ref struct {
		dest  blocks.Destination
		block *store.Block
	}
	var refs []ref
	var urls []string
	for _, b := range list {
		for _, d := range blocks.Destinations(b.Type, b.Data) {
			refs = append(refs, ref{d, b})
			urls = append(urls, d.URL)
		}
	}
	bad, err := s.unsafeURLs(ctx, urls)
	var hits []blockedHit
	for _, r := range refs {
		if v, ok := bad[r.dest.URL]; ok {
			hits = append(hits, blockedHit{Field: r.dest.Field, URL: r.dest.URL, Verdict: v, Block: r.block})
		}
	}
	return hits, err
}

func blockedMessage(u string) string {
	host := screen.Hostname(u)
	if host == "" {
		host = u
	}
	return "This address (" + host + ") is on a phishing or malware blocklist, so it can't be added. Use a different link."
}

// rejectBlocked screens a block's destinations before it is saved. It writes
// a 422 and returns true when one is unsafe. If the screener is unavailable
// the block is allowed and the periodic re-check catches it later.
func (s *Server) rejectBlocked(w http.ResponseWriter, r *http.Request, typ string, data json.RawMessage) bool {
	hits, err := s.blockHits(r.Context(), []*store.Block{{Type: typ, Data: data}})
	if err != nil {
		s.log.Warn("destination screening incomplete; allowing unflagged URLs", "err", err)
	}
	if len(hits) == 0 {
		return false
	}
	writeErr(w, http.StatusUnprocessableEntity, "blocked_destination", blockedMessage(hits[0].URL), hits[0].Field)
	return true
}

// publishUnlocksAt returns when u may publish, and whether u may publish now.
func (s *Server) publishUnlocksAt(ctx context.Context, u *store.User) (time.Time, bool, error) {
	if u.IsAdmin || s.cfg.PublishMinAge <= 0 {
		return time.Time{}, true, nil
	}
	unlocks := u.CreatedAt.Add(s.cfg.PublishMinAge)
	if !time.Now().Before(unlocks) {
		return unlocks, true, nil
	}
	trusted, err := s.store.UserTrustedAt(ctx, u.ID)
	if err != nil {
		return unlocks, false, err
	}
	return unlocks, trusted != nil, nil
}

// allowPublish applies the new-account gate and screens the page's blocks
// before a draft goes live. It writes the error response and returns false
// when publishing must not proceed.
func (s *Server) allowPublish(w http.ResponseWriter, r *http.Request, u *store.User, p *store.Page) bool {
	unlocks, ok, err := s.publishUnlocksAt(r.Context(), u)
	if err != nil {
		s.serverErr(w, r, err)
		return false
	}
	if !ok {
		writeErr(w, http.StatusForbidden, "account_too_new",
			"New accounts can publish from "+unlocks.UTC().Format("January 2, 2006 at 15:04 UTC")+
				". An admin can approve your account sooner.", "published")
		return false
	}
	list, err := s.store.Blocks(r.Context(), p.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return false
	}
	hits, err := s.blockHits(r.Context(), list)
	if err != nil {
		s.log.Warn("destination screening incomplete; allowing unflagged URLs", "err", err)
	}
	if len(hits) > 0 {
		h := hits[0]
		writeErr(w, http.StatusUnprocessableEntity, "blocked_destination",
			"Remove or change the block \""+blocks.Label(h.Block.Type, h.Block.Data)+"\" before publishing: "+
				screen.Hostname(h.URL)+" is on a phishing or malware blocklist.", "published")
		return false
	}
	return true
}

// RecheckOnce re-screens the least recently checked published pages. A page
// with an unsafe destination is unpublished and reported for admins with
// reason "phishing". It returns how many pages were checked and unpublished.
func (s *Server) RecheckOnce(ctx context.Context) (checked, unpublished int, err error) {
	if s.screener == nil {
		return 0, 0, nil
	}
	pages, err := s.store.PagesToRescreen(ctx, RecheckBatch)
	if err != nil {
		return 0, 0, err
	}
	var errs []error
	for _, p := range pages {
		if ctx.Err() != nil {
			break
		}
		list, err := s.store.Blocks(ctx, p.ID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		enabled := list[:0]
		for _, b := range list {
			if b.Enabled {
				enabled = append(enabled, b)
			}
		}
		hits, herr := s.blockHits(ctx, enabled)
		if herr != nil {
			// Leave the page unmarked so it is retried first next time,
			// but still act on anything that was flagged.
			errs = append(errs, herr)
		} else if err := s.store.MarkPageScreened(ctx, p.ID); err != nil {
			errs = append(errs, err)
		}
		checked++
		if len(hits) == 0 {
			continue
		}
		done, err := s.takeDown(ctx, p, hits[0])
		if err != nil {
			errs = append(errs, err)
		} else if done {
			unpublished++
		}
	}
	if err := s.store.PurgeExpiredVerdicts(ctx); err != nil {
		errs = append(errs, err)
	}
	return checked, unpublished, errors.Join(errs...)
}

func (s *Server) takeDown(ctx context.Context, p *store.Page, h blockedHit) (bool, error) {
	source := h.Verdict.Source
	if source == "" {
		source = "a blocklist"
	}
	details := fmt.Sprintf("Automatic screening: %s is flagged by %s (%s) in block %q. The page was unpublished.",
		h.URL, source, h.Verdict.Threat, blocks.Label(h.Block.Type, h.Block.Data))
	done, err := s.store.UnpublishForAbuse(ctx, p.ID, "phishing", details)
	if err != nil || !done {
		return done, err
	}
	s.log.Warn("unpublished page with unsafe destination", "page", p.ID, "slug", p.Slug, "url", h.URL, "source", source)
	if owner, err := s.store.UserByID(ctx, p.UserID); err == nil {
		s.sendMail(mail.Message{To: owner.Email, Subject: "Your " + s.cfg.AppName + " page was unpublished",
			Body: "We unpublished " + s.pageURL(p.Slug) + " because it links to " + h.URL + ", which " + source +
				" lists as phishing or malware.\n\nRemove or change that link in the editor, then publish the page again.\n"})
	}
	return true, nil
}

// RunRechecks calls RecheckOnce a minute after start and then every interval
// until ctx is done.
func (s *Server) RunRechecks(ctx context.Context, every time.Duration) {
	if s.screener == nil {
		return
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		checked, unpublished, err := s.RecheckOnce(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("destination re-check incomplete", "err", err)
		}
		s.log.Info("destination re-check", "pages", checked, "unpublished", unpublished)
		t.Reset(every)
	}
}

// ---- admin: new-account gate ----

func (s *Server) handleAdminPendingUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.PendingUsers(r.Context(), s.cfg.PublishMinAge)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	// Verified ZELDA_ADMIN_EMAILS accounts are admins and bypass the gate.
	out := users[:0]
	for _, u := range users {
		if !(u.EmailVerified && s.cfg.AdminEmails[strings.ToLower(u.Email)]) {
			out = append(out, u)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAdminTrustUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "user not found", "")
		return
	}
	if err := s.store.TrustUser(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "user not found", "")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
