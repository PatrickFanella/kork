# Zelda API contract

The Go service serves everything from one origin:

| Path | Purpose |
|---|---|
| `/api/*` | JSON API used by the editor (cookie session) |
| `/app/*` | React editor (built from `web/`, embedded in the Go binary) |
| `/preview/{pageId}` | Server-rendered draft preview, owner only; the editor shows it in an iframe |
| `/{slug}` | Published public page |
| `POST /t/{blockId}` | Click beacon sent by the public page script (links stay direct so `rel="me"` verification works) |
| `/vcard/{blockId}` | vCard download for a contact block |
| `/unsubscribe/weekly-stats?token=...` | Signed one-click unsubscribe from the weekly stats email (GET or POST) |
| `/u/{uploadId}` | Uploaded image |
| `/static/*` | Public page CSS/JS |
| `/terms`, `/privacy` | Terms of service and privacy policy (server-rendered, main host only) |

On a verified custom domain, `/` renders the attached page.

## Conventions

- Requests and responses are JSON (`Content-Type: application/json`) except uploads (multipart) and QR images.
- Mutating requests must send `X-Requested-With: zelda` (CSRF guard). The editor's fetch wrapper adds it to every request.
- Session is an HttpOnly cookie named `zelda_session`; use `credentials: "same-origin"`.
- Errors: HTTP 4xx/5xx with body `{"error": {"code": "string", "message": "human text", "field": "optional field name"}}`.
- IDs are UUID strings. Timestamps are RFC 3339 strings.

## Auth

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/auth/signup` | `{email, password, accept_terms: true}` | `201 {user}` and session cookie. Password ≥ 10 chars. Without `accept_terms: true`: `422`, code `terms_required`, field `accept_terms`. The server records the current terms version (`render.TermsVersion`) and time. |
| POST | `/api/auth/login` | `{email, password}` | `200 {user}` |
| POST | `/api/auth/logout` | – | `204` |
| GET | `/api/auth/me` | – | `200 {user, plan: Plan}` or `401` (see [Plans](#plans)) |
| POST | `/api/auth/verify` | `{token}` | `200 {user}` |
| POST | `/api/auth/resend-verification` | – | `204` |
| POST | `/api/auth/forgot` | `{email}` | `204` (always) |
| POST | `/api/auth/reset` | `{token, password}` | `204` |

`User`: `{id, email, email_verified: bool, is_admin: bool, created_at}`

Emails link to `/app/verify?token=...` and `/app/reset?token=...`; the editor reads the query param and calls the endpoint.

Publishing a page requires a verified email (`403 {"error":{"code":"email_unverified"}}`).

New accounts also can't publish until they are `ZELDA_PUBLISH_MIN_ACCOUNT_AGE` old (default 24 hours; `0` turns the gate off) or an admin approves them. Until then, publishing returns `403 {"error":{"code":"account_too_new","field":"published","message":"New accounts can publish from <date> UTC. ..."}}`. Admins and accounts created before the gate existed are not affected.

## Account

All endpoints need a session except `email/confirm`.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/account` | – | `200 Account` |
| POST | `/api/account/password` | `{current_password, new_password}` | `204`. Signs out every other session. Wrong current password: `422` code `wrong_password`, field `current_password`. |
| POST | `/api/account/email` | `{email, password}` | `202 Account` with `pending_email` set. Emails a link to the new address and a notice to the old one. `409 email_taken` if another account uses it. |
| POST | `/api/account/email/confirm` | `{token}` | `200 {user}`. Switches the email and marks it verified. `400 invalid_token`; `409 email_taken` if the address was taken meanwhile. |
| GET | `/api/account/export` | – | `200 application/zip` download (see below). 5 per hour per account, then `429`. |
| DELETE | `/api/account` | `{password}` | `204` and the session cookie is cleared. |

`Account`: `{user, pending_email: string|null, terms_version: string|null, terms_accepted_at: string|null}`. Accounts created before terms existed have `null` terms fields and are not asked to accept.

Password checks on these endpoints are limited to 10 attempts per 15 minutes per account (`429`).

The email-change link is `/app/confirm-email?token=...` and expires after 24 hours. Requesting another change replaces the pending one. Until confirmation the account keeps its old address.

The export ZIP holds `account.json` (`exported_at, email, email_verified, created_at, terms_version, terms_accepted_at, pages: [Page fields + blocks, domains: [{hostname, verified, created_at}], stats: [{block_id, day, count}]], uploads: [{id, content_type, size, file}]`) and each uploaded image at its `file` path (`uploads/<id>.<ext>`). Stats rows with the zero block ID are page views.

Deleting an account removes the user, sessions, tokens, pending email changes, uploads rows and files, pages, blocks, domains, reports about those pages and stats. Its public pages return 404.

## Catalog

`GET /api/catalog` (public) drives every editor form:

```json
{
  "services": [
    {"key": "instagram", "name": "Instagram", "category": "social", "input": "handle",
     "placeholder": "username", "prefix": "@", "copy_only": false}
  ],
  "categories": [{"key": "social", "name": "Social"}],
  "block_types": [
    {"key": "service", "name": "Social / payment / contact", "description": "..."}
  ],
  "themes": [
    {"key": "signal", "name": "Signal", "colors": {"bg": "#08060c", "surface": "#17101e", "text": "#f5eff8",
      "muted": "#b7a8c0", "accent": "#c05cff", "button_bg": "#17101e", "button_text": "#f5eff8"},
     "font": "mono", "button_style": "ledger", "button_shape": "square", "layout": "ledger"}
  ],
  "fonts": [{"key": "system", "name": "System sans"}],
  "button_styles": [{"key": "filled", "name": "Filled"}],
  "button_shapes": [{"key": "rounded", "name": "Rounded"}],
  "layouts": [{"key": "stack", "name": "Stacked buttons"}],
  "embed_providers": ["YouTube", "Vimeo", "Spotify", "SoundCloud", "Twitch", "Apple Music"]
}
```

`input` is one of `handle | url | text | email | phone`. `copy_only: true` services (Zelle, crypto addresses, Discord usernames) render as a copy button instead of a link.

Categories: `social, video, music, creator, payment, crypto, contact, web`.

## Pages

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/pages` | – | `200 [PageSummary]` |
| POST | `/api/pages` | `{slug, title}` | `201 Page` |
| GET | `/api/pages/{id}` | – | `200 Page` |
| PATCH | `/api/pages/{id}` | any of `{slug, title, bio, avatar_upload_id (string or null), theme, published}` | `200 Page` |
| DELETE | `/api/pages/{id}` | – | `204` |
| GET | `/api/slugs/{slug}` | – | `200 {available: bool, reason?: string}` |
| GET | `/api/pages/{id}/qr.svg` | – | SVG QR code of the public URL |
| GET | `/api/pages/{id}/stats?days=30` | – | `200 Stats`; `days` defaults to 30 and is capped at the plan's `stats_days` (Free 30, Pro 365) and at `ZELDA_STATS_RETENTION_DAYS` |

Slugs: 3–32 chars, `[a-z0-9][a-z0-9-_]*`, some reserved. The page limit depends on the plan. A Free account at its limit gets `403 upgrade_required` with `feature: "pages"`; a Pro account at its limit gets `422 limit`.

`PageSummary`: `{id, slug, title, published, url, updated_at}`

`Page`:

```json
{
  "id": "uuid", "slug": "alice", "title": "Alice", "bio": "text",
  "avatar_url": "/u/uuid or null", "avatar_upload_id": "uuid or null",
  "published": false, "suspended": false,
  "url": "https://links.example.com/alice",
  "theme": Theme,
  "blocks": [Block],
  "domains": [Domain],
  "created_at": "...", "updated_at": "..."
}
```

`Theme` (every field optional on PATCH; server fills defaults from the preset):

```json
{
  "preset": "signal",
  "colors": {"bg": "#rrggbb", "surface": "#rrggbb", "text": "#rrggbb", "muted": "#rrggbb",
             "accent": "#rrggbb", "button_bg": "#rrggbb", "button_text": "#rrggbb"},
  "font": "system | serif | mono | rounded | condensed",
  "button_style": "filled | outline | soft | shadow | ledger",
  "button_shape": "square | rounded | pill",
  "layout": "stack | ledger",
  "background": {"kind": "solid | gradient | image", "color2": "#rrggbb", "image_upload_id": "uuid"},
  "hide_branding": false
}
```

`hide_branding: true` removes the "Made with Kork" footer link. Turning it on needs Pro (`403 upgrade_required`, `feature: "remove_branding"`, `field: "hide_branding"`). Pages show the link whenever the owner's plan lacks the feature. The stored flag is kept, so other theme changes still save after Pro ends.

Changing `preset` on PATCH without `colors` resets the colors to that preset.

`Stats`:

```json
{
  "days": 30,
  "max_days": 30,
  "total_views": 12,
  "views": [{"day": "2026-10-01", "count": 3}],
  "blocks": [{"block_id": "uuid", "label": "Instagram", "count": 4, "ctr": 0.3333}],
  "referrers": [{"name": "instagram.com", "count": 7}, {"name": "direct", "count": 5}],
  "countries": [{"name": "US", "count": 9}, {"name": "unknown", "count": 3}]
}
```

- `total_views` and every list cover the last `days` days, ending today (server date). `max_days` is the longest range this account can request.
- `ctr` is the block's clicks divided by `total_views`, rounded to 4 decimals, and 0 with no views. Views served from a cache are not counted, so it can exceed 1.
- `referrers`: top 20 by views. The name is the `Referer` host, lowercased, without `www.` or port. `direct` covers no referrer, non-web referrers and links from the same host. After 200 distinct referrers for a page in one day, the rest count as `other`. Referrers apply to page views only.
- `countries`: top 20 ISO 3166 alpha-2 codes from Cloudflare's `CF-IPCountry` header; missing, `XX` and `T1` (Tor) count as `unknown`.
- Only daily totals are stored: no IP addresses, cookies or user agents. Rows older than `ZELDA_STATS_RETENTION_DAYS` (default 400) are deleted hourly.

## Account settings

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/account/settings` | – | `200 AccountSettings` |
| PATCH | `/api/account/settings` | any of `AccountSettings` | `200 AccountSettings` |

`AccountSettings`: `{"weekly_stats_email": false}`

Turning `weekly_stats_email` on needs a verified email (`403 {"error":{"code":"email_unverified","field":"weekly_stats_email"}}`). When it is on, a background job emails a summary of the previous Monday–Sunday (UTC) every Monday, at most once per week: views, clicks and click-through per page, plus the top referrers and countries across all the user's pages. The first email is sent in the week after opting in.

Each email links to `GET /unsubscribe/weekly-stats?token=...`, which turns the email off and shows a confirmation page. The token is an HMAC of the user ID with a key stored in the database, and it does not expire. The email also carries `List-Unsubscribe` and `List-Unsubscribe-Post` headers; mail clients' one-click `POST` to the same URL returns `204`. Invalid tokens get `400`.

## Blocks

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/pages/{id}/blocks` | `{type, data, enabled?, starts_at?, ends_at?}` | `201 Block` (appended) |
| PATCH | `/api/pages/{id}/blocks/{blockId}` | `{data?, enabled?, starts_at?, ends_at?}` | `200 Block` |
| DELETE | `/api/pages/{id}/blocks/{blockId}` | – | `204` |
| PUT | `/api/pages/{id}/blocks/order` | `{ids: [uuid, ...]}` (all block IDs) | `204` |

`Block`: `{id, type, data, enabled, position, label, starts_at, ends_at}` where `label` is the server's display name for lists and `starts_at`/`ends_at` are RFC 3339 UTC strings or `null`.

Limit 100 blocks per page. Validation errors return `422` with `field` set to the data key.

When link screening is configured, web destinations are checked on create and update: `link`, `image` and `contact` `url`, `embed` `url`, the profile URL of a `service` block, and a `qr` `value` that is an http(s) URL. A listed destination returns `422 {"error":{"code":"blocked_destination","field":"url" or "value","message":"..."}}`. Publishing a page with a listed destination returns the same code with `field: "published"`. Only the destination URL is sent to the screening provider.

### Schedules

`starts_at` and `ends_at` set an optional visibility window. Setting a bound needs the `scheduled_links` feature, which every plan has for now; without it, create and update return `403 upgrade_required` with `field` naming the bound. Clearing a bound with `null` always works. Send RFC 3339 times with a zone (`2026-10-02T18:00:00Z` or `2026-10-02T13:00:00-05:00`); the server stores and returns UTC. A block is visible from `starts_at` (inclusive) until `ends_at` (exclusive). A `null` or missing bound is open. On PATCH, an omitted field keeps its value and `null` clears it.

- `ends_at` must be after `starts_at`; otherwise `422` with `field: "ends_at"`. An unparseable time returns `422` with that field.
- Public pages, click beacons (`/t/{blockId}`) and vCards (`/vcard/{blockId}`) treat a block outside its window like a disabled block. Visibility is computed per request, so no redeploy or edit is needed when a window opens or closes.
- The owner preview (`/preview/{pageId}`) shows enabled blocks outside their window with a "Scheduled: shows from …" or "Expired …" badge.
- Public pages send `Cache-Control: public, max-age=60`. When an enabled block starts or ends within the next 60 seconds, `max-age` drops to the whole seconds remaining before that boundary, so cached copies expire by the time the page changes.

### Block types and `data`

| type | data | Notes |
|---|---|---|
| `link` | `{url, title, subtitle?, thumbnail_upload_id?, show_qr?}` | http/https/mailto/tel/sms only |
| `service` | `{service, value, label?, show_qr?}` | `service` is a catalog key; `value` is handle/URL/address per its `input` |
| `heading` | `{text}` | |
| `text` | `{text}` | Plain text, newlines kept, URLs auto-linked |
| `divider` | `{}` | |
| `image` | `{upload_id, alt, url?}` | Optional click-through URL |
| `embed` | `{url}` | Must match a supported embed provider |
| `qr` | `{value, label?}` | Renders a QR code inline |
| `copy` | `{label, value}` | Copy-to-clipboard button |
| `contact` | `{name, phone?, email?, org?, url?, note?}` | "Save contact" button downloading a vCard |
| `map` | `{address, label?}` | Opens the address in a maps app |

## Domains

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/pages/{id}/domains` | `{hostname}` | `201 Domain` |
| POST | `/api/pages/{id}/domains/{domainId}/verify` | – | `200 Domain` or `422` with the reason |
| DELETE | `/api/pages/{id}/domains/{domainId}` | – | `204` |

Adding a domain needs Pro. Free accounts get `403 upgrade_required` with `feature: "custom_domains"` and `field: "hostname"`. Pro includes 1 domain per account; adding another returns `422 limit`. Verifying also needs Pro, except for domains attached before plans existed. Removing a domain always works.

`Domain`:

```json
{
  "id": "uuid", "hostname": "links.alice.com", "verified": false,
  "txt_name": "_zelda.links.alice.com", "txt_value": "zelda-verify=abc123",
  "target": "edge.example.com", "created_at": "...", "active": true
}
```

`active` is false when the owner's plan no longer covers the domain. The domain stays attached, but the page isn't served on it and `/internal/tls-ask` refuses it (see [Plans](#plans)).

Setup instructions shown to the user: add the TXT record, point the hostname at `target` with a CNAME (or A/AAAA for apex domains), then press Verify. TLS is issued automatically on first request after verification.

## Uploads

`POST /api/uploads` multipart field `file`; PNG, JPEG, WebP or GIF, max 5 MB. Response `201 {id, url, content_type, size}`.

## Import and link previews

These endpoints make the server fetch a URL the user supplies. All fetches go through `internal/fetch`:

- Only `http` and `https` on ports 80 and 443; URLs with credentials are refused.
- Every connection is checked at dial time against the resolved IP. Loopback, private (RFC 1918, IPv6 ULA `fc00::/7`), link-local (including `169.254.169.254` metadata), CGNAT `100.64.0.0/10`, multicast, unspecified, documentation and NAT64/6to4 addresses are refused, so DNS rebinding and redirects to internal hosts fail the same way.
- At most 5 redirects, each re-checked. 8-second timeout. Pages are capped at 2 MB, images at 5 MB.
- Requests identify themselves as `KorkBot/1.0 (+https://kork.subcult.tv)`.

A URL without a scheme gets `https://`. Fetch failures return `422` with `field: "url"` and one of these codes: `invalid` (scheme, port or credentials), `blocked_address`, `too_large`, `too_many_redirects`, `fetch_failed` (unreachable, timeout or non-2xx), `not_html`, `unsupported` (image type).

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/import/preview` | `{url}` | `200 ImportPreview`. Nothing is saved. Limit 20 per user per hour. |
| POST | `/api/import/image` | `{url}` | `201 {id, url, content_type, size}`: the image copied into an upload owned by the user. Counts against the upload limit (60 per hour). |
| POST | `/api/link-preview` | `{url}` | `200 LinkPreview`. Limit 60 per user per hour; a stored image also counts against the upload limit. |

`ImportPreview`:

```json
{
  "source_url": "https://linktr.ee/alice",
  "parser": "linktree | generic",
  "title": "Alice", "bio": "Illustrator.",
  "avatar_url": "https://remote.example/avatar.png",
  "items": [
    {"type": "service", "data": {"service": "instagram", "value": "alice"}, "label": "Instagram"},
    {"type": "heading", "data": {"text": "Support"}, "label": "Support"},
    {"type": "link", "data": {"url": "https://example.org/x", "title": "My thing"}, "label": "My thing"}
  ]
}
```

Each item's `data` has already passed block validation, so the editor creates it unchanged with `POST /api/pages/{id}/blocks`. Linktree pages are read from their embedded `__NEXT_DATA__` JSON (anchors if it is missing); any other page contributes its title, description, `og:image` and links to other sites. Links to known services become service blocks: profile URLs on handle services (`instagram.com/alice` → `alice`) and any URL on URL-input services (Spotify, Apple Music). `mailto:` and `tel:` become email and phone blocks. Everything else becomes a link block. At most 100 items. `avatar_url` is a remote address: copy it with `/api/import/image`, then set `avatar_upload_id`. The editor never loads it directly.

`LinkPreview`:

```json
{
  "url": "https://example.org/post",
  "title": "Open Graph or <title> text, ≤ 80 chars",
  "description": "og:description or meta description, ≤ 120 chars",
  "thumbnail_upload_id": "uuid (optional)",
  "thumbnail_url": "/u/uuid (optional)",
  "image_error": "why the image was skipped (optional)"
}
```

The page image (`og:image`, then `twitter:image`) is downloaded and stored as the user's upload after the same byte-sniffed type check as `/api/uploads` (PNG, JPEG, GIF, WebP; never SVG). Pages never hotlink it. The editor offers the title and description as suggestions and applies the thumbnail only when the link has none.

## Reports and admin

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/report` | `{slug, reason, details?}` | `204` (public) |
| GET | `/api/admin/reports` | – | `200 [{id, page_id, slug, reason, details, created_at, resolved}]` |
| POST | `/api/admin/reports/{id}/resolve` | – | `204` |
| POST | `/api/admin/pages/{id}/suspend` | `{suspended: bool}` | `204` |
| POST | `/api/admin/users/{id}/suspend` | `{suspended: bool}` | `204` |
| GET | `/api/admin/users/pending` | – | `200 [{id, email, email_verified, created_at, publish_unlocks_at, pages: [slug]}]`: accounts still inside the publish gate |
| POST | `/api/admin/users/{id}/trust` | – | `204`; the account can publish now |
| GET | `/api/admin/users?q=&limit=50` | – | `200 [AdminUser]`: accounts whose email contains `q` (case-insensitive), Pro first, then newest. `limit` is 1–200. |
| POST | `/api/admin/users/{id}/plan` | `{plan: "free" or "pro", expires_at?: RFC 3339 or null}` | `200 Plan` for that user. Grants or revokes Pro by hand (`source: "manual"`). `expires_at` must be in the future; omitted or `null` means no expiry. `free` clears the source and expiry. |

`AdminUser`: `{id, email, email_verified, suspended, created_at, plan, plan_source, plan_expires_at, effective_plan, domains_grace_until, pages}`. `plan` is the stored plan; `effective_plan` is `free` once a Pro grant has expired.

`reason`: `spam | scam | phishing | impersonation | illegal | other`.

Every 6 hours the server re-screens published pages (200 per pass, least recently checked first). If a page links to a listed destination, it is unpublished, a `phishing` report naming the URL is added to `/api/admin/reports`, and the owner gets an email. The owner can republish after removing the link.

## Plans

Every account is `free` or `pro`. Admins grant Pro by hand for now; billing will set `source` to `patreon` or `stripe` later. A Pro grant can have an expiry, after which the account acts as Free. All limits are in one table in `internal/plans/plans.go`. The values are placeholders until the owner sets pricing.

| | Free | Pro |
|---|---|---|
| Pages | 5 (`ZELDA_FREE_PAGE_LIMIT`) | 25 |
| Stats history | 30 days | 365 days |
| Custom domains | 0 | 1 |
| `remove_branding`: hide the footer credit | no | yes |
| `custom_css` | no | yes (not built yet, #14) |
| `custom_fonts`: font uploads | no | yes (not built yet, #14) |
| `scheduled_links` | yes | yes |

`Plan` (in `GET /api/auth/me`):

```json
{
  "plan": "free",
  "stored_plan": "pro",
  "source": "manual",
  "expires_at": "2026-11-01T00:00:00Z",
  "domains_grace_until": "2026-11-08T00:00:00Z",
  "pages": 5, "stats_days": 30, "custom_domains": 0,
  "features": {"custom_domains": false, "remove_branding": false, "custom_css": false, "custom_fonts": false, "scheduled_links": true},
  "pro": {"plan": "pro", "pages": 25, "stats_days": 365, "custom_domains": 1, "features": {"custom_domains": true, "...": true}}
}
```

`plan`, the limits and `features` describe what the account can use now. `stored_plan`, `source` and `expires_at` describe the grant. `pro` lists Pro's limits for upgrade prompts. Plan changes apply on the next request, so the editor unlocks as soon as an admin grants Pro.

A request for something the plan doesn't include returns `403`:

```json
{"error": {"code": "upgrade_required", "message": "Custom domains are part of Pro. Upgrade to add one.", "field": "hostname", "feature": "custom_domains"}}
```

`feature` is one of the keys above, or `pages`.

### When Pro ends

Data is kept; Pro-only settings stop applying.

- Pages over the Free limit stay and keep working. New pages can't be created until the account is under the limit.
- The footer credit comes back. The stored `hide_branding` flag is kept for when Pro returns.
- Stats requests are capped at 30 days again. Older rows stay until `ZELDA_STATS_RETENTION_DAYS` removes them.
- Custom domains keep serving for 7 days after `expires_at` (`domains_grace_until`). After that they stay attached but aren't served and get no certificate until Pro is granted again. Revoking Pro by hand (setting `free`) has no grace period.
- Domains attached before plans existed (migration `0008`) keep working on any plan.
