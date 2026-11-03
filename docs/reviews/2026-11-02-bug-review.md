# Bug review and test run, 2026-11-02

Reviewed revision: `e2732be` (main after PR #43). The review covered the Go
server, its library packages and the React editor. Three reviewers read the
code in parallel. The most serious findings were then reproduced against a
locally built server and a disposable Postgres 17 database.

## Test results before fixes

| Check (as in `.gitea/workflows/ci.yml`) | Result |
|---|---|
| `go mod verify`, `go vet`, `gofmt` | Pass |
| `go test -race ./...` with a database | Pass, 169 tests, none skipped |
| `govulncheck@v1.7.0` | **Fail**: GO-2026-6860 in `golang.org/x/net` v0.59.0, reached through `htmlmeta.Parse` |
| `npm audit --audit-level=high` | Pass, 0 vulnerabilities |
| Editor typecheck and Vitest | Pass, 48 tests in 9 files |
| Editor and server builds | Pass |
| Server on a fresh database | Migrations applied; public routes returned the expected status codes |

Coverage with a database: 68% of `internal/...` statements from the server
tests. `internal/analytics` is at 32%. `internal/mail`, `internal/render` and
`cmd/zelda` have no tests of their own.

The local toolchain was Go 1.27.3; CI pins Go 1.26.10. The vulnerability
findings are in modules, not the standard library, so CI reports them too.

## Fixed on this branch

Each fix has a regression test. Each test was run against the original code
(through `go test -overlay` or before the change) and failed there.

1. **HTML injection through the theme's second background color.**
   `Normalize` validated `background.color2` only for gradients, and
   `cssVars` wrote it into the page's `<style>` unescaped. A free account could
   save `red}</style><meta http-equiv=refresh ...>` and the public page carried
   the redirect. `Normalize` now clears `color2` for other kinds, and
   `cssVars` rechecks every color and falls back to the default theme.
   Stored values are cleaned when rendered, because `ThemeFrom` normalizes.
   Test: `TestThemeColor2CannotEscapeStyle`.
2. **Phone and SMS links rendered as `#ZgotmplZ`.** `html/template` allows
   only http, https and mailto in `href`. `Item.Link` now marks `tel:` and
   `sms:` links that match the validated phone format as safe. Every other
   href is still filtered by the template.
   Test: `TestPhoneAndSMSLinksRender`.
3. **Custom CSS could select elements beside `<main>`.** Selectors such as
   `body ~ footer` or `~ #report-dialog` hid the report link and dialog. Rules
   whose selector continues from the scope with `~` or `+` are now removed with
   a warning. Siblings inside the scope still work.
   Test: `TestScopeRejectsSiblingsOfMain`. The existing
   `TestScopeCannotReachOutsideMain` only checked the selector prefix, so it
   passed for `:root ~ footer`.
4. **CI vulnerability check.** `golang.org/x/net` was upgraded to v0.62.0;
   `go get` also raised `x/crypto`, `x/sync`, `x/sys` and `x/text`.
   `govulncheck` now exits 0. One module-level report remains, GO-2026-5932 in
   `x/crypto`, which the code does not reach and which is fixed only in a
   pseudo-version.
5. **Editing a Brevo sign-up block failed with 422.** The API returns
   `brevo_list_id` as a number and `cleanBlockData` dropped non-string values.
   Numbers are now sent as strings, which `FlexInt` accepts. It is the only
   numeric block field.
   Test: "keeps a list ID the server returned as a number".
6. **Email verification showed a signed-in account without a session.**
   `/api/auth/verify` creates no session, yet the editor stored the returned
   user. The verify screen now reloads the real session.
7. **Title length.** The profile title field allowed 120 characters; the
   server accepts 80. The field now stops at 80.

## Open

Confirmed by reading the code:

- **Reorder rollback** (`web/src/screens/editor/BlocksTab.tsx:45`). A failed
  reorder restores the list from before the move. This can bring back a
  deleted block or hide a newly added one.
- **Schedule bounds resent on every edit** (`BlockForm.tsx:74`). A user whose
  Pro has lapsed gets 403 `upgrade_required` when editing any scheduled block.
- **Unverified custom domains never expire**
  (`migrations/0001_init.sql:66`). Anyone can hold a hostname they don't own.
- **Unsaved edits lost on tab switch.** The theme autosave timer is cleared on
  unmount (`ThemeTab.tsx:74`). Other forms have no unsaved-changes warning.

Plausible, not reproduced:

- **Forged client IP.** `clientIP` trusts the leftmost `X-Forwarded-For`
  entry. The bundled Caddy setup replaces that header; a proxy that appends
  to it would let clients bypass rate limits.
- **Stripe.** Out-of-order events within one second can drop a subscriber to
  Free. Two open checkouts can create two subscriptions.
- **Brevo delivery.**
  - Confirmed sign-ups are deleted after 7 days of failed pushes, even when
    the cause is a missing or rotated `ZELDA_SECRET_KEY`.
  - A push cancelled with the request context is never queued for retry.
  - 200 failing rows starve newer ones.
- **Mail and shutdown.** SMTP has no timeout, so a stalled relay blocks the
  weekly stats and retention loop. In-flight mail is lost on shutdown. The
  30-second write timeout can truncate account exports.
- **Races.**
  - The Patreon re-check can mix two accounts during a reconnect.
  - Feed thumbnail uploads can leave orphaned files.
  - Admin user search and the link preview can apply stale responses.
- **Low-impact.** `displayURL` can cut a multi-byte character in half.

## Tracking issues

Every finding has a Gitea issue, created 2026-11-02.

- Fixed on this branch: #44 (theme injection), #45 (phone and SMS links),
  #46 (custom CSS siblings), #47 (`x/net` vulnerability), #48 (Brevo list ID),
  #49 (verification session), #50 (title length).
- Open, confirmed: #51 (reorder rollback), #52 (schedule bounds),
  #53 (custom domain expiry), #54 (unsaved edits).
- Open, plausible: #55 (forwarded IP), #56 and #57 (Stripe), #58, #59 and #60
  (Brevo delivery), #61 and #62 (mail), #63 (export timeout), #64 (Patreon),
  #65 (thumbnail leak), #66 (stale responses), #67 (`displayURL`).
- Test coverage: #68.
