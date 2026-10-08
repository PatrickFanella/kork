# Kork roadmap

Written October 2, 2026. Each step is a Gitea issue; this file explains the order and the reasoning. Update both when plans change.

Status checked October 8, 2026: the feature implementations through Phase 3 are in the code. Backups, monitoring, account controls, imports, previews, analytics, scheduling, additional blocks and Pro feature gates have closed issues. Production link screening now uses a populated PhishTank CSV cache, refreshed daily; a listed URL was rejected through the public API and a safe control was accepted. Terms/privacy version `2026-10-08` is live with the owner's operator, jurisdiction, age and contact choices. The owner approved $5/month or $48/year through UNDERTOW Stripe and SUBCULT Backer access through Patreon, with scheduled links free and a five-page Free limit. Stripe's test and live catalogs are prepared; server credentials and real-provider subscription, webhook and OAuth qualification are pending. See [billing catalog](billing-catalog.md). Custom domains (#13) and VPS migration (#20) are deferred at the owner's request for the current work.

## Where Kork is now

Kork (code name `zelda`) is live at https://kork.subcult.tv: self-serve signup with email verification, 92 catalog services, 11 block types, 10 themes, QR codes, uploads, anonymous daily stats, reports and admin suspension. It runs on the NUC behind the Cloudflare tunnel and Almaz Caddy. Nightly backups go to the NUC and Almaz. Custom domains exist in the code but are switched off, because the tunnel only serves hostnames it lists.

## Principles for charging

- **Charge for what costs money or matters to people with an audience:** custom domains, longer analytics, removing the Kork footer, custom CSS and fonts.
- **Keep the core free:** links, payment buttons, QR codes and themes. A useful free tier is what spreads the product.
- **No ads and no trackers.** The landing page promises this.
- **No cut of creator tips.** Payment blocks link straight to Cash App, PayPal and the rest. Handling money through Stripe Connect would add identity checks, payouts, disputes and tax reporting.
- **SUBCULT Patreon supporters can unlock Pro.** This could ship before or alongside Stripe.

## Phase 1: Ready to charge

Nobody should pay until these are done.

| Issue | Step |
|---|---|
| [#1](https://git.subcult.tv/subculture-collective/zelda/issues/1) | Add Kork to the off-site restic backups (Kvant and Volga) |
| [#2](https://git.subcult.tv/subculture-collective/zelda/issues/2) | Monitor kork.subcult.tv and alert on downtime |
| [#3](https://git.subcult.tv/subculture-collective/zelda/issues/3) | Screen links for phishing and gate publishing for new accounts |
| [#4](https://git.subcult.tv/subculture-collective/zelda/issues/4) | Terms of service and privacy policy |
| [#5](https://git.subcult.tv/subculture-collective/zelda/issues/5) | Account deletion and data export |
| [#6](https://git.subcult.tv/subculture-collective/zelda/issues/6) | Decision: homelab, VPS, or both for paid Kork |

## Phase 2: Worth switching to

Features that make Kork worth choosing over an existing link page. These can run in parallel with Phase 1.

| Issue | Step |
|---|---|
| [#7](https://git.subcult.tv/subculture-collective/zelda/issues/7) | Import from Linktree and other link pages |
| [#8](https://git.subcult.tv/subculture-collective/zelda/issues/8) | Automatic link previews (title and thumbnail) |
| [#9](https://git.subcult.tv/subculture-collective/zelda/issues/9) | Analytics without tracking: referrers, countries, click-through, weekly email |
| [#10](https://git.subcult.tv/subculture-collective/zelda/issues/10) | Scheduled and expiring links |
| [#11](https://git.subcult.tv/subculture-collective/zelda/issues/11) | New blocks: email sign-up, events, latest video/post |

## Phase 3: Kork Pro

| Issue | Step |
|---|---|
| [#12](https://git.subcult.tv/subculture-collective/zelda/issues/12) | Plan support and feature gates, with Pro granted by hand at first |
| [#13](https://git.subcult.tv/subculture-collective/zelda/issues/13) | Custom domains through Cloudflare for SaaS (first Pro feature) |
| [#14](https://git.subcult.tv/subculture-collective/zelda/issues/14) | Remaining Pro features: no footer, analytics history, more pages, custom CSS, fonts |
| [#15](https://git.subcult.tv/subculture-collective/zelda/issues/15) | Billing: Patreon unlock and/or Stripe subscriptions |

## Suggested order

1. #1 and #2: small, and they protect what's already live.
2. #3, #4 and #5: the abuse and legal baseline.
3. #7 and #8: the biggest growth levers. The fetcher #7 builds is reused by #8 and #11.
4. #12: plan gates, so Pro features can ship before billing exists.
5. #13: custom domains, after you enable Cloudflare for SaaS on the zone.
6. #15 and #14: billing, then the rest of Pro.

## Decisions only the owner can make

- Where paid Kork runs (#6).
- Billing route and prices (#15): approved October 8, 2026 as UNDERTOW Stripe at $5/month or $48/year, plus SUBCULT Backer membership.
- Enabling Cloudflare for SaaS and accepting its per-hostname pricing (#13).
- Policy details (#4): SUBCULT, Illinois/USA, minimum age 18 and `info@subcult.tv`; published as version `2026-10-08`. Final text is available for owner review.
