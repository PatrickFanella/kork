-- Email sign-up, events and latest video/post blocks (#11).

-- Addresses submitted to an email_signup block. A row is pending until the
-- visitor opens the confirmation link (token_hash set, confirmed_at NULL).
-- For blocks that keep the list in Kork, confirmed rows are the list. For
-- Brevo blocks a confirmed row is deleted once Brevo accepts it;
-- pending_push marks one still waiting for a retry.
CREATE TABLE signup_contacts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    block_id     uuid NOT NULL REFERENCES blocks(id) ON DELETE CASCADE,
    email        text NOT NULL,
    token_hash   bytea UNIQUE,
    expires_at   timestamptz,
    confirmed_at timestamptz,
    pending_push boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX signup_contacts_block_email_key ON signup_contacts (block_id, lower(email));
CREATE INDEX signup_contacts_expiry_idx ON signup_contacts (expires_at) WHERE confirmed_at IS NULL;
CREATE INDEX signup_contacts_push_idx ON signup_contacts (confirmed_at) WHERE pending_push;

-- The creator's Brevo API key for a sign-up block, sealed with
-- ZELDA_SECRET_KEY (AES-GCM). Never returned by the API.
CREATE TABLE signup_secrets (
    block_id   uuid PRIMARY KEY REFERENCES blocks(id) ON DELETE CASCADE,
    brevo_key  bytea NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Newest item of a latest block's feed. source is the block source the row
-- was fetched for; a different source means the row is stale.
CREATE TABLE feed_cache (
    block_id            uuid PRIMARY KEY REFERENCES blocks(id) ON DELETE CASCADE,
    source              text NOT NULL,
    item_key            text NOT NULL DEFAULT '',
    title               text NOT NULL DEFAULT '',
    link                text NOT NULL DEFAULT '',
    published_at        timestamptz,
    thumbnail_url       text NOT NULL DEFAULT '',
    thumbnail_upload_id uuid REFERENCES uploads(id) ON DELETE SET NULL,
    checked_at          timestamptz NOT NULL,
    fetched_at          timestamptz,
    error               text NOT NULL DEFAULT ''
);

-- YouTube @handle to channel ID, read from the channel page.
CREATE TABLE youtube_channels (
    handle     text PRIMARY KEY,
    channel_id text NOT NULL,
    fetched_at timestamptz NOT NULL
);
