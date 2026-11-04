#!/usr/bin/env bash
# Deploys one main commit of Kork on the NUC. CI reaches it as the forced
# command of a dedicated SSH key, so the only thing a caller can do is
# "deploy <40-hex commit>". Install a copy outside the checkout:
#
#   install -m 0755 deploy/nuc/kork-deploy.sh ~/.local/bin/kork-deploy
#
# and point the key's authorized_keys entry at it (see DEVELOPMENT.md,
# "Automated deploys"). The copy is what runs, so a change to this file
# takes effect only when it is installed again by hand.
#
# Steps: refuse anything not on origin/main, take a verified backup, check
# out the commit, build zelda:<short>, write releases/<short>/ with the
# activation and rollback overrides, activate with the NUC's override chain
# and wait for the health check. If activation fails the previous image is
# put back.
set -euo pipefail

DIR=${KORK_DIR:-/srv/apps/projects/zelda}
# NUC-local compose files layered on docker-compose.proxy.yml.
OVERRIDES=(docker-compose.proxy.yml compose.nuc.yml)
# Pre-deploy backups live beside the nightly ones but in their own directory
# with their own retention, so a busy day of merges doesn't push nightly
# backups out of the 14 kept by zelda-backup.timer.
export BACKUP_DIR=${BACKUP_DIR:-$DIR/backups-deploy}
export REMOTE=${REMOTE:-almaz:/opt/server/backups/zelda-deploy}
export NTFY_URL=${NTFY_URL:-http://10.0.0.56:8085/homelab}
# ntfy refuses anonymous publishing; read the host's write-only token (NTFY_TOKEN=...) when it is not set.
NTFY_TOKEN_FILE=${NTFY_TOKEN_FILE:-$HOME/.config/ntfy-publisher/token.env}
if [[ -z ${NTFY_TOKEN:-} && -r $NTFY_TOKEN_FILE ]]; then
  NTFY_TOKEN=$(sed -n 's/^NTFY_TOKEN=//p' "$NTFY_TOKEN_FILE")
fi
export NTFY_TOKEN=${NTFY_TOKEN:-}
export KEEP=${KEEP:-7}

log() { printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

# A failed deploy alerts the same ntfy topic as a failed backup (homelab).
failed() {
  local msg=$1
  echo "$msg" >&2
  if [[ -n ${NTFY_URL:-} ]]; then
    curl -fsS -m 10 ${NTFY_TOKEN:+-H "Authorization: Bearer $NTFY_TOKEN"} -H "Title: Kork deploy failed" -H "Priority: high" -d "$msg" "$NTFY_URL" >/dev/null 2>&1 || true
  fi
  exit 1
}

cmd=${SSH_ORIGINAL_COMMAND:-${*:-}}
read -r verb sha rest <<<"$cmd"
if [[ $verb != deploy || ! $sha =~ ^[0-9a-f]{40}$ || -n $rest ]]; then
  echo "usage: deploy <40-hex commit on main>" >&2
  exit 2
fi

cd "$DIR"
mkdir -p releases
exec 9>releases/.deploy.lock
if ! flock -w 900 9; then
  echo "another deploy is still running" >&2
  exit 1
fi

git fetch -q origin main || failed "deploy: git fetch failed"
if ! git merge-base --is-ancestor "$sha" origin/main; then
  echo "refusing to deploy $sha: not on origin/main" >&2
  exit 3
fi
short=${sha:0:7}
release=releases/$short
publish=$(sed -n 's/^ZELDA_PUBLISH=//p' .env | tr -d '"' | tail -1)
health="http://${publish:-10.0.0.56:3053}/healthz"

compose() {
  local args=(-p zelda)
  for f in "${OVERRIDES[@]}"; do args+=(-f "$DIR/$f"); done
  docker compose "${args[@]}" "$@"
}
activate() {
  compose -f "$DIR/$1" up -d --no-build --no-deps --pull never --wait --wait-timeout 120 zelda
}

current=$(docker inspect zelda-zelda-1 --format '{{.Config.Image}}' 2>/dev/null || echo none)
if [[ $current == "zelda:$short" ]]; then
  log "zelda:$short is already running"
  curl -fsS "$health" >/dev/null && log "health ok at $health"
  exit 0
fi

log "backup before deploying $short (current image $current)"
scripts/backup.sh | tail -1 || failed "deploy $short: pre-deploy backup failed; nothing was changed"

log "checking out $sha"
git checkout -q --detach "$sha" || failed "deploy $short: checkout failed"
mkdir -p "$release"
# The installed copy of this script is what runs; say so when the commit
# being deployed carries a newer version of it.
if [[ -f deploy/nuc/kork-deploy.sh ]] && ! cmp -s deploy/nuc/kork-deploy.sh "${BASH_SOURCE[0]}"; then
  log "WARNING: deploy/nuc/kork-deploy.sh in $short differs from the installed ${BASH_SOURCE[0]}; reinstall it with: install -m 0755 deploy/nuc/kork-deploy.sh ~/.local/bin/kork-deploy"
fi
# Images are named by commit, so deploying a commit again (a rollback)
# reuses the image that was built for it the first time.
if docker image inspect "zelda:$short" >/dev/null 2>&1; then
  log "reusing the existing image zelda:$short"
else
  log "building zelda:$short"
  docker build -q -t "zelda:$short" . >"$release/build.log" 2>&1 || {
    tail -20 "$release/build.log" >&2
    failed "deploy $short: image build failed; nothing was changed"
  }
fi

printf '{"services": {"zelda": {"image": "zelda:%s", "restart": "unless-stopped"}}}\n' "$short" >"$release/compose.json"
printf '{"services": {"zelda": {"image": "%s", "restart": "unless-stopped"}}}\n' "$current" >"$release/rollback.json"
chain=$(printf -- '-f %s ' "${OVERRIDES[@]}")
cat >"$release/README.md" <<README
# Kork release $short ($sha) deployed $(date -u +%Y-%m-%dT%H:%M:%SZ) by kork-deploy
Previous image: $current
Pre-deploy backup: the newest entry in $BACKUP_DIR at deploy time (also on $REMOTE)
Activate: docker compose -p zelda ${chain}-f $release/compose.json up -d --no-build --no-deps --pull never --wait zelda
Rollback: same command with $release/rollback.json, or deploy the previous commit again
README

log "activating zelda:$short"
if ! activate "$release/compose.json"; then
  echo "activation failed; restoring $current" >&2
  activate "$release/rollback.json" || echo "rollback failed too; check docker logs zelda-zelda-1" >&2
  failed "deploy $short: activation failed; restored $current"
fi
curl -fsS "$health" >/dev/null || {
  echo "health check failed at $health; restoring $current" >&2
  activate "$release/rollback.json" || true
  failed "deploy $short: health check failed; restored $current"
}
log "deployed zelda:$short; health ok at $health; rollback in $release/README.md"
docker inspect zelda-zelda-1 --format 'restarts={{.RestartCount}} health={{.State.Health.Status}} image={{.Config.Image}}'
