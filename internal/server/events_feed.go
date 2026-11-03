package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
	"git.subcult.tv/subculture-collective/zelda/internal/ics"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// Events blocks that read a public calendar feed (#39). Like latest blocks,
// the result is cached per block, served from the cache and refreshed in
// the background; a failed refresh keeps the last good dates.

// maxICS bounds a calendar feed's size.
const maxICS = 2 << 20

type eventFeedResult struct {
	events         []blocks.Event
	found, dropped int
}

// fetchEventFeed fetches and parses a calendar and screens its ticket links.
func (s *Server) fetchEventFeed(ctx context.Context, url string) (eventFeedResult, error) {
	res, err := s.fetcher.Get(ctx, url, "text/calendar, text/plain;q=0.5, */*;q=0.1", maxICS)
	if err != nil {
		return eventFeedResult{}, err
	}
	parsed, skipped, err := ics.Parse(res.Body)
	if err != nil {
		return eventFeedResult{}, err
	}
	evs, dropped := blocks.EventsFromFeed(parsed, s.now())
	var urls []string
	for _, e := range evs {
		if e.TicketURL != "" {
			urls = append(urls, e.TicketURL)
		}
	}
	if bad, err := s.unsafeURLs(ctx, urls); err != nil {
		s.log.Warn("calendar ticket link screening incomplete", "err", err)
	} else {
		for i := range evs {
			if _, ok := bad[evs[i].TicketURL]; ok {
				evs[i].TicketURL = "" // keep the date, drop the flagged link
			}
		}
	}
	return eventFeedResult{events: evs, found: len(evs), dropped: dropped + skipped}, nil
}

// eventFeedErrMessage explains a failed calendar fetch to the page owner.
func eventFeedErrMessage(err error) string {
	switch {
	case errors.Is(err, ics.ErrNotCalendar):
		return "That address doesn't return a calendar (.ics) feed."
	case errors.Is(err, fetch.ErrBlocked), errors.Is(err, fetch.ErrURL):
		return "That address can't be fetched. It must be a public web address."
	case errors.Is(err, fetch.ErrTooLarge):
		return "That calendar is larger than 2 MB."
	case errors.Is(err, fetch.ErrStatus):
		return "The calendar address didn't return the calendar. Check that the calendar is public."
	}
	return "Couldn't reach the calendar. Try again in a few minutes."
}

// saveEventFeed stores a fetch result. On failure the previous dates for the
// same address are kept and only the attempt and error are recorded.
func (s *Server) saveEventFeed(ctx context.Context, blockID, source string, r eventFeedResult, fetchErr error, prev *store.EventFeed) {
	now := s.now()
	f := &store.EventFeed{BlockID: blockID, Source: source, CheckedAt: now}
	if fetchErr != nil {
		f.Error = eventFeedErrMessage(fetchErr)
		if prev != nil && prev.Source == source {
			f.Events, f.Found, f.Dropped, f.FetchedAt = prev.Events, prev.Found, prev.Dropped, prev.FetchedAt
		}
	} else {
		raw, err := json.Marshal(r.events)
		if err != nil {
			return
		}
		f.Events, f.Found, f.Dropped, f.FetchedAt = raw, r.found, r.dropped, &now
	}
	if err := s.store.SaveEventFeed(ctx, f); err != nil {
		s.log.Warn("save calendar cache", "err", err)
	}
}

// refreshEventFeed fetches a block's calendar in the background, sharing
// the latest blocks' per-block guard and concurrency slots.
func (s *Server) refreshEventFeed(blockID, source string) <-chan struct{} {
	key := "ics:" + blockID
	s.x.mu.Lock()
	if ch, ok := s.x.inflight[key]; ok {
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
	s.x.inflight[key] = ch
	s.x.wg.Add(1)
	s.x.mu.Unlock()
	go func() {
		defer func() {
			s.x.mu.Lock()
			delete(s.x.inflight, key)
			s.x.mu.Unlock()
			<-s.x.slots
			close(ch)
			s.x.wg.Done()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		prev, err := s.store.EventFeeds(ctx, []string{blockID})
		if err != nil {
			s.log.Warn("load calendar cache", "err", err)
			return
		}
		r, err := s.fetchEventFeed(ctx, source)
		s.saveEventFeed(ctx, blockID, source, r, err, prev[blockID])
	}()
	return ch
}

func icsSource(b blocks.Block) (string, bool) {
	if b.Type != "events" {
		return "", false
	}
	var d blocks.Events
	if json.Unmarshal(b.Data, &d) != nil || d.Source != blocks.EventsICS || d.ICSURL == "" {
		return "", false
	}
	return d.ICSURL, true
}

// eventFeeds loads cached dates for a page's calendar events blocks and
// starts refreshes for stale or missing ones, waiting briefly for missing.
func (s *Server) eventFeeds(ctx context.Context, list []blocks.Block) func(string) []blocks.Event {
	sources := map[string]string{}
	var ids []string
	for _, b := range list {
		if src, ok := icsSource(b); ok {
			sources[b.ID] = src
			ids = append(ids, b.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	cache, err := s.store.EventFeeds(ctx, ids)
	if err != nil {
		s.log.Warn("load calendar cache", "err", err)
		cache = map[string]*store.EventFeed{}
	}
	now := s.now()
	var waits []<-chan struct{}
	for _, id := range ids {
		e := cache[id]
		switch {
		case e == nil || e.Source != sources[id]:
			waits = append(waits, s.refreshEventFeed(id, sources[id]))
		case s.eventFeedStale(e, now):
			s.refreshEventFeed(id, sources[id])
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
		if fresh, err := s.store.EventFeeds(ctx, ids); err == nil {
			cache = fresh
		}
	}
	return func(id string) []blocks.Event {
		e := cache[id]
		if e == nil || e.Source != sources[id] {
			return nil
		}
		var evs []blocks.Event
		_ = json.Unmarshal(e.Events, &evs)
		return evs
	}
}

func (s *Server) eventFeedStale(e *store.EventFeed, now time.Time) bool {
	ttl := s.x.feedTTL
	if e.Error != "" {
		ttl = s.x.feedRetry
	}
	return now.Sub(e.CheckedAt) >= ttl
}

// prepareEvents fetches a new or changed calendar address when the block is
// saved, so a bad address is reported in the editor, and caches the result.
func (s *Server) prepareEvents(w http.ResponseWriter, r *http.Request, existing *store.Block, data json.RawMessage) (func(context.Context, *store.Block) error, bool) {
	src, ok := icsSource(blocks.Block{Type: "events", Data: data})
	if !ok {
		return nil, true
	}
	if existing != nil {
		if old, ok := icsSource(blocks.Block{Type: existing.Type, Data: existing.Data}); ok && old == src {
			return nil, true
		}
	}
	if !s.previewLimit.allow(userFrom(r).ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many calendar checks, try again later", "")
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := s.fetchEventFeed(ctx, src)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "feed_failed", eventFeedErrMessage(err), "ics_url")
		return nil, false
	}
	return func(ctx context.Context, b *store.Block) error {
		s.saveEventFeed(context.WithoutCancel(ctx), b.ID, src, res, nil, nil)
		return nil
	}, true
}

// eventFeedStatus fills in the editor's view of calendar events blocks: how
// many dates the last fetch found and the last error.
func (s *Server) eventFeedStatus(ctx context.Context, list []*store.Block) {
	var ids []string
	for _, b := range list {
		if _, ok := icsSource(blocks.Block{Type: b.Type, Data: b.Data}); ok {
			ids = append(ids, b.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	cache, err := s.store.EventFeeds(ctx, ids)
	if err != nil {
		return
	}
	for _, b := range list {
		if f := cache[b.ID]; f != nil {
			b.Feed = &store.BlockFeed{Found: f.Found, Dropped: f.Dropped, CheckedAt: f.CheckedAt, FetchedAt: f.FetchedAt, Error: f.Error}
		}
	}
}
