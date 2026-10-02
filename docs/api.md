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
| `/u/{uploadId}` | Uploaded image |
| `/static/*` | Public page CSS/JS |

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
| POST | `/api/auth/signup` | `{email, password}` | `201 {user}` and session cookie. Password ≥ 10 chars. |
| POST | `/api/auth/login` | `{email, password}` | `200 {user}` |
| POST | `/api/auth/logout` | – | `204` |
| GET | `/api/auth/me` | – | `200 {user}` or `401` |
| POST | `/api/auth/verify` | `{token}` | `200 {user}` |
| POST | `/api/auth/resend-verification` | – | `204` |
| POST | `/api/auth/forgot` | `{email}` | `204` (always) |
| POST | `/api/auth/reset` | `{token, password}` | `204` |

`User`: `{id, email, email_verified: bool, is_admin: bool, created_at}`

Emails link to `/app/verify?token=...` and `/app/reset?token=...`; the editor reads the query param and calls the endpoint.

Publishing a page requires a verified email (`403 {"error":{"code":"email_unverified"}}`).

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
| GET | `/api/pages/{id}/stats?days=30` | – | `200 Stats` |

Slugs: 3–32 chars, `[a-z0-9][a-z0-9-_]*`, some reserved. Limit 5 pages per user.

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
  "background": {"kind": "solid | gradient | image", "color2": "#rrggbb", "image_upload_id": "uuid"}
}
```

Changing `preset` on PATCH without `colors` resets the colors to that preset.

`Stats`: `{"total_views": 12, "views": [{"day": "2026-10-01", "count": 3}], "blocks": [{"block_id": "uuid", "label": "Instagram", "count": 4}]}`

## Blocks

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/api/pages/{id}/blocks` | `{type, data, enabled?}` | `201 Block` (appended) |
| PATCH | `/api/pages/{id}/blocks/{blockId}` | `{data?, enabled?}` | `200 Block` |
| DELETE | `/api/pages/{id}/blocks/{blockId}` | – | `204` |
| PUT | `/api/pages/{id}/blocks/order` | `{ids: [uuid, ...]}` (all block IDs) | `204` |

`Block`: `{id, type, data, enabled, position, label}` where `label` is the server's display name for lists.

Limit 100 blocks per page. Validation errors return `422` with `field` set to the data key.

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

`Domain`:

```json
{
  "id": "uuid", "hostname": "links.alice.com", "verified": false,
  "txt_name": "_zelda.links.alice.com", "txt_value": "zelda-verify=abc123",
  "target": "edge.example.com", "created_at": "..."
}
```

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

`reason`: `spam | scam | impersonation | illegal | other`.
