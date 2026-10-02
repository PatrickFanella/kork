package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Feed cache for latest video/post blocks (#11).

// FeedEntry is the cached newest item of a latest block's feed.
type FeedEntry struct {
	BlockID           string
	Source            string
	ItemKey           string
	Title             string
	Link              string
	PublishedAt       *time.Time
	ThumbnailURL      string // remote address the thumbnail was copied from
	ThumbnailUploadID *string
	CheckedAt         time.Time  // last attempt
	FetchedAt         *time.Time // last success
	Error             string     // why the last attempt failed, or ""
}

const feedCols = `block_id, source, item_key, title, link, published_at, thumbnail_url, thumbnail_upload_id, checked_at, fetched_at, error`

func scanFeed(row pgx.CollectableRow) (*FeedEntry, error) {
	var f FeedEntry
	err := row.Scan(&f.BlockID, &f.Source, &f.ItemKey, &f.Title, &f.Link, &f.PublishedAt, &f.ThumbnailURL,
		&f.ThumbnailUploadID, &f.CheckedAt, &f.FetchedAt, &f.Error)
	return &f, err
}

// FeedEntries returns the cache rows for the given blocks, keyed by block ID.
func (s *Store) FeedEntries(ctx context.Context, blockIDs []string) (map[string]*FeedEntry, error) {
	out := map[string]*FeedEntry{}
	if len(blockIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+feedCols+` FROM feed_cache WHERE block_id = ANY($1::uuid[])`, blockIDs)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, scanFeed)
	if err != nil {
		return nil, err
	}
	for _, f := range list {
		out[f.BlockID] = f
	}
	return out, nil
}

// SaveFeedEntry writes a block's cache row. It returns false when the block
// no longer exists.
func (s *Store) SaveFeedEntry(ctx context.Context, f *FeedEntry) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO feed_cache (`+feedCols+`)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11 WHERE EXISTS (SELECT 1 FROM blocks WHERE id = $1)
		ON CONFLICT (block_id) DO UPDATE SET source = excluded.source, item_key = excluded.item_key,
			title = excluded.title, link = excluded.link, published_at = excluded.published_at,
			thumbnail_url = excluded.thumbnail_url, thumbnail_upload_id = excluded.thumbnail_upload_id,
			checked_at = excluded.checked_at, fetched_at = excluded.fetched_at, error = excluded.error`,
		f.BlockID, f.Source, f.ItemKey, f.Title, f.Link, f.PublishedAt, f.ThumbnailURL, f.ThumbnailUploadID,
		f.CheckedAt, f.FetchedAt, f.Error)
	return err == nil && tag.RowsAffected() > 0, err
}

// FeedThumbnail returns the upload a block's cache row owns, if any.
func (s *Store) FeedThumbnail(ctx context.Context, blockID string) (string, bool) {
	var id *string
	_ = s.pool.QueryRow(ctx, `SELECT thumbnail_upload_id FROM feed_cache WHERE block_id = $1`, blockID).Scan(&id)
	if id == nil {
		return "", false
	}
	return *id, true
}

// YouTubeChannel returns a cached @handle to channel ID mapping.
func (s *Store) YouTubeChannel(ctx context.Context, handle string) (string, time.Time, error) {
	var id string
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT channel_id, fetched_at FROM youtube_channels WHERE handle = lower($1)`, handle).Scan(&id, &at)
	return id, at, notFound(err)
}

// SaveYouTubeChannel caches a @handle to channel ID mapping.
func (s *Store) SaveYouTubeChannel(ctx context.Context, handle, channelID string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO youtube_channels (handle, channel_id, fetched_at) VALUES (lower($1), $2, $3)
		ON CONFLICT (handle) DO UPDATE SET channel_id = excluded.channel_id, fetched_at = excluded.fetched_at`,
		handle, channelID, at)
	return err
}
