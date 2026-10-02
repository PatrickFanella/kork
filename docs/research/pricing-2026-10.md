# Kork pricing research

Researched October 2, 2026. All prices are USD. Each figure carries its source in the Sources section. "Secondary" means a third-party review site, used where the vendor page blocked automated reading (HTTP 403) or rendered prices with JavaScript. Recheck secondary figures in a browser before quoting them publicly.

## Recommendation

**Two tiers now: Free and Kork Pro. Add Studio later.**

| | Free | Kork Pro |
|---|---|---|
| Price | $0 | **$5/month or $48/year** (two months free), or the SUBCULT Patreon Backer tier ($10/month) |
| Links, all block types, embeds, contact card | Unlimited | Unlimited |
| Payment buttons (Cash App, PayPal, etc.) | Yes, 0% Kork fee | Yes, 0% Kork fee |
| QR codes | Yes | Yes, plus colour and logo options |
| Themes | All 10 | All 10, plus custom CSS and fonts |
| Pages per account | 1 | 5 |
| Analytics (anonymous, no trackers) | Last 30 days, daily totals | 12 months, referrers/countries/click-through, CSV export, weekly email |
| Kork footer | Shown | Removable |
| Custom domain | No (kork.subcult.tv/name) | 1 domain, via Cloudflare for SaaS |
| Scheduled and expiring links (#10) | No | Yes |
| Email sign-up block (#11) | Yes, with CSV export | Yes, plus newsletter integrations |

Reasons:

- **$5/month sits under every subscription competitor that offers comparable features.** Linktree Pro is $15/month ($12 annual), Beacons Creator Pro $10, Milkshake Pro $6.99 and Pro+ (custom domain) $10, bio.link Pro about $6 (secondary). Carrd ($19/year with custom domain) and Lnk.Bio ($24.99 lifetime plus $29.99/year for a domain) are cheaper or comparable, but neither competes on the no-trackers stance, and Carrd is a general site builder rather than a link page.
- **Every Pro feature either costs money or matters most to people with an audience,** which follows the principles in `docs/ROADMAP.md`. Linktree, Milkshake and Lnk.Bio all keep unlimited links free. Kork would also keep payment buttons, QR codes and every theme free, so its free tier is at least as generous as theirs on the features people choose a link page for.
- **Kork charges no transaction fee on either tier.** Linktree takes 12% of sales on Free and 9% on Starter/Pro (secondary); Beacons takes 9% on Free and Creator Pro (secondary). Kork's payment blocks link out to the creator's own processor, so this costs Kork nothing and is a clear message: "Kork never takes a cut."
- **Annual at $48** matches the 17-25% annual discounts the market uses (Linktree about 20%, Beacons 17%, Later 25%). The annual plan also cuts Stripe's fixed 30-cent fee from twelve charges to one (see break-even).
- **No lifetime plan.** Lnk.Bio sells lifetime access, but custom domains and analytics retention are ongoing costs for Kork. A founding-member price lock for early annual buyers is a safer way to reward early users.

**Patreon mapping.** SUBCULT's public Patreon has one paid tier, Backer at $10/month (recorded in `subcult-studio/content/patreon/Patreon Relaunch 2026-08.md` from a 2026-08-09 public search; not rechecked live today). Recommendation: an active Backer patron (`patron_status = active_patron` and Backer among `currently_entitled_tiers`) unlocks Pro on one Kork account. Pro stays on for a 7-day grace period after a declined payment, then reverts to Free without deleting Pro settings. Do not add a $5 Kork-only Patreon tier now: the Studio notes say to add tiers only after four weeks of delivery show a distinct benefit, and Patreon keeps a larger share of a $5 pledge than Stripe does (see costs). Put the Kork benefit in the Backer tier description and on the Kork upgrade page, linking to the web checkout rather than the iOS app.

**Studio/Team tier: later, not now.** Kork has no team seats, shared editing or multi-account management, and there is no demand evidence yet. Once Pro has paying users and someone asks, a Studio tier around $15/month ($144/year) could include 10 pages, 5 custom domains, 3 seats and an account switcher. That is roughly half of Beacons Store Pro ($30) and Linktree Premium ($35), and above Lnk.Bio's agency plans, which start at $2/month for 3 accounts.

## Competitor comparison

Observed 2026-10-02. "P" = vendor's own page; "S" = secondary source.

| Product | Status | Tiers and price (monthly / annual per month) | Custom domain | Remove branding | Analytics history | Other paid features | Fee on sales/tips |
|---|---|---|---|---|---|---|---|
| Linktree | Active | Free; Starter $8 / $6; Pro $15 / $12; Premium $35 / $30 (S). Pro was raised from $9 in Nov 2025 (P, per Lnk.Bio) | Not confirmed per tier; secondary sources disagree | Pro and Premium (P) | Starter 90 days, Pro 365 days, Premium lifetime (P) | Scheduling, fonts/colours, unlimited featured links on all paid tiers; Mailchimp/Klaviyo/Kit, Meta Pixel, GA, UTM on Pro+ (P) | Digital sales: 12% Free, 9% Starter/Pro, 0% Premium, plus Stripe (S) |
| Beacons | Active | Free; Creator Pro $10 / ~$8.33; Store Pro $30 / $25; Business Pro $90 / $75 (S) | Paid tiers (S, not confirmed per tier) | Paid tiers (S) | Not verified | Store, email marketing (S) | 9% Free and Creator Pro; 0% Store Pro and up, plus Stripe (S) |
| Carrd | Active | Free; Pro Lite $9/yr (3 sites); Pro Standard $19/yr (10 sites); Pro Plus $49/yr (25 sites); larger site packs up to $999/yr (P) | Pro Standard and up (P) | All Pro tiers (P) | Analytics via integrations, Standard and up (P) | Forms and Stripe/Mailchimp integrations on Standard; password protection, redirects on Plus (P) | None found |
| Lnk.Bio | Active | Free; Mini $0.99/mo or $9.99 lifetime; Unique $24.99 lifetime; agency from $2/mo (P) | $29.99/yr add-on on any paid plan (P) | Unique only (P) | Mini: advanced analytics, "2-year historical data" (P) | Scheduling, 5 pages (Mini), 10 pages (Unique), add-ons $2-4/mo (P) | Not stated |
| Taplink | Active | Free; Pro ~$4/mo; Business ~$8/mo billed annually (S, sources conflict on monthly billing) | Paid (P lists custom domain and SSL as paid features) | Paid (P) | Not verified | Forms, payments | Not verified |
| Milkshake | Active | Free; Lite $2.99/mo; Pro $6.99/mo or $59.99/yr; Pro+ $29.99/quarter or $99.99/yr (P) | Pro+ only (P) | Lite and up (P) | Free 30 days; Pro and Pro+ 365 days with export (P) | Contact form, mailing list, Meta pixel, SEO on Pro (P) | Sells files via Stripe; Pro "lower fees", Pro+ "lowest fees", percentages not published on the page (P) |
| Later (Linkin.bio) | Active, bundled only | Starter $25 / $18.75; Growth $50 / $37.50; Scale not verified (S). 25% annual discount (P) | Not verified | "No Later Branding" listed in plan comparison (P) | Starter 3 months, Growth 1 year, Scale 2 years (P) | Link-in-bio is part of the social scheduler, not sold alone (S) | Not applicable |
| Stan Store | Active | Creator $29 / $25 ($300/yr); Creator Pro $99 / $79 ($948/yr) (P). No free plan, 14-day trial (S) | Not verified | Not verified | Not verified | Store, courses, bookings; Pro adds funnels, email, affiliates, pixels (P/S) | 0% platform fee; Stripe or PayPal processing applies (P) |
| bio.link | Active | Free; Pro ~$6/mo (S) | Pro (S) | Not verified | Pro: advanced (S) | | Not verified |
| Bento.me | **Shut down 2026-02-13** after Linktree acquisition; users offered migration to Linktree | | | | | | |
| Koji | **Shut down 2024-01-31** after Linktree acquisition | | | | | | |

Takeaways for Kork:

- Unlimited links are free almost everywhere, so a link cap would make Kork worse than Linktree, Lnk.Bio and Milkshake.
- The usual paid features are branding removal, longer analytics history, custom domains, scheduling, email/newsletter integrations and extra pages. The proposed Pro tier covers the same set.
- Custom domain pricing ranges from about $19/year (Carrd Pro Standard) to $8-15/month (Milkshake Pro+, Linktree Pro). $5/month with a domain included is near the low end for a dedicated link-in-bio product.
- Commerce products (Beacons, Stan, Linktree Premium) charge $29-99/month or take a sales percentage. Kork should not compete there; its payment blocks only link out.

## Cost notes

### Cloudflare for SaaS (custom domains)

- Available on Free, Pro and Business zones. Each includes **100 custom hostnames**; each additional hostname is **$0.10/month**. Enterprise is custom-priced.
- With one domain per Pro account, the first 100 Pro custom domains cost nothing. At 500 Pro domains: 400 × $0.10 = $40/month, under 2% of the Stripe revenue those accounts bring in.
- The roadmap notes that the current tunnel only serves hostnames it lists. Cloudflare for SaaS routes custom hostnames to a fallback origin; whether that origin can be the existing tunnel hostname still needs testing (#13).

### Stripe (US, subscriptions)

- Card processing: **2.9% + $0.30** per successful domestic card charge; **+1.5%** for international cards; **+1%** if currency conversion is needed.
- Stripe Billing, pay-as-you-go: **0.7% of billing volume**, no monthly fee. Monthly plans start at $620/month and only make sense above roughly $90,000/month in billing volume.
- Stripe Tax Basic: **0.5% per transaction** for no-code integrations (including Checkout) or **$0.50 per transaction** via the API, charged **only where Kork is registered to collect tax**. Tax Complete starts at $90/month. SaaS taxability varies by state; registration obligations at Kork's scale have not been assessed.

Per-charge cost, assuming domestic card and Tax Basic no-code in a registered state:

| Charge | Card | Billing 0.7% | Tax 0.5% | Total fees | Net |
|---|---|---|---|---|---|
| $5 monthly | $0.445 | $0.035 | $0.025 | **$0.505 (10.1%)** | $4.50/month |
| $48 annual | $1.692 | $0.336 | $0.240 | **$2.27 (4.7%)** | $45.73/year ($3.81/month) |

The fixed 30 cents is most of the monthly fee. Monthly prices below $5 lose a much larger share; at $3/month the fees would be about 13%.

### Patreon

- Creators whose pages were published after **2025-08-04** pay a **10% platform fee** (the old Pro 8% and Premium 12% plans were merged). Pages published before then keep their legacy rate (secondary sources cite 5%, 8% or 11%). **Whether SUBCULT's page is on a legacy rate was not verified.**
- Payment processing (secondary): about **2.9% + $0.30** for standard US payments; pledges of $3 or less use a micropayment rate of **5% + $0.10**. Payout and currency-conversion fees are extra.
- Net on the $10 Backer tier at 10%: $10 − $1.00 − $0.59 ≈ **$8.41**. A hypothetical $5 tier would net about $4.06, below Stripe's $4.50.
- **iOS in-app billing:** Apple set **2026-11-01** as the deadline for remaining Patreon creators to move to subscription billing. New memberships started inside the iOS app pay Apple's commission (30%, falling to 15% after a year). Memberships started in a web browser are not affected. Kork's upgrade page should link to the web checkout.
- **Membership checks via API v2 work.** Register the OAuth client under the SUBCULT creator account and request the `identity` scope (add `identity[email]` if you want to match by email). Per Patreon's docs: "If you request memberships and DON'T have the `identity.memberships` scope, you will receive data about the user's membership to your campaign." Call `GET /api/oauth2/v2/identity?include=memberships,memberships.currently_entitled_tiers` and read `patron_status` (`active_patron`, `declined_patron`, `former_patron`, or null) plus the tier IDs. For changes between logins, use `members:create`, `members:update` and `members:delete` webhooks. Webhooks managed through the API need `w:campaigns.webhook`; `campaigns.members` lets the creator token read the full member list for periodic reconciliation. Rate limits: 100 requests per 2 seconds per client, 100 per minute per access token.

### Break-even

Variable cost per Pro account is close to zero until Cloudflare's 100 free hostnames are used up; storage and bandwidth for link pages are small. The real costs are fixed: hosting (homelab or VPS, undecided in #6), transactional email, backups and the time spent on support and abuse handling. None of these were priced in this research.

Pro subscribers needed = fixed monthly cost ÷ net per subscriber. With $4.50 net (monthly Stripe), $3.81 (annual Stripe) or $8.41 (Patreon Backer):

| Fixed monthly cost (illustrative, not quoted) | Monthly Stripe subs | Annual Stripe subs | Patreon Backers |
|---|---|---|---|
| $10 | 3 | 3 | 2 |
| $25 | 6 | 7 | 3 |
| $50 | 12 | 14 | 6 |

Cloudflare adds $0.10 per Pro account beyond the first 100, which barely changes these numbers. On the homelab NUC, the cash break-even is one or two subscribers. The real question is whether the time spent on support, abuse handling and uptime is worth it at that scale.

## Sources

All accessed 2026-10-02.

Competitors

- Linktree paid features (P): https://linktr.ee/help/en/articles/5434140-an-overview-of-paid-features-available-on-linktree
- Linktree pricing page, blocked to automated reads: https://linktr.ee/s/pricing
- Linktree prices and seller fees (S): https://easyapp.ai/en/blog/linktree-pricing ; https://bytesize.me/blog/linktree-pricing (dated July 2026; its custom-domain and logo rows conflict with Linktree's help centre and were not used)
- Linktree November 2025 increase, as reported by a competitor (P, Lnk.Bio): https://lnk.bio/what-is-lnk-bio
- Beacons pricing page, blocked (403): https://beacons.ai/pricing
- Beacons (S): https://niftysite.co/resources/beacons-pricing ; https://talkspresso.com/blog/beacons-pricing-2026
- Carrd (P): https://carrd.com/pro
- Lnk.Bio (P): https://lnk.bio/all-features ; https://lnk.bio/what-is-lnk-bio
- Taplink feature list (P, prices not rendered): https://taplink.at/en/prices.html
- Taplink prices (S): https://www.spotsaas.com/product/taplink/pricing ; https://www.capterra.com/p/235736/Taplink/
- Milkshake (P): https://milkshake.app/
- Later plan comparison (P, prices not rendered): https://later.com/pricing/
- Later prices (S): https://www.blotato.com/blog/later-pricing ; https://boomp.net/blog/later-pricing-free-plan-2026
- Stan Store (P): https://stan.store/blog/stan-store-pricing/
- Stan trial (S): https://kourses.com/stan-store-pricing/
- bio.link (S): https://www.saasworthy.com/product/bio-link
- Bento shutdown: https://alternativeto.net/news/2025/12/bento-to-shut-down-in-2026-as-linktree-takes-over-and-offers-migration-path/
- Koji shutdown: https://www.tubefilter.com/2023/12/14/linktree-link-in-bio-acquires-koji/

Costs

- Cloudflare for SaaS plans: https://developers.cloudflare.com/cloudflare-for-platforms/cloudflare-for-saas/plans/
- Stripe payments pricing: https://stripe.com/pricing
- Stripe Billing pricing: https://stripe.com/billing/pricing
- Stripe Tax pricing: https://stripe.com/tax/pricing
- Patreon pricing page, blocked (403): https://www.patreon.com/pricing
- Patreon August 2025 fee change, creator post (blocked to automated reads): https://www.patreon.com/posts/were-increasing-130695366
- Patreon fee details (S): https://www.patron.com/blog/post/patreon-fee-changes-2025/ ; https://www.gemlist.io/blog/how-much-does-patreon-take
- Apple and Patreon billing deadline: https://www.macworld.com/article/3046184/apple-sets-deadline-for-patreon-to-migrate-creators-to-in-app-subscriptions.html ; https://www.macobserver.com/news/patreon-creators-get-more-time-before-apple-takes-30-percent-of-ios-subscriptions/
- Patreon API v2 (scopes, identity endpoint, webhooks, rate limits): https://docs.patreon.com/

Local

- SUBCULT Patreon tier notes: `/home/onnwee/Work/Subcult/subcult-studio/content/patreon/Patreon Relaunch 2026-08.md`, `Patreon Positioning.md`
- Kork charging principles: `docs/ROADMAP.md`

## Not verified

- Linktree prices and seller fees come only from secondary sources; it is also unconfirmed which Linktree tier includes custom domains.
- All Beacons, Taplink, Later and bio.link prices and limits come from secondary sources.
- Milkshake's selling fee percentages per tier.
- Patreon processing rates come from secondary sources; SUBCULT's platform-fee rate (legacy or 10%) and the live Backer tier price and benefits were not checked.
- Whether Cloudflare for SaaS can use the existing Cloudflare Tunnel hostname as its fallback origin.
- Kork's US sales-tax registration obligations for SaaS.
