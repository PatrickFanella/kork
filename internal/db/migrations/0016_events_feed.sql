-- Dates read from an events block's calendar feed (#39). events holds the
-- last good result; a failed refresh only updates checked_at and error.
CREATE TABLE events_feed_cache (
    block_id   uuid PRIMARY KEY REFERENCES blocks(id) ON DELETE CASCADE,
    source     text NOT NULL,
    events     jsonb NOT NULL DEFAULT '[]'::jsonb,
    found      integer NOT NULL DEFAULT 0,
    dropped    integer NOT NULL DEFAULT 0,
    checked_at timestamptz NOT NULL,
    fetched_at timestamptz,
    error      text NOT NULL DEFAULT ''
);
