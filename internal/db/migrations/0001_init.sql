CREATE TABLE users (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email             text NOT NULL,
    password_hash     text NOT NULL,
    email_verified_at timestamptz,
    is_admin          boolean NOT NULL DEFAULT false,
    suspended_at      timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

CREATE TABLE user_tokens (
    token_hash bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    text NOT NULL CHECK (purpose IN ('verify', 'reset')),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

CREATE TABLE uploads (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content_type text NOT NULL,
    size_bytes   integer NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE pages (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slug             text NOT NULL UNIQUE,
    title            text NOT NULL DEFAULT '',
    bio              text NOT NULL DEFAULT '',
    avatar_upload_id uuid REFERENCES uploads(id) ON DELETE SET NULL,
    theme            jsonb NOT NULL DEFAULT '{}'::jsonb,
    published        boolean NOT NULL DEFAULT false,
    suspended_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pages_user_idx ON pages (user_id);

CREATE TABLE blocks (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id    uuid NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    position   integer NOT NULL,
    type       text NOT NULL,
    data       jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled    boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX blocks_page_idx ON blocks (page_id, position);

CREATE TABLE domains (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id      uuid NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    hostname     text NOT NULL UNIQUE,
    verify_token text NOT NULL,
    verified_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX domains_page_idx ON domains (page_id);

CREATE TABLE reports (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id     uuid NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    reason      text NOT NULL,
    details     text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

-- block_id is the zero UUID for page views.
CREATE TABLE stats_daily (
    page_id  uuid NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    block_id uuid NOT NULL,
    day      date NOT NULL,
    count    bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (page_id, block_id, day)
);
