-- Cloudflare for SaaS state for custom domains (#13). cf_hostname_id is set
-- when the domain was registered with Cloudflare; the status columns and
-- cf_details (records to show the customer, Cloudflare's error messages)
-- are the last values read from the API.
ALTER TABLE domains
    ADD COLUMN cf_hostname_id text UNIQUE,
    ADD COLUMN cf_status      text NOT NULL DEFAULT '',
    ADD COLUMN cf_ssl_status  text NOT NULL DEFAULT '',
    ADD COLUMN cf_details     jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN cf_checked_at  timestamptz;

-- The background refresh reads unverified Cloudflare domains, oldest check first.
CREATE INDEX domains_cf_pending_idx ON domains (cf_checked_at NULLS FIRST)
    WHERE cf_hostname_id IS NOT NULL AND verified_at IS NULL;
