-- Publish gate: accounts are trusted once an admin approves them. Accounts that
-- exist before this migration are trusted so the gate only affects new signups.
ALTER TABLE users ADD COLUMN trusted_at timestamptz;
UPDATE users SET trusted_at = created_at;

-- Destination screening verdicts, keyed by the exact URL that was checked.
CREATE TABLE screen_verdicts (
    url        text PRIMARY KEY,
    unsafe     boolean NOT NULL,
    threat     text NOT NULL DEFAULT '',
    source     text NOT NULL DEFAULT '',
    checked_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX screen_verdicts_expires_idx ON screen_verdicts (expires_at);

-- When each published page was last re-screened.
CREATE TABLE page_screenings (
    page_id    uuid PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
    checked_at timestamptz NOT NULL
);
