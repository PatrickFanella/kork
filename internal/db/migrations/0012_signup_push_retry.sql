-- Retry state for sign-ups waiting to be sent to Brevo (#58, #60).
-- next_push_at spaces out retries so a few failing rows can't hold back
-- newer ones; push_error is a fixed message safe to show the page owner;
-- push_rejected is true when Brevo itself refused the last attempt, the only
-- case in which the address is eventually dropped.
ALTER TABLE signup_contacts
    ADD COLUMN push_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN next_push_at  timestamptz,
    ADD COLUMN push_error    text NOT NULL DEFAULT '',
    ADD COLUMN push_rejected boolean NOT NULL DEFAULT false;

DROP INDEX signup_contacts_push_idx;
CREATE INDEX signup_contacts_push_idx ON signup_contacts (next_push_at NULLS FIRST, confirmed_at) WHERE pending_push;
