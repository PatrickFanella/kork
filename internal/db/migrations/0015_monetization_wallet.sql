-- A page's Web Monetization wallet address (#37), rendered as
-- <link rel="monetization"> on the live public page.
ALTER TABLE pages ADD COLUMN monetization_wallet text NOT NULL DEFAULT '';
