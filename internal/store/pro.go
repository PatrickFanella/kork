package store

import (
	"context"
	"encoding/json"
)

// PageCSS returns a page's stored (already sanitized) custom CSS.
func (s *Store) PageCSS(ctx context.Context, pageID string) (string, error) {
	var css string
	err := s.pool.QueryRow(ctx, `SELECT custom_css FROM pages WHERE id = $1`, pageID).Scan(&css)
	return css, notFound(err)
}

// SetPageCSS stores sanitized custom CSS for a page.
func (s *Store) SetPageCSS(ctx context.Context, pageID, css string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE pages SET custom_css = $2, updated_at = now() WHERE id = $1`, pageID, css)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ExportRow is one row of the stats CSV: a count for one day and metric.
type ExportRow struct {
	Day    string
	Metric string // views, clicks, referrer or country
	Key    string // block ID, referrer domain or country code; empty for views
	Count  int64
	// For clicks: the block's type and data, to build its label. Both are
	// empty when the block has been deleted.
	BlockType string
	BlockData json.RawMessage
}

// StatsExport returns every aggregate row for the last days, ending today
// (database date): daily views zero-filled, then clicks per block, referrers
// and countries for days that have them. Rows are ordered by day, then
// metric in that order, then count descending.
func (s *Store) StatsExport(ctx context.Context, pageID string, days int) ([]ExportRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(day, 'YYYY-MM-DD'), metric, key, count, btype, bdata FROM (
			SELECT d::date AS day, 0 AS ord, 'views' AS metric, '' AS key, COALESCE(sd.count, 0) AS count,
				'' AS btype, NULL::jsonb AS bdata
			FROM generate_series(current_date - ($2::int - 1), current_date, interval '1 day') AS d
			LEFT JOIN stats_daily sd ON sd.page_id = $1 AND sd.block_id = $3 AND sd.day = d::date
			UNION ALL
			SELECT sd.day, 1, 'clicks', sd.block_id::text, sd.count, COALESCE(b.type, ''), b.data
			FROM stats_daily sd LEFT JOIN blocks b ON b.id = sd.block_id AND b.page_id = sd.page_id
			WHERE sd.page_id = $1 AND sd.block_id <> $3 AND sd.day > current_date - $2::int
			UNION ALL
			SELECT day, 2, 'referrer', referrer, count, '', NULL FROM stats_referrers
			WHERE page_id = $1 AND day > current_date - $2::int
			UNION ALL
			SELECT day, 3, 'country', country, count, '', NULL FROM stats_countries
			WHERE page_id = $1 AND day > current_date - $2::int
		) r ORDER BY day, ord, count DESC, key`, pageID, days, ZeroBlockID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExportRow
	for rows.Next() {
		var r ExportRow
		if err := rows.Scan(&r.Day, &r.Metric, &r.Key, &r.Count, &r.BlockType, &r.BlockData); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
