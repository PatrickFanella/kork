# Custom domains with Cloudflare for SaaS

Approved in [#18](https://github.com/subculture-collective/kork/issues/18) (October 2, 2026). While Kork runs behind the Cloudflare tunnel, Cloudflare for SaaS issues certificates for customer domains and routes their traffic to Kork. After a move to a VPS with the bundled Caddy (see `deploy-vps.md`), Kork's own DNS check and on-demand TLS can replace it.

Sources: [Configuring Cloudflare for SaaS](https://developers.cloudflare.com/cloudflare-for-platforms/cloudflare-for-saas/start/getting-started/), [Create Custom Hostname API](https://developers.cloudflare.com/api/resources/custom_hostnames/methods/create/), [Tunnel as fallback origin](https://developers.cloudflare.com/reference-architecture/design-guides/extending-cloudflares-benefits-to-saas-providers-end-customers/). Pricing (from the October 2026 research): 100 custom hostnames included, then $0.10 per hostname per month; check the current price when enabling.

## How traffic will flow

`links.alice.com` (customer CNAME) → `customers.subcult.tv` (proxied, our CNAME target) → fallback origin `kork-origin.subcult.tv` (tunnel public hostname) → cloudflared → Almaz Caddy → Kork on the NUC. Kork serves the page only if the hostname is verified and attached to a page; every other host gets Kork's 404.

Cloudflare's docs note that tunnel ingress rules route by hostname, and customer hostnames won't match one. So the tunnel's catch-all rule and an Almaz Caddy catch-all site must send unmatched hosts to Kork. Test this with one domain before inviting users.

## Status

Live since November 4, 2026. The zone is enrolled in Cloudflare for SaaS with fallback origin `kork-origin.subcult.tv` (proxied CNAME to the Almaz tunnel) and customer target `customers.subcult.tv` (proxied CNAME to the fallback origin). The Almaz tunnel's catch-all ingress goes to Caddy and Caddy's `:80` catch-all forwards unmatched hosts to Kork, which answers 404 for anything but a verified domain. The NUC's `.env` holds `ZELDA_CLOUDFLARE_TOKEN` (zone-scoped: SSL and Certificates edit, DNS edit, zone read), `ZELDA_CLOUDFLARE_ZONE_ID`, `ZELDA_DOMAIN_TARGET=customers.subcult.tv` and `ZELDA_CUSTOM_DOMAINS=true`. Proven with `kork-test.onnwee.me` on a throwaway Pro account: Kork registered the hostname, the customer-side CNAME and ownership TXT were added by hand, Cloudflare issued the certificate in about six minutes, the background refresh marked the domain active, and the page served over HTTPS; deleting the account removed the hostname from Cloudflare. Enrolling the zone was the only step that needed the dashboard; the API answers "no quota" until then.

## Owner steps (Cloudflare dashboard; an agent can't do these)

1. **Enable Cloudflare for SaaS** on the `subcult.tv` zone: zone → **SSL/TLS → Custom Hostnames** → enable. Cloudflare may ask for a payment method even though the first 100 hostnames are included.
2. **Fallback origin record.** In DNS for `subcult.tv`, make sure `kork-origin.subcult.tv` is a **proxied** record that routes through the existing tunnel. A tunnel public hostname creates this; the existing `*.subcult.tv` wildcard may already cover it. Then on the Custom Hostnames page set **Fallback Origin** = `kork-origin.subcult.tv` and wait for it to show **Active**.
3. **CNAME target for customers.** Create a **proxied** CNAME `customers.subcult.tv` → `kork-origin.subcult.tv`. Customers will point their domains here.
4. **API token.** My Profile → API Tokens → Create custom token: permission **Zone → SSL and Certificates → Edit**, zone resource **subcult.tv** only. Copy the token and the zone ID (zone Overview page, right column).
5. **Put them on the NUC** the same way as the SMTP key (never in chat): add `ZELDA_CLOUDFLARE_TOKEN=` and `ZELDA_CLOUDFLARE_ZONE_ID=` to `/opt/server/projects/zelda/.env`, mode 600.

## Agent steps after that

1. **Done (November 4, 2026).** The cloudflared catch-all on Almaz went from `http_status:404` to `http://caddy:80`, and the Caddy `:80` catch-all now proxies to `10.0.0.56:3053` instead of answering 404 itself. Backups sit beside both files with a `before-kork-domains-` suffix. cloudflared needed a container restart to load the new ingress.
2. **Done (branch `issue-13-cloudflare-hostnames`).** Kork uses Cloudflare when both `ZELDA_CLOUDFLARE_TOKEN` and `ZELDA_CLOUDFLARE_ZONE_ID` are set; otherwise it keeps the DNS check and on-demand TLS. Adding a domain calls the Custom Hostnames API (`ssl.method: http`, `type: dv`) and stores the hostname ID (migration `0009`). The editor shows the customer the CNAME target, any `ownership_verification` TXT record, certificate validation records and Cloudflare's error messages. Verify, and a background check every 10 minutes, mark the domain verified once the hostname and certificate are both `active`. Deleting a domain, page or account deletes the custom hostname. Custom domains are a Pro feature (#12). API details: "Domains" in `api.md`.
3. **Done (November 4, 2026).** `ZELDA_CUSTOM_DOMAINS=true` and `ZELDA_DOMAIN_TARGET=customers.subcult.tv` are set; see Status above for the end-to-end test. When giving customers the records, write the CNAME target as plain text: a value pasted with Markdown backticks around it breaks the CNAME and leaves the certificate at pending validation.
