-- A page's Bluesky (AT Protocol) DID, served at /.well-known/atproto-did on
-- the page's verified custom domains so a domain can be a Bluesky handle (#36).
ALTER TABLE pages ADD COLUMN atproto_did text NOT NULL DEFAULT '';
