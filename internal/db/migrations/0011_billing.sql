-- Billing: each source that can grant Pro keeps its own state here, and the
-- application recomputes users.plan, plan_source and plan_expires_at from all
-- of them (plans.Resolve), so a Stripe event never clobbers a longer manual or
-- Patreon grant.
CREATE TABLE billing (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    -- Admin grant. manual_expires_at NULL with manual_pro means no expiry.
    manual_pro        boolean NOT NULL DEFAULT false,
    manual_expires_at timestamptz,

    -- Stripe subscription, as last reported by a webhook.
    stripe_customer_id          text UNIQUE,
    stripe_subscription_id      text UNIQUE,
    stripe_status               text,
    stripe_price_id             text,
    stripe_current_period_end   timestamptz,
    stripe_cancel_at_period_end boolean NOT NULL DEFAULT false,
    stripe_payment_failed_at    timestamptz,
    -- created time of the newest subscription event applied; older events are skipped.
    stripe_event_at             timestamptz,
    -- Pro granted by Stripe ends here; NULL means Stripe grants nothing.
    stripe_pro_until            timestamptz,

    -- Patreon connection. One Kork account per Patreon user.
    patreon_user_id       text UNIQUE,
    -- AES-GCM sealed with ZELDA_SECRET_KEY, bound to user_id.
    patreon_refresh_token bytea,
    patreon_patron_status text,
    patreon_entitled      boolean NOT NULL DEFAULT false,
    patreon_checked_at    timestamptz,
    patreon_lost_at       timestamptz,
    patreon_pro_until     timestamptz,

    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Stripe events already handled, so retries and duplicates are no-ops.
CREATE TABLE stripe_events (
    id           text PRIMARY KEY,
    type         text NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX stripe_events_processed_at ON stripe_events (processed_at);

-- Existing Pro grants were all made by hand.
INSERT INTO billing (user_id, manual_pro, manual_expires_at)
SELECT id, true, plan_expires_at FROM users
WHERE plan = 'pro' AND (plan_source IS NULL OR plan_source = 'manual');
