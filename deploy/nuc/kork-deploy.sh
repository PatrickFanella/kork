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
# NUC-local compose files layered on docker-compose.proxy.yml. The last one
# carries the price display environment from the October 2026 SEO release.
OVERRIDES=(docker-compose.proxy.yml compose.nuc.yml seo-releases/c20c564-preserved/deploy.json)
# Same settings as zelda-backup.timer.
export REMOTE=${REMOTE:-almaz:/opt/server/backups/zelda}
export NTFY_URL=${NTFY_URL:-http://10.0.0.56:8085/backups}
export KEEP=${KEEP:-14}

log() { printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

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

git fetch -q origin main
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
scripts/backup.sh | tail -1

log "checking out $sha"
git checkout -q --detach "$sha"
mkdir -p "$release"
# Images are named by commit, so deploying a commit again (a rollback)
# reuses the image that was built for it the first time.
if docker image inspect "zelda:$short" >/dev/null 2>&1; then
  log "reusing the existing image zelda:$short"
else
  log "building zelda:$short"
  docker build -q -t "zelda:$short" . >"$release/build.log" 2>&1 || {
    tail -20 "$release/build.log" >&2
    echo "build failed; nothing was changed" >&2
    exit 1
  }
fi

printf '{"services": {"zelda": {"image": "zelda:%s", "restart": "unless-stopped"}}}\n' "$short" >"$release/compose.json"
printf '{"services": {"zelda": {"image": "%s", "restart": "unless-stopped"}}}\n' "$current" >"$release/rollback.json"
chain=$(printf -- '-f %s ' "${OVERRIDES[@]}")
cat >"$release/README.md" <<README
# Kork release $short ($sha) deployed $(date -u +%Y-%m-%dT%H:%M:%SZ) by kork-deploy
Previous image: $current
Pre-deploy backup: the newest entry in backups/ at deploy time (also on $REMOTE)
Activate: docker compose -p zelda ${chain}-f $release/compose.json up -d --no-build --no-deps --pull never --wait zelda
Rollback: same command with $release/rollback.json, or deploy the previous commit again
README

log "activating zelda:$short"
if ! activate "$release/compose.json"; then
  echo "activation failed; restoring $current" >&2
  activate "$release/rollback.json" || echo "rollback failed too; check docker logs zelda-zelda-1" >&2
  exit 1
fi
curl -fsS "$health" >/dev/null || {
  echo "health check failed at $health; restoring $current" >&2
  activate "$release/rollback.json" || true
  exit 1
}
log "deployed zelda:$short; health ok at $health; rollback in $release/README.md"
docker inspect zelda-zelda-1 --format 'restarts={{.RestartCount}} health={{.State.Health.Status}} image={{.Config.Image}}'
