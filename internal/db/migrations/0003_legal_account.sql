-- Terms acceptance is recorded at signup. Accounts created before this
-- migration keep NULL and are not asked to accept again.
ALTER TABLE users
    ADD COLUMN terms_version     text,
    ADD COLUMN terms_accepted_at timestamptz;

-- A requested email change. users.email only switches once the new address
-- follows the emailed link.
CREATE TABLE email_changes (
    token_hash bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    new_email  text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);
CREATE INDEX email_changes_user_idx ON email_changes (user_id);
