# Zelda

Self-serve themed link pages. People sign up, add their socials, payment handles, crypto addresses, QR codes, embeds and contact card, pick a theme, and publish at `/{name}` or on their own domain.

- **Go API + server-rendered pages.** Public pages are plain HTML and CSS with one small script for copy buttons, sharing and click counts. No React or third-party requests on visitor pages, apart from embeds a page owner adds.
- **React editor** (`web/`) at `/app`, built into the Go binary.
- **PostgreSQL** for everything except uploaded images, which live on disk.

## What a page can hold

| Block | Examples |
|---|---|
| Service | 90+ entries: Instagram, TikTok, X, Bluesky, Mastodon, YouTube, Twitch, Spotify, Bandcamp, Patreon, Substack, Etsy, Cash App, PayPal, Venmo, Ko-fi, Buy Me a Coffee, Stripe links, Zelle, Bitcoin, Lightning, Ethereum, Monero, WhatsApp, Telegram, Signal, Discord, Calendly, GitHub, LinkedIn… |
| Link | Any http(s), `mailto:`, `tel:` or `sms:` link, with optional thumbnail |
| Embed | YouTube, Vimeo, Spotify, SoundCloud, Twitch, Apple Music |
| QR code | Any text or URL; any link or service can also show its own QR |
| Copy button | Usernames, Zelle email, wallet addresses |
| Contact card | "Save contact" downloads a vCard |
| Location | Opens an address in maps |
| Heading, text, image, divider | Layout and content |

Handles are checked and turned into URLs per service (`$cashtag` → `https://cash.app/$cashtag`, `@you@instance` → the Mastodon profile). Pasted profile URLs are accepted only on that service's domains. Copy-only payment services (Zelle, crypto) render as copy buttons, and crypto addresses get a scannable `bitcoin:`/`monero:`/… QR code.

Themes: 10 presets (including **Signal**, modeled on socials.subcult.tv) plus overrides for 7 colors, font, button style and shape, layout (stacked buttons or numbered ledger), and solid/gradient/image backgrounds.

Brand icons come from [Simple Icons](https://simpleicons.org) (CC0). Brands that Simple Icons doesn't carry, such as LinkedIn, fall back to a category icon. Regenerate with `make icons`.

## Running locally

Requires Go 1.26+, Node 22+, and Docker for Postgres.

```bash
make web    # build the editor once (or after editor changes)
make run    # starts Postgres on :5433 and the server on :8080
```

Open http://localhost:8080/app/signup. Without SMTP, verification and reset links are printed in the server log. The first account becomes the admin.

For editor work, run `cd web && npm run dev` alongside `make run`; Vite proxies API and preview calls to :8080.

`scripts/seed-demo.sh http://localhost:8080 server.log` creates a verified demo account with a published page (set `PRESET=pastel` etc. to change theme).

## Tests

```bash
make test       # Go unit tests, the Postgres end-to-end test, editor typecheck and Vitest
```

The end-to-end test (`internal/server/server_test.go`) drops and recreates the schema of the database in `ZELDA_TEST_DATABASE_URL`. `make test-go` points it at a separate `zelda_test` database.

## Deploying

`docker compose up -d --build` runs Postgres, the app, and Caddy. Copy `.env.example` to `.env` first.

1. Point `ZELDA_HOST` (for example `links.subcult.tv`) at the Caddy host.
2. Custom domains: the user adds a hostname in the editor, creates the TXT record `_zelda.<host>` and a CNAME to `ZELDA_DOMAIN_TARGET`, then presses Verify. The app checks both records. Caddy issues a certificate on the first request after asking `/internal/tls-ask`, which approves only verified domains. Caddy blocks `/internal/*` from outside, and the app answers it only for private-network callers.
3. Configure SMTP before opening signups. Unverified accounts can build pages but can't publish them.

Ports 80 and 443 must reach Caddy for ACME. If the service sits behind an existing edge proxy instead, drop the `caddy` service, forward the base host and custom domains to `zelda:8080`, keep `/internal/*` private, and reuse the same `ask` URL for on-demand TLS.

### Abuse controls

- Email verification before publishing.
- Per-IP and per-account rate limits on signup, login, mail, uploads, reports and click beacons (in-process, so run one replica).
- Reserved names (`admin`, `api`, `support`…), URL scheme allowlist, image-only uploads checked by content (no SVG), strict CSP on public pages.
- Visitors can report a page; admins see reports in the editor and can suspend pages or accounts.

## Layout

```
cmd/zelda          entry point
internal/catalog   services, block types, themes
internal/blocks    block validation and render items, embeds, vCard
internal/render    public page templates and static CSS/JS
internal/server    HTTP handlers, auth, rate limits
internal/store     Postgres queries
internal/db        pool and embedded migrations
web/               React editor (Vite)
docs/api.md        API contract between editor and server
deploy/Caddyfile   TLS and on-demand certificates
```
