# Billing setup

Kork can sell Pro through Stripe subscriptions and unlock it for SUBCULT Patreon members. Either can be used alone. Each stays off until all of its settings are present; with a provider off, its buttons are hidden in the editor and its endpoints return 404.

Owner decisions, October 8, 2026: Pro costs **$5/month or $48/year**, a qualifying SUBCULT **Backer** membership unlocks Pro, scheduled links remain free, and Free accounts keep their **five-page limit**. Stripe uses the **UNDERTOW** account (`acct_1ULZ9hElO9mtgE8Q`) and its test mode. Prices and tier IDs are configured by environment; the editor's labels must match the actual prices.

Keep secrets in the server's `.env` (or the existing secret store), never in this repository. Do everything in Stripe's test mode first, then repeat it in live mode.

## 1. Server key

Patreon tokens are stored encrypted with `ZELDA_SECRET_KEY`. Generate it once and keep a copy with your backups; if it is lost or changed, connected patrons have to reconnect.

```sh
openssl rand -base64 32
```

## 2. Stripe

### Product and prices

1. In the Stripe Dashboard, open **Product catalog** and create a product named **Kork Pro**.
2. Add two **recurring** prices in USD: one billed monthly and one billed yearly.
3. Copy each price ID (`price_...`) into `ZELDA_STRIPE_PRICE_MONTHLY` and `ZELDA_STRIPE_PRICE_ANNUAL`.
4. Set the button labels to match, for example `ZELDA_PRICE_DISPLAY_MONTHLY='$5'` and `ZELDA_PRICE_DISPLAY_ANNUAL='$48'`. They are display text only; if you change a price in Stripe, change the label too.

### API key

Under **Developers → API keys**, create a restricted key or use the secret key, and set `ZELDA_STRIPE_SECRET_KEY`. Kork makes only these calls, so a restricted key needs the matching permissions:

- Checkout Sessions (create)
- Customer portal sessions (create)
- Subscriptions (read, and cancel when an account is deleted)

Checkout creates the customer, so Kork doesn't need customer write access.

### Webhook

1. Under **Developers → Webhooks**, add an endpoint: `https://kork.subcult.tv/stripe/webhook`.
2. Select exactly these events:
   - `checkout.session.completed`
   - `customer.subscription.created`
   - `customer.subscription.updated`
   - `customer.subscription.deleted`
   - `invoice.payment_failed`
3. Copy the endpoint's signing secret (`whsec_...`) into `ZELDA_STRIPE_WEBHOOK_SECRET`.

Use API version `2026-09-30.endive`, matching Kork's pinned API requests. Kork also reads legacy subscription period-end fields for stored events.

The webhook must reach the server directly. If a proxy or tunnel rewrites the request body, the signature check fails and Stripe shows `400` responses in the endpoint's event log.

### Customer portal

Under **Settings → Billing → Customer portal**:

On the shared UNDERTOW account, create a Kork-specific portal configuration and set its `bpc_...` ID as `ZELDA_STRIPE_PORTAL_CONFIGURATION`. This lets Kork select its own policy links and cancellation settings without changing the portal used by another product. Without that variable, Kork uses the account's default configuration.

1. Turn on: update payment methods, view invoice history, cancel subscriptions.
2. Set cancellation to **at the end of the billing period**. Kork keeps Pro until the paid period ends even if a subscription is canceled immediately.
3. Under subscription updates, allow switching between the Kork Pro monthly and yearly prices if you want customers to change plans themselves.
4. Set the business name, privacy policy link (`https://kork.subcult.tv/privacy`) and terms link (`https://kork.subcult.tv/terms`).

### Tax

Kork explicitly disables Managed Payments on each Checkout Session. UNDERTOW currently enables it by default, but Kork uses direct Stripe Billing with SUBCULT as the operator. This setting does not change another product's account defaults.

`ZELDA_STRIPE_AUTOMATIC_TAX=true` turns on Stripe Tax in Checkout, and Checkout then asks for a billing address. Set it only after Stripe Tax is configured: origin address, product tax code (software as a service) and the registrations where Kork collects tax. Whether Kork must register anywhere has not been assessed (see the pricing research). Leave it `false` until that is decided.

### Test it

With test-mode keys:

1. In the editor, open **Settings** and choose a price. Pay with card `4242 4242 4242 4242`.
2. Back in Settings, Pro should appear within a minute. If it doesn't, check the webhook's event log in Stripe and the server log for `stripe webhook rejected` or `stripe event failed`.
3. Open **Manage billing**, cancel, and confirm Settings shows the end date.

## 3. Patreon

Patreon's API v2 docs say that when a client requests memberships without the `identity.memberships` scope, it "will receive data about the user's membership to your campaign." Kork relies on that: it requests only the `identity` scope, which works when the client belongs to the SUBCULT creator account, and it sees nothing about a patron's other pledges.

### Register the client

1. Log in to Patreon **as the SUBCULT creator account**.
2. Open `https://www.patreon.com/portal/registration/register-clients` and create a client:
   - App name: Kork
   - Redirect URIs: `https://kork.subcult.tv/app/patreon/callback` (it must match exactly)
   - API version: 2
3. Copy the **Client ID** into `ZELDA_PATREON_CLIENT_ID` and the **Client Secret** into `ZELDA_PATREON_CLIENT_SECRET`.

### Find the campaign and tier IDs

The client page also shows a **Creator's Access Token**. Use it once from your own machine to look up the IDs; Kork doesn't need it.

```sh
read -rs PATREON_CREATOR_TOKEN   # paste the token; it isn't echoed or saved in history
curl -s -H "Authorization: Bearer $PATREON_CREATOR_TOKEN" \
  'https://www.patreon.com/api/oauth2/v2/campaigns?fields%5Bcampaign%5D=vanity,url'
```

The `id` of the SUBCULT campaign goes into `ZELDA_PATREON_CAMPAIGN_ID`. Then list its tiers:

```sh
curl -s -H "Authorization: Bearer $PATREON_CREATOR_TOKEN" \
  "https://www.patreon.com/api/oauth2/v2/campaigns/CAMPAIGN_ID?include=tiers&fields%5Btier%5D=title,amount_cents,published"
unset PATREON_CREATOR_TOKEN
```

Put the `id` of each tier that should unlock Pro (for now, Backer) in `ZELDA_PATREON_TIER_IDS`, comma-separated. Adding a tier later only needs this setting and a restart.

### What patrons get

A patron connects from **Settings → Connect Patreon**. Pro turns on when the membership's status is `active_patron` and it is entitled to one of the listed tiers. One Patreon account can unlock one Kork account; disconnecting frees it for another.

Kork re-checks connected memberships once a day. If a payment is declined or the patron leaves, Pro continues for 7 days from the check that noticed, then ends. If the patron revokes Kork's access on Patreon, the same grace applies and Settings asks them to reconnect. Patreon webhooks are not used.

### Test it

1. Use a Patreon account that pledges to a listed tier (or temporarily list a free tier ID you belong to).
2. Connect from Settings; Pro should appear immediately.
3. Connecting the same Patreon account from a second Kork account should fail with "already connected to another Kork account."

## 4. Restart and check

After setting the variables, restart the app. The log line `billing enabled` shows which providers are on; startup fails with a message naming the missing variable if a provider is only partly configured.

Admins can still grant Pro by hand in **Admin → Users**. Manual, Stripe and Patreon grants are kept separately and the longest one wins, so revoking a manual grant does not end a paid subscription.
