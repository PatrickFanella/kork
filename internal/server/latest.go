package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/feed"
	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// Latest video/post blocks (#11). The newest feed item is cached per block
// in Postgres. Rendering never waits on a stale entry: it serves the cached
// item and refreshes in the background. A block with no cached item waits
// up to feedWait for its first fetch.

const (
	feedAccept       = "application/atom+xml, application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.1"
	youtubeHandleTTL = 30 * 24 * time.Hour
	maxYouTubePage   = 4 << 20
)

var errNoChannel = errors.New("couldn't find that YouTube channel")

// SetYouTubeBase points YouTube lookups at another origin (tests).
func (s *Server) SetYouTubeBase(base string) { s.x.youtubeBase = base }

// WaitFeedRefreshes blocks until background feed refreshes finish (tests).
func (s *Server) WaitFeedRefreshes() { s.x.wg.Wait() }

// feedURL returns the RSS/Atom address for a latest block source, resolving
// a YouTube @handle to its channel ID (cached).
func (s *Server) feedURL(ctx context.Context, source string) (string, error) {
	kind, v := blocks.SourceKind(source)
	base := s.x.youtubeBase
	switch kind {
	case blocks.SourceYouTubeChannel:
		return base + "/feeds/videos.xml?channel_id=" + url.QueryEscape(v), nil
	case blocks.SourceYouTubeUser:
		return base + "/feeds/videos.xml?user=" + url.QueryEscape(v), nil
	case blocks.SourceYouTubeHandle:
		if id, at, err := s.store.YouTubeChannel(ctx, v); err == nil && s.now().Sub(at) < youtubeHandleTTL {
			return base + "/feeds/videos.xml?channel_id=" + id, nil
		}
		res, err := s.fetcher.Get(ctx, base+"/@"+url.PathEscape(v), htmlAccept, maxYouTubePage)
		if err != nil {
			if errors.Is(err, fetch.ErrStatus) {
				return "", errNoChannel
			}
			return "", err
		}
		id, ok := feed.ChannelID(res.Body)
		if !ok {
			return "", errNoChannel
		}
		if err := s.store.SaveYouTubeChannel(ctx, v, id, s.now()); err != nil {
			s.log.Warn("cache youtube channel", "err", err)
		}
		return base + "/feeds/videos.xml?channel_id=" + id, nil
	}
	return v, nil
}

// fetchLatest fetches a source's feed and returns its newest item.
func (s *Server) fetchLatest(ctx context.Context, source string) (feed.Item, error) {
	u, err := s.feedURL(ctx, source)
	if err != nil {
		return feed.Item{}, err
	}
	res, err := s.fetcher.Get(ctx, u, feedAccept, fetch.MaxHTML)
	if err != nil {
		return feed.Item{}, err
	}
	return feed.Latest(res.Body, res.URL)
}

func latestErrMessage(err error) string {
	var ne interface{ Timeout() bool }
	switch {
	case errors.Is(err, errNoChannel):
		return "couldn't find that YouTube channel"
	case errors.Is(err, feed.ErrNotFeed):
		return "that address isn't an RSS or Atom feed"
	case errors.Is(err, feed.ErrNoItems):
		return "that feed has no posts yet"
	case errors.Is(err, fetch.ErrBlocked):
		return "that address is on a private or local network, so it can't be fetched"
	case errors.Is(err, fetch.ErrURL):
		return "enter a public http(s) address on the standard port"
	case errors.Is(err, fetch.ErrTooLarge):
		return "that feed is too large to read"
	case errors.Is(err, fetch.ErrRedirects):
		return "that address redirects too many times"
	case errors.Is(err, fetch.ErrStatus):
		return "the site didn't return the feed"
	case errors.As(err, &ne) && ne.Timeout(), errors.Is(err, context.DeadlineExceeded):
		return "the site took too long to respond"
	}
	return "couldn't reach that site"
}

// prepareLatest checks a new or changed source by fetching it, so a bad
// address is reported in the editor. The returned function caches the item
// once the block exists.
func (s *Server) prepareLatest(w http.ResponseWriter, r *http.Request, existing *store.Block, data json.RawMessage) (func(context.Context, *store.Block) error, bool) {
	var d blocks.Latest
	_ = json.Unmarshal(data, &d)
	if existing != nil && existing.Type == "latest" {
		var old blocks.Latest
		_ = json.Unmarshal(existing.Data, &old)
		if old.Source == d.Source {
			return nil, true
		}
	}
	u := userFrom(r)
	if !s.previewLimit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many feed checks, try again later", "")
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	item, err := s.fetchLatest(ctx, d.Source)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "feed_failed", latestErrMessage(err), "source")
		return nil, false
	}
	return func(ctx context.Context, b *store.Block) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		prev, _ := s.store.FeedEntries(ctx, []string{b.ID})
		s.saveLatest(ctx, b.ID, u.ID, d.Source, item, nil, prev[b.ID])
		return nil
	}, true
}

// saveLatest caches a fetch result. On failure the previous item is kept
// and only the attempt is recorded. A new thumbnail is copied into an
// upload owned by the page owner; the previous copy is deleted.
func (s *Server) saveLatest(ctx context.Context, blockID, ownerID, source string, item feed.Item, fetchErr error, prev *store.FeedEntry) {
	now := s.now()
	if prev != nil && prev.Source != source {
		if prev.ThumbnailUploadID != nil {
			s.removeUpload(ctx, *prev.ThumbnailUploadID)
		}
		prev = nil
	}
	if fetchErr != nil {
		e := &store.FeedEntry{BlockID: blockID, Source: source}
		if prev != nil {
			c := *prev
			e = &c
		}
		e.CheckedAt, e.Error = now, latestErrMessage(fetchErr)
		if _, err := s.store.SaveFeedEntry(ctx, e); err != nil {
			s.log.Warn("save feed cache", "err", err)
		}
		return
	}
	e := &store.FeedEntry{BlockID: blockID, Source: source, ItemKey: item.Key, Title: clipText(item.Title, 120),
		Link: item.Link, CheckedAt: now, FetchedAt: &now}
	if !item.Published.IsZero() {
		at := item.Published
		e.PublishedAt = &at
	}
	var created string
	switch {
	case item.Thumbnail == "":
	case prev != nil && prev.ThumbnailUploadID != nil && prev.ThumbnailURL == item.Thumbnail:
		e.ThumbnailURL, e.ThumbnailUploadID = prev.ThumbnailURL, prev.ThumbnailUploadID
	default:
		up, err := s.importImage(ctx, ownerID, item.Thumbnail)
		if err != nil {
			s.log.Info("feed thumbnail skipped", "block", blockID, "err", err)
			break
		}
		created, _ = up["id"].(string)
		e.ThumbnailURL, e.ThumbnailUploadID = item.Thumbnail, &created
	}
	saved, err := s.store.SaveFeedEntry(ctx, e)
	if err != nil {
		s.log.Warn("save feed cache", "err", err)
	}
	if !saved {
		if created != "" {
			s.removeUpload(ctx, created)
		}
		return
	}
	if prev != nil && prev.ThumbnailUploadID != nil && (e.ThumbnailUploadID == nil || *e.ThumbnailUploadID != *prev.ThumbnailUploadID) {
		s.removeUpload(ctx, *prev.ThumbnailUploadID)
	}
}

func (s *Server) removeUpload(ctx context.Context, id string) {
	if err := s.store.DeleteUpload(ctx, id); err != nil {
		s.log.Warn("delete upload", "upload", id, "err", err)
		return
	}
	if err := os.Remove(s.uploadPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.log.Warn("remove upload file", "upload", id, "err", err)
	}
}

// refreshLatest fetches a block's feed in the background, at most once at a
// time per block and a few at a time overall. The channel closes when the
// refresh finishes, or at once if no refresh could start.
func (s *Server) refreshLatest(blockID, ownerID, source string) <-chan struct{} {
	s.x.mu.Lock()
	if ch, ok := s.x.inflight[blockID]; ok {
		s.x.mu.Unlock()
		return ch
	}
	ch := make(chan struct{})
	select {
	case s.x.slots <- struct{}{}:
	default:
		s.x.mu.Unlock()
		close(ch)
		return ch
	}
	s.x.inflight[blockID] = ch
	s.x.wg.Add(1)
	s.x.mu.Unlock()
	go func() {
		defer func() {
			s.x.mu.Lock()
			delete(s.x.inflight, blockID)
			s.x.mu.Unlock()
			<-s.x.slots
			close(ch)
			s.x.wg.Done()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		prev, err := s.store.FeedEntries(ctx, []string{blockID})
		if err != nil {
			s.log.Warn("load feed cache", "err", err)
			return
		}
		item, err := s.fetchLatest(ctx, source)
		s.saveLatest(ctx, blockID, ownerID, source, item, err, prev[blockID])
	}()
	return ch
}

func (s *Server) feedStale(e *store.FeedEntry, now time.Time) bool {
	ttl := s.x.feedTTL
	if e.Error != "" {
		ttl = s.x.feedRetry
	}
	return now.Sub(e.CheckedAt) >= ttl
}

// latestItems loads cached items for a page's latest blocks and starts
// refreshes for stale or missing ones.
func (s *Server) latestItems(ctx context.Context, p *store.Page, list []blocks.Block) func(string) (blocks.LatestItem, bool) {
	sources := map[string]string{}
	var ids []string
	for _, b := range list {
		if b.Type != "latest" {
			continue
		}
		var d blocks.Latest
		if json.Unmarshal(b.Data, &d) == nil && d.Source != "" {
			sources[b.ID] = d.Source
			ids = append(ids, b.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	cache, err := s.store.FeedEntries(ctx, ids)
	if err != nil {
		s.log.Warn("load feed cache", "err", err)
		cache = map[string]*store.FeedEntry{}
	}
	now := s.now()
	var waits []<-chan struct{}
	for _, id := range ids {
		e := cache[id]
		switch {
		case e == nil || e.Source != sources[id]:
			waits = append(waits, s.refreshLatest(id, p.UserID, sources[id]))
		case s.feedStale(e, now):
			s.refreshLatest(id, p.UserID, sources[id])
		}
	}
	if len(waits) > 0 {
		timer := time.NewTimer(s.x.feedWait)
	wait:
		for _, ch := range waits {
			select {
			case <-ch:
			case <-timer.C:
				break wait
			case <-ctx.Done():
				break wait
			}
		}
		timer.Stop()
		if fresh, err := s.store.FeedEntries(ctx, ids); err == nil {
			cache = fresh
		}
	}
	return func(id string) (blocks.LatestItem, bool) {
		e := cache[id]
		if e == nil || e.Source != sources[id] || e.Link == "" {
			return blocks.LatestItem{}, false
		}
		li := blocks.LatestItem{Title: e.Title, Link: e.Link}
		if e.PublishedAt != nil {
			li.Published = *e.PublishedAt
		}
		if e.ThumbnailUploadID != nil {
			li.ThumbnailID = *e.ThumbnailUploadID
		}
		return li, true
	}
}

// prepareExtras runs the per-type steps that need the server: the sign-up
// block's Brevo key and the latest block's feed check. It returns the data
// to store and a function to call with the saved block.
func (s *Server) prepareExtras(w http.ResponseWriter, r *http.Request, existing *store.Block, typ string, data json.RawMessage,
	secrets map[string]string) (json.RawMessage, func(context.Context, *store.Block) error, bool) {
	switch typ {
	case "email_signup":
		return s.prepareSignup(w, r, existing, data, secrets)
	case "latest":
		after, ok := s.prepareLatest(w, r, existing, data)
		return data, after, ok
	}
	return data, nil, true
}

// blockDeleteCleanup returns what to remove once a block is deleted: a
// latest block's thumbnail copy. Call it before deleting; run the result
// after.
func (s *Server) blockDeleteCleanup(ctx context.Context, blockID string) func() {
	thumb, ok := s.store.FeedThumbnail(ctx, blockID)
	if !ok {
		return func() {}
	}
	return func() { s.removeUpload(context.WithoutCancel(ctx), thumb) }
}
