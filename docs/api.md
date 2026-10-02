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
| `/subscribe/confirm?token=...` | Email sign-up confirmation: GET shows a button, POST (form field `token`) confirms. Main host only |
| `POST /api/subscribe/{blockId}` | Public email sign-up submission (see "Email sign-up"); also served on custom domains |
| `/u/{uploadId}` | Uploaded image or font |
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
| DELETE | `/api/account` | `{password}` | `204` and the session cookie is cleared. A Stripe subscription that could still charge is canceled first; if Stripe doesn't answer, `502 billing_unavailable` and nothing is deleted. |

`Account`: `{user, pending_email: string|null, terms_version: string|null, terms_accepted_at: string|null}`. Accounts created before terms existed have `null` terms fields and are not asked to accept.

Password checks on these endpoints are limited to 10 attempts per 15 minutes per account (`429`).

The email-change link is `/app/confirm-email?token=...` and expires after 24 hours. Requesting another change replaces the pending one. Until confirmation the account keeps its old address.

The export ZIP holds `account.json` (`exported_at, email, email_verified, created_at, terms_version, terms_accepted_at, pages: [Page fields + custom_css, blocks, domains: [{hostname, verified, created_at}], stats: [{block_id, day, count}], signup_contacts: [{id, block_id, email, confirmed_at}]], uploads: [{id, content_type, size, file}]`) and each uploaded image or font at its `file` path (`uploads/<id>.<ext>`). Stats rows with the zero block ID are page views.

`signup_contacts` lists confirmed addresses kept in Kork by the page's email sign-up blocks. Brevo API keys are never exported.

Deleting an account removes the user, billing state (including the Patreon connection; a live Stripe subscription is canceled first), sessions, tokens, pending email changes, uploads rows and files, pages, blocks, domains, reports about those pages, stats, and sign-up addresses and Brevo keys. Its public pages return 404.

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
  "embed_providers": ["YouTube", "Vimeo", "Spotify", "SoundCloud", "Twitch", "Apple Music"],
  "features": {"custom_domains": true},
  "domain_mode": "cloudflare",
  "domain_target": "customers.subcult.tv"
}
```

`domain_mode` is `cloudflare` when the server registers custom domains with Cloudflare for SaaS, otherwise `dns` (see [Domains](#domains)). `domain_target` is the hostname customers point a CNAME at.

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
| GET | `/api/pages/{id}/stats.csv?days=30` | – | `200 text/csv` download (Pro, see [Stats CSV](#stats-csv)) |
| PUT | `/api/pages/{id}/css` | `{css}` | `200 {css, warnings, applied}` (Pro, see [Custom CSS](#custom-css)) |

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
  "custom_css": "sanitized CSS or empty",
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
  "hide_branding": false,
  "font_uploads": {"body": "uuid", "display": "uuid"}
}
```

`hide_branding: true` removes the "Made with Kork" footer link. Turning it on needs Pro (`403 upgrade_required`, `feature: "remove_branding"`, `field: "hide_branding"`). Pages show the link whenever the owner's plan lacks the feature. The stored flag is kept, so other theme changes still save after Pro ends.

`font_uploads` is optional; either key can be left out. `body` sets the font for the whole page and `display` sets it for the page title and heading blocks. Each ID must be a font the user uploaded with `POST /api/uploads/font`, otherwise `422` with `field: "font_uploads"`. Choosing a font that isn't already stored needs `custom_fonts` (`403 upgrade_required`, `field: "font_uploads"`). Keeping or removing stored fonts works on every plan. A PATCH that sends a theme without `font_uploads` removes them. While the owner has the feature, the page adds `@font-face` rules named `kork-body` and `kork-display` that load `/u/{id}` with `font-display: swap`, and puts them in front of the preset font stack. Without the feature, the preset `font` is used.

### Custom CSS

`PUT /api/pages/{id}/css` with `{"css": "..."}` sanitizes the CSS, stores the result on the page and returns:

```json
{"css": "body.kork-page main.page .btn{letter-spacing:.02em}", "warnings": ["line 3: @import is not allowed"], "applied": true}
```

- Input is limited to 20 KB (`422 too_large`, `field: "css"`). A sanitized result over 64 KB is also rejected.
- Setting non-empty CSS needs `custom_css` (`403 upgrade_required`, `field: "css"`). Sending `""` removes it on every plan.
- The stored value is the sanitized output, not the input. `warnings` lists each removed part with its line number; the editor shows them.
- `applied` is false when the owner's plan doesn't include custom CSS. The CSS stays stored and the page doesn't use it until Pro returns.

The sanitizer tokenizes the input with the CSS Syntax Level 3 algorithm (`internal/customcss`), builds rules from the tokens and writes the output from tokens. It doesn't use regexes on the source text. Comments are removed and escapes are decoded before any check, so `\75 rl(` counts as `url(` and `@im/**/port` doesn't form `@import`. It keeps:

- style rules, plus `@media`, `@supports` and `@container` (up to 3 levels) containing style rules;
- `@keyframes` (and `-webkit-keyframes`) with `from`, `to` and percentage frames;
- `@font-face`, but only if every `src` URL is one of the owner's uploads. Only font descriptors are kept.

It removes:

- every other at-rule: `@import`, `@charset`, `@namespace`, `@layer`, `@page`, `@property` and the rest;
- `url()` and `src()` unless the value is exactly `/u/<uuid>` for an upload owned by the page owner. Remote, protocol-relative and `data:` URLs are removed;
- functions outside an allowlist of math, color, gradient, transform, filter, easing, shape and font functions. This removes `expression()`, `image-set()`, `cross-fade()`, `element()` and `paint()`;
- `behavior`, `-moz-binding` and any property whose name contains `behavior` or `binding`;
- strings containing `javascript:` or `vbscript:`, including when whitespace or control characters are inserted;
- nested rules inside a style rule, and declarations containing `{}` blocks;
- selectors containing at-keywords, URLs, broken tokens or delimiters that no selector uses, such as `<`, `/` and `!`.

The output can't close the `<style>` element. Identifiers and strings are written with `<`, `>`, `&`, quotes, backslashes and control characters hex-escaped. A `<` delimiter, which media range syntax uses, is always followed by `=` or a space. As a last check, output containing `</`, `<!--` or `-->` is discarded.

Scoping: each selector is prefixed with `body.kork-page main.page `. A leading `html`, `body`, `html body` or `:root` is replaced by `body.kork-page main.page`, so `body { background: #000 }` styles the page column, `:root { --accent: red }` overrides theme variables there, and `.btn` styles the buttons. The prefix adds specificity, so custom rules override the page stylesheet without `!important`. Rules can only reach elements inside `<main>`. The footer (Share, Report, Terms, Privacy) and the report dialog sit after `<main>` and are pinned visible above it, so custom CSS can't hide or cover the Report link. A prefix was chosen over CSS nesting because it works in every browser.

Public pages put the CSS in the page's nonce'd `<style>` element, after the theme variables and font rules, so the CSP still allows only nonce'd inline styles. The stored CSS is sanitized again on every render, with URL ownership not rechecked, so CSS saved under an older sanitizer is still checked.

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

### Stats CSV

`GET /api/pages/{id}/stats.csv?days=N` downloads the same aggregates as one CSV table. It needs the `stats_export` feature (`403 upgrade_required`, `feature: "stats_export"`). `days` defaults to 30 and is capped like `/stats`. The file is `text/csv; charset=utf-8`, sent as an attachment named `<slug>-stats-<days>d-<YYYY-MM-DD>.csv`.

The CSV uses one long-format table instead of several sections. Spreadsheets and data tools can load it directly and pivot it by `metric`:

```csv
date,metric,key,label,count
2026-10-01,views,,,12
2026-10-01,clicks,6f1c…,Instagram,4
2026-10-01,referrer,instagram.com,,7
2026-10-01,country,US,,9
```

| `metric` | `key` | `label` | Rows |
|---|---|---|---|
| `views` | empty | empty | one per day in the range, including zero days |
| `clicks` | block ID | block label, or `Deleted block` | one per block per day with clicks |
| `referrer` | referrer name as in `Stats` (`direct`, `other` included) | empty | one per referrer per day |
| `country` | ISO code or `unknown` | empty | one per country per day |

Rows are sorted by date, then in the metric order above, then by count, highest first. Referrers and countries aren't limited to the top 20. Text cells that start with `=`, `+`, `-`, `@`, tab or carriage return get a leading `'`, so spreadsheets don't run them as formulas.

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

When link screening is configured, web destinations are checked on create and update: `link`, `image` and `contact` `url`, `embed` `url`, the profile URL of a `service` block, a `qr` `value` that is an http(s) URL, each `events` `ticket_url` (reported with `field: "events"`) and a `latest` feed URL (`field: "source"`). A listed destination returns `422 {"error":{"code":"blocked_destination","field":"url" or "value","message":"..."}}`. Publishing a page with a listed destination returns the same code with `field: "published"`. Only the destination URL is sent to the screening provider.

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
| `email_signup` | `{title, description?, button_label?, destination, brevo_list_id?, brevo_api_key?}` | Email form with double opt-in. See "Email sign-up" |
| `events` | `{events: [{date, time?, title, city?, ticket_url?, sold_out?}]}` | 1–50 dates. See "Events" |
| `latest` | `{source, label?}` | Newest item of a YouTube channel or RSS/Atom feed. See "Latest video or post" |

### Email sign-up

`data`: `title` (≤ 80), `description` (≤ 200), `button_label` (≤ 30, default "Subscribe"), `destination` `"kork"` (default) or `"brevo"`, and for Brevo `brevo_list_id` (a number or a string of digits).

`brevo_api_key` is write-only. The server checks the key and list with Brevo (`GET /v3/contacts/lists/{id}`), seals the key with AES-256-GCM under `ZELDA_SECRET_KEY` and stores it outside the block data. Responses never contain it; they include `brevo_key_set: true` instead. On PATCH, omit the key to keep the saved one. Switching to `"kork"` deletes the key. Errors:

- `422` `field: "destination"`, code `unavailable`: the server has no `ZELDA_SECRET_KEY`. `GET /api/catalog` reports `features.brevo_signup`.
- `422` `field: "brevo_api_key"`: missing key (code `invalid`), rejected by Brevo (`brevo_key`), or Brevo unreachable (`brevo_unavailable`).
- `422` `field: "brevo_list_id"`: missing, or no such list in that Brevo account (`brevo_list`).

Key and list checks share the link-preview limit (60 per user per hour).

Public submission, on the main host and on verified custom domains:

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/subscribe/{blockId}` | `{email, website}` | `202 {"message": "Check your inbox for a link to confirm your subscription."}` |

- Needs `X-Requested-With: zelda`. 10 submissions per IP per hour (`429`).
- `website` is a honeypot the page hides from people. When it is filled in, the response is `202` and nothing is stored or sent.
- `422 field: "email"` for an invalid address; `404` when the block isn't visible on a live page.
- The response is the same whether the address is new, waiting for confirmation or already on the list. Already-listed addresses get no email. Each address gets at most 3 confirmation emails per block per hour; past that the response is still `202`.
- `429 list_full` when a block has 1,000 unconfirmed or 50,000 total addresses. `503 unavailable` if a Brevo block has lost its key.

The confirmation email links to `/subscribe/confirm?token=...` on the main host. Opening it shows a button; the address is confirmed only by the POST, so mail scanners that fetch links don't confirm it. Links expire after 48 hours, and unconfirmed addresses are deleted then.

After confirmation:

- `kork`: the address joins the block's list.
- `brevo`: the address is sent to Brevo (`POST /v3/contacts` with `listIds` and `updateEnabled: true`) and deleted from Kork. If Brevo fails, the address is kept and retried every 15 minutes for up to 7 days, then deleted.

The creator's list:

| Method | Path | Response |
|---|---|---|
| GET | `/api/pages/{id}/blocks/{blockId}/contacts` | `200 {contacts: [{id, block_id, email, confirmed_at}], pending, awaiting_push}`, newest first. `pending` counts unconfirmed addresses, `awaiting_push` confirmed addresses waiting for Brevo. |
| GET | `/api/pages/{id}/blocks/{blockId}/contacts.csv` | `200 text/csv` with `email,confirmed_at`. Values starting with `= + - @` are prefixed with `'`. |
| DELETE | `/api/pages/{id}/blocks/{blockId}/contacts/{contactId}` | `204` |

Deleting the block deletes its addresses.

### Events

Each event: `date` `YYYY-MM-DD` (years 2000–2100), optional `time` `HH:MM` 24-hour, `title` (title or venue, ≤ 100), `city` (≤ 80), `ticket_url` (http or https only), `sold_out`. The server sorts events by date and time on save. Validation errors use `field: "events"` with a message naming the event, such as `Event 2: enter the date as YYYY-MM-DD`.

Dates carry no time zone, so an event stays on the public page until its date has ended everywhere: 12:00 UTC the day after. A block whose events are all past isn't rendered. Ticket link clicks count as clicks on the block.

### Latest video or post

`source` is one of:

- a YouTube handle (`@name`, `youtube.com/@name`), stored as `https://www.youtube.com/@name`;
- a channel address (`youtube.com/channel/UC…`, or the channel's `feeds/videos.xml?channel_id=` URL), stored as `https://www.youtube.com/channel/UC…`;
- a legacy `youtube.com/user/name` address;
- any other http(s) URL, treated as an RSS or Atom feed.

Other YouTube addresses (`/c/…`, video links) are rejected. `label` (≤ 60) replaces the small "Latest video"/"Latest post" heading.

On create, and on update when `source` changes, the server fetches the feed through `internal/fetch` (same address rules and limits as link previews) and returns `422 field: "source"` (code `feed_failed`) if it can't be read or has no items. These checks share the link-preview limit.

Handles are resolved by fetching the channel page and reading its channel ID; the mapping is cached for 30 days. Feeds are read from `https://www.youtube.com/feeds/videos.xml?channel_id=…`.

The newest item (by date, else the first) is cached per block in Postgres with its title, link, date and thumbnail. The thumbnail is downloaded once per image URL and stored as an upload owned by the page owner, with the same type checks as `/api/uploads`, so pages keep `img-src 'self'`. When a cached item exists, rendering never waits for the network: after an hour (10 minutes after a failed fetch) the page serves the cached item and refreshes in the background. A block with nothing cached waits up to 1 second for its first fetch and is left out if that doesn't finish. A failed refresh keeps the last good item. Replacing an item's thumbnail or deleting the block deletes the old thumbnail upload.

## Domains

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/pages/{id}/domains` | `{hostname}` | `201 Domain` |
| POST | `/api/pages/{id}/domains/{domainId}/verify` | – | `200 Domain`; in DNS mode `422 dns_not_ready` with the reason |
| DELETE | `/api/pages/{id}/domains/{domainId}` | – | `204` |

Adding a domain needs Pro. Free accounts get `403 upgrade_required` with `feature: "custom_domains"` and `field: "hostname"`. Pro includes 1 domain per account; adding another returns `422 limit`. Verifying also needs Pro, except for domains attached before plans existed. Removing a domain always works, unless Cloudflare can't be reached (see below).

`Domain`:

```json
{
  "id": "uuid", "hostname": "links.alice.com", "verified": false,
  "txt_name": "_cf-custom-hostname.links.alice.com", "txt_value": "0e2d5a7f-...",
  "target": "customers.subcult.tv", "created_at": "...", "active": true,
  "mode": "cloudflare", "status": "pending",
  "records": [
    {"type": "CNAME", "name": "links.alice.com", "value": "customers.subcult.tv", "purpose": "routing"},
    {"type": "TXT", "name": "_cf-custom-hostname.links.alice.com", "value": "0e2d5a7f-...", "purpose": "ownership"}
  ],
  "errors": ["custom hostname does not CNAME to this zone."],
  "checked_at": "2026-10-02T12:00:00Z"
}
```

- `active` is false when the owner's plan no longer covers the domain. The domain stays attached, but the page isn't served on it and `/internal/tls-ask` refuses it (see [Plans](#plans)).
- `status` is `pending`, `active` (verified) or `error` (Cloudflare reports a state that won't fix itself; `errors` says what to do).
- `records` lists the DNS records to add. `purpose` is `routing` (CNAME to `target`), `ownership` (TXT) or `certificate` (certificate validation records Cloudflare asks for, Cloudflare mode only, hidden once verified).
- `errors` holds readable problems: Cloudflare's `verification_errors` and `ssl.validation_errors`, plus our explanation of terminal states. Empty in DNS mode.
- `checked_at` is when Cloudflare was last asked; null in DNS mode.

### DNS mode

Used when `ZELDA_CLOUDFLARE_TOKEN` or `ZELDA_CLOUDFLARE_ZONE_ID` is unset. `txt_name` is `_zelda.<hostname>` and `txt_value` is `zelda-verify=<token>`. The user adds that TXT record, points the hostname at `target` with a CNAME (or A/AAAA for apex domains), then presses Verify. Verify checks both records and returns `422 dns_not_ready` with the reason until they match. Caddy issues the certificate on the first request after asking `/internal/tls-ask`.

### Cloudflare mode

Used when both `ZELDA_CLOUDFLARE_TOKEN` and `ZELDA_CLOUDFLARE_ZONE_ID` are set ([setup](custom-domains-cloudflare.md)).

- Adding a domain creates a Cloudflare custom hostname (`ssl.method: http`, `ssl.type: dv`) and stores its ID. If Cloudflare refuses the hostname the response is `422 cloudflare_rejected` with Cloudflare's message; if Cloudflare can't be reached or rejects our token it is `502 cloudflare_unavailable`. Either way no domain is kept. A hostname left in Cloudflare by an earlier failed attempt is reused.
- `txt_name`/`txt_value` are Cloudflare's `ownership_verification` record, or empty when Cloudflare didn't send one.
- Verify reads the hostname from Cloudflare and returns `200 Domain`. `verified` becomes true only when the hostname status and the certificate status are both `active`; until then `status` and `errors` explain what's missing. Domains added before Cloudflare mode was enabled are registered with Cloudflare on their next Verify.
- Every 10 minutes the server rechecks up to 50 pending domains that weren't checked in the last 5 minutes, so users don't have to keep pressing Verify. If Cloudflare has dropped a pending hostname, `status` becomes `error` and the domain is no longer rechecked; the user removes and re-adds it.
- Deleting a domain deletes the custom hostname first. A hostname Cloudflare no longer has counts as deleted; any other failure returns `502 cloudflare_unavailable` and keeps the domain. Deleting a page or account removes its custom hostnames too; failures there are logged, and the hostname must then be removed in the Cloudflare dashboard.
- `/internal/tls-ask` keeps answering as in DNS mode. Cloudflare issues the certificates, so nothing calls it.

## Uploads

`POST /api/uploads` multipart field `file`; PNG, JPEG, WebP or GIF, max 5 MB. Response `201 {id, url, content_type, size}`.

`POST /api/uploads/font` multipart field `file`; WOFF2, WOFF, TTF or OTF, max 1 MB. Needs the `custom_fonts` feature (`403 upgrade_required`, `field: "file"`). The type comes from the file's first bytes (`wOF2`, `wOFF`, `00 01 00 00`, `OTTO`), never from the file name or declared type. WOFF and WOFF2 headers must state the file's actual length, and TTF/OTF need 1–100 tables. `content_type` is `font/woff2`, `font/woff`, `font/ttf` or `font/otf`. Other files get `422 unsupported`; files over 1 MB get `413 too_large`. Font uploads count against the same 60-per-hour upload limit.

`/u/{id}` serves fonts with their font type and `Cross-Origin-Resource-Policy: same-origin`, without CORS headers. Pages load them from their own host: the main host or the custom domain.

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
| POST | `/api/admin/users/{id}/plan` | `{plan: "free" or "pro", expires_at?: RFC 3339 or null}` | `200 Plan` for that user. Sets or clears the manual Pro grant. `expires_at` must be in the future; omitted or `null` means no expiry. `free` removes only the manual grant: an account with an active Stripe or Patreon grant stays Pro (see Billing). |

`AdminUser`: `{id, email, email_verified, suspended, created_at, plan, plan_source, plan_expires_at, effective_plan, domains_grace_until, pages}`. `plan` is the stored plan; `effective_plan` is `free` once a Pro grant has expired.

`reason`: `spam | scam | phishing | impersonation | illegal | other`.

Every 6 hours the server re-screens published pages (200 per pass, least recently checked first). If a page links to a listed destination, it is unpublished, a `phishing` report naming the URL is added to `/api/admin/reports`, and the owner gets an email. The owner can republish after removing the link.

## Plans

Every account is `free` or `pro`. Pro comes from an admin grant (`manual`), a Stripe subscription (`stripe`) or a Patreon membership (`patreon`); see Billing. A Pro grant can have an expiry, after which the account acts as Free. All limits are in one table in `internal/plans/plans.go`. The values are placeholders until the owner sets pricing.

| | Free | Pro |
|---|---|---|
| Pages | 5 (`ZELDA_FREE_PAGE_LIMIT`) | 25 |
| Stats history | 30 days | 365 days |
| Custom domains | 0 | 1 |
| `remove_branding`: hide the footer credit | no | yes |
| `custom_css`: custom CSS per page | no | yes |
| `custom_fonts`: font uploads | no | yes |
| `stats_export`: stats CSV download | no | yes |
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
  "features": {"custom_domains": false, "remove_branding": false, "custom_css": false, "custom_fonts": false, "stats_export": false, "scheduled_links": true},
  "pro": {"plan": "pro", "pages": 25, "stats_days": 365, "custom_domains": 1, "features": {"custom_domains": true, "...": true}}
}
```

`plan`, the limits and `features` describe what the account can use now. `stored_plan`, `source` and `expires_at` describe the grant. `pro` lists Pro's limits for upgrade prompts. Plan changes apply on the next request, so the editor unlocks as soon as an admin, Stripe or Patreon grants Pro.

A request for something the plan doesn't include returns `403`:

```json
{"error": {"code": "upgrade_required", "message": "Custom domains are part of Pro. Upgrade to add one.", "field": "hostname", "feature": "custom_domains"}}
```

`feature` is one of the keys above, or `pages`.

## Billing

Stripe subscriptions and the Patreon unlock are optional. Each is off until all its settings are present (see `docs/billing.md`); while off, its endpoints return `404` and `GET /api/billing` reports `enabled: false`.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/billing` | – | `200 Billing` |
| POST | `/api/billing/checkout` | `{interval: "month" or "year"}` | `200 {url}`: a Stripe Checkout page to send the browser to. `422` field `interval`; `409 already_subscribed` while a subscription is active, past due, unpaid or paused (use the portal); `502 billing_unavailable` if Stripe fails. |
| POST | `/api/billing/portal` | – | `200 {url}`: the Stripe Customer Portal. `409 no_billing_account` before the first checkout. |
| GET | `/api/billing/patreon/connect` | – | `302` to Patreon's authorize page. Navigate the browser here. |
| POST | `/api/billing/patreon/callback` | `{code, state}` | `200 Billing`. `400 bad_state` if the state is invalid, older than 15 minutes or from another session; `400 patreon_denied` if Patreon rejects the code; `409 patreon_in_use` if that Patreon account is connected to another Kork account. |
| DELETE | `/api/billing/patreon` | – | `200 Billing`. Removes the connection and the Pro it granted. |
| POST | `/stripe/webhook` | Stripe event | See below. No session or CSRF header. |

Checkout, portal and Patreon connect are limited to 20 per hour per account (`429`).

`Billing`:

```json
{
  "stripe": {
    "enabled": true, "price_monthly": "$5", "price_annual": "$48",
    "status": "active", "interval": "year", "current_period_end": "2027-10-02T12:00:00Z",
    "cancel_at_period_end": false, "payment_failed": false, "subscribed": true, "can_manage": true
  },
  "patreon": {
    "enabled": true, "connected": true, "entitled": true, "patron_status": "active_patron",
    "checked_at": "2026-10-02T12:00:00Z", "pro_until": "2026-10-10T12:00:00Z", "grace_until": null, "reconnect": false
  }
}
```

`status` is Stripe's subscription status. `interval` is set when the price is one of the configured prices. `payment_failed` is set by `invoice.payment_failed` and cleared when the subscription is active again. `grace_until` is set while Pro continues after a lost Patreon membership. `reconnect` means Patreon no longer accepts the stored authorization.

### Plan sources

The `billing` table keeps each source's grant separately: the manual grant (with optional expiry), the Stripe grant and the Patreon grant. After any change the stored plan is recomputed (`plans.Resolve`): the account is Pro if any source grants it, and the grant that lasts longest sets `source` and `expires_at`. An expired grant still counts, so the custom-domain grace period applies. Ties prefer manual, then Stripe, then Patreon.

Stripe grant by subscription status:

| Status | Pro until |
|---|---|
| `active`, `trialing` | current period end + 24 hours |
| `past_due` | the same; Stripe is retrying the payment |
| `canceled`, `unpaid`, `incomplete_expired`, `paused` | current period end, never later than the previous grant; none if there was no previous grant |
| `incomplete`, anything else | no grant |

Patreon grant: a membership qualifies when `patron_status` is `active_patron` and one of its `currently_entitled_tiers` is in `ZELDA_PATREON_TIER_IDS` (in the campaign `ZELDA_PATREON_CAMPAIGN_ID`). A qualifying check grants Pro for 8 days (7-day grace plus one day between checks). Connected accounts are re-checked daily; the check refreshes the token and reads the membership again. When a check finds no qualifying membership, or Patreon rejects the refresh token, Pro continues for 7 days from that check and is then dropped. A later qualifying check or reconnect restores it. Network or Patreon errors change nothing and are retried hourly.

### Stripe webhook

`POST /stripe/webhook` checks the `Stripe-Signature` header as Stripe documents: the HMAC-SHA256 of `"{t}.{raw body}"` keyed with `ZELDA_STRIPE_WEBHOOK_SECRET` must match one of the `v1` signatures, and `t` must be within 5 minutes of the server clock. Otherwise `400`.

Handled events: `checkout.session.completed` (links the customer and subscription to `client_reference_id`, then reads the subscription from the API), `customer.subscription.created`, `customer.subscription.updated`, `customer.subscription.deleted` and `invoice.payment_failed`. Other types are acknowledged and ignored. Each event ID is applied once; repeats return `200` without changes. Subscription events older than the newest one applied are skipped. Events for unknown customers are acknowledged. A processing error returns `500` so Stripe retries. Event IDs are kept for 30 days.

Both API versions are read: `current_period_end` on the subscription (before 2025-03-31) or on its items (later), and an invoice's subscription at `subscription` or `parent.subscription_details.subscription`.

### When Pro ends

Data is kept; Pro-only settings stop applying.

- Pages over the Free limit stay and keep working. New pages can't be created until the account is under the limit.
- The footer credit comes back. The stored `hide_branding` flag is kept for when Pro returns.
- Stats requests are capped at 30 days again. Older rows stay until `ZELDA_STATS_RETENTION_DAYS` removes them. CSV download returns `403 upgrade_required`.
- Custom CSS stays stored but isn't added to the page. It can be removed (`PUT .../css` with `""`) but not changed.
- Uploaded fonts stay in the theme but the page uses the preset `font`. Theme changes that keep or remove them still save.
- Custom domains keep serving for 7 days after `expires_at` (`domains_grace_until`). After that they stay attached but aren't served and get no certificate until Pro is granted again. Revoking Pro by hand (setting `free`) has no grace period.
- Domains attached before plans existed (migration `0008`) keep working on any plan.
