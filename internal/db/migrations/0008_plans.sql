-- Account plans. Pro can expire; an expired Pro grant acts as Free. Billing
-- will set plan_source to patreon or stripe later; provider customer and
-- subscription IDs belong in their own table, not here.
ALTER TABLE users
    ADD COLUMN plan            text NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'pro')),
    ADD COLUMN plan_source     text CHECK (plan_source IN ('manual', 'patreon', 'stripe')),
    ADD COLUMN plan_expires_at timestamptz,
    ADD COLUMN plan_updated_at timestamptz;

-- Domains attached before plans existed keep working on any plan. Newer
-- domains are served only while the owner has Pro, plus a grace period.
ALTER TABLE domains ADD COLUMN grandfathered boolean NOT NULL DEFAULT false;
UPDATE domains SET grandfathered = true;
