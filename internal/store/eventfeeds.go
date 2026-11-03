package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// EventFeed is the cached result of an events block's calendar feed (#39).
type EventFeed struct {
	BlockID string
	Source  string
	// Events is JSON owned by the blocks package ([]blocks.Event).
	Events    json.RawMessage
	Found     int // upcoming events kept from the last good fetch
	Dropped   int // events in that fetch that couldn't be used
	CheckedAt time.Time
	FetchedAt *time.Time
	Error     string
}

const eventFeedCols = `block_id, source, events, found, dropped, checked_at, fetched_at, error`

// EventFeeds returns the cache rows for the given blocks, keyed by block ID.
func (s *Store) EventFeeds(ctx context.Context, blockIDs []string) (map[string]*EventFeed, error) {
	out := map[string]*EventFeed{}
	if len(blockIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+eventFeedCols+` FROM events_feed_cache WHERE block_id = ANY($1::uuid[])`, blockIDs)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*EventFeed, error) {
		var f EventFeed
		err := row.Scan(&f.BlockID, &f.Source, &f.Events, &f.Found, &f.Dropped, &f.CheckedAt, &f.FetchedAt, &f.Error)
		return &f, err
	})
	if err != nil {
		return nil, err
	}
	for _, f := range list {
		out[f.BlockID] = f
	}
	return out, nil
}

// SaveEventFeed writes a block's cache row unless the block is gone.
func (s *Store) SaveEventFeed(ctx context.Context, f *EventFeed) error {
	events := f.Events
	if len(events) == 0 {
		events = json.RawMessage("[]")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO events_feed_cache (`+eventFeedCols+`)
		SELECT $1, $2, $3::jsonb, $4, $5, $6, $7, $8 WHERE EXISTS (SELECT 1 FROM blocks WHERE id = $1)
		ON CONFLICT (block_id) DO UPDATE SET source = excluded.source, events = excluded.events, found = excluded.found,
			dropped = excluded.dropped, checked_at = excluded.checked_at, fetched_at = excluded.fetched_at, error = excluded.error`,
		f.BlockID, f.Source, string(events), f.Found, f.Dropped, f.CheckedAt, f.FetchedAt, f.Error)
	return err
}
