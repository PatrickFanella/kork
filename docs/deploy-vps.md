# Moving Kork to a VPS

Decision (#6, October 2, 2026): paid Kork will run on a VPS (option B). Until one is available, Kork stays on the homelab NUC behind the Cloudflare tunnel and Almaz Caddy. This runbook is the plan for the move; follow-up work is tracked in its own issue.

## What changes

| | Homelab today | VPS |
|---|---|---|
| Compose file | `docker-compose.proxy.yml` + local `compose.nuc.yml` | `docker-compose.yml` (app, Postgres, Caddy) |
| TLS and routing | Cloudflare tunnel → Almaz Caddy → NUC:3053 | Caddy on the VPS, ports 80/443 |
| Custom domains | Cloudflare for SaaS (#13) | Built-in DNS check plus Caddy on-demand TLS (`/internal/tls-ask`); Cloudflare for SaaS optional |
| Client IPs | `X-Forwarded-For` from cloudflared via Caddy | `X-Forwarded-For` from the bundled Caddy (or `CF-Connecting-IP` if the zone stays proxied) |
| Backups | `zelda-backup.timer` → Almaz; restic → Kvant/Volga | Same script; `REMOTE` points at a homelab host over SSH or Tailscale, plus provider snapshots |
| Monitoring | Dozor Blackbox probes | Same public probes; replace the LAN `/healthz` target with the VPS address over Tailscale |

## Sizing

Kork is one Go binary and one Postgres database. Start with 2 vCPU, 2–4 GB RAM and 40 GB disk. Uploads are capped at 5 MB each, so disk grows with users' images; watch `zelda_uploads`.

## Steps

1. **Provision.** Create the VPS, install Docker and the compose plugin, and join it to Tailscale so the homelab can reach it for backups and monitoring. Allow only 22 (or Tailscale SSH), 80 and 443.
2. **Prepare.** Clone `subculture-collective/kork` into `/opt/kork`. Copy `.env` from the NUC through a secure channel (never through chat or Git); set `ZELDA_HOST=kork.subcult.tv`, keep `ZELDA_BASE_URL`, set `ZELDA_TRUST_PROXY=true`, and set `ZELDA_CUSTOM_DOMAINS=true` if domains move to on-demand TLS.
3. **Rehearse.** Run `scripts/backup.sh` on the NUC, copy the newest backup to the VPS, restore it (README "Backups and restore") into the VPS stack, and test with a temporary hostname or a hosts-file override.
4. **Freeze and copy.** Announce a short maintenance window. Stop the NUC app container, take a final backup, restore it on the VPS, and start the VPS stack.
5. **Cut over.** Point `kork.subcult.tv` at the VPS. If the zone stays proxied through Cloudflare, change the record's origin; otherwise use a DNS-only record. Remove the `*.subcult.tv` tunnel route for Kork only, or leave the Almaz site as a fallback until the VPS is proven.
6. **Verify.** Landing, `/app/`, a published page, sign-in, an upload, an email (verification), the backup timer on the VPS and the Dozor probes.
7. **Retire the homelab copy** after a week of clean backups and probes: stop the NUC stack, keep its last backup, remove the Almaz Caddy site.

## Rollback

Until step 7 the NUC stack and its data are untouched apart from being stopped. To roll back, restore the latest VPS backup on the NUC, start its stack and point DNS back at the tunnel.
