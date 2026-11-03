-- Consent receipt for each sign-up (#35): the exact sender, purpose, list
-- and disclosure the visitor saw when they asked to subscribe. Delivery only
-- sends an address to the list in its receipt. '{}' marks rows from before
-- receipts, which follow the block's current destination as before.
ALTER TABLE signup_contacts ADD COLUMN consent jsonb NOT NULL DEFAULT '{}'::jsonb;
