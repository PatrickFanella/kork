#!/usr/bin/env bash
# Back up a docker-compose deployment of Zelda: a pg_dump of the database and
# a tarball of uploaded images, verified and checksummed, with retention.
#
# Run from the checkout (or set ZELDA_DIR). Settings, all optional:
#   COMPOSE_FILES  compose files, space-separated (default: docker-compose.proxy.yml,
#                  plus compose.nuc.yml when present)
#   PROJECT        compose project name (default: zelda)
#   BACKUP_DIR     where backups go (default: $ZELDA_DIR/backups)
#   KEEP           backups to keep locally and remotely (default: 14)
#   REMOTE         rsync target such as almaz:/opt/server/backups/zelda (default: none)
#   NTFY_URL       ntfy topic URL for failure alerts (default: none)
set -euo pipefail
umask 077

ZELDA_DIR=${ZELDA_DIR:-$(cd "$(dirname "$0")/.." && pwd)}
cd "$ZELDA_DIR"
PROJECT=${PROJECT:-zelda}
BACKUP_DIR=${BACKUP_DIR:-$ZELDA_DIR/backups}
KEEP=${KEEP:-14}
REMOTE=${REMOTE:-}
NTFY_URL=${NTFY_URL:-}
if [[ -z ${COMPOSE_FILES:-} ]]; then
  COMPOSE_FILES=docker-compose.proxy.yml
  [[ -f compose.nuc.yml ]] && COMPOSE_FILES+=" compose.nuc.yml"
fi

compose=(docker compose -p "$PROJECT")
for f in $COMPOSE_FILES; do compose+=(-f "$f"); done

log() { printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
fail() {
  log "ERROR: $*"
  if [[ -n $NTFY_URL ]]; then
    curl -fsS -H 'Title: Zelda backup failed' -H 'Priority: high' -H 'Tags: warning,floppy_disk' \
      --data-binary "Zelda backup failed on $(hostname): $*" "$NTFY_URL" >/dev/null 2>&1 || true
  fi
  exit 1
}
trap 'fail "line $LINENO: $BASH_COMMAND"' ERR

mkdir -p "$BACKUP_DIR"
exec 9>"$BACKUP_DIR/.lock"
flock -n 9 || { log "another backup is running"; exit 0; }

stamp=$(date -u +%Y%m%dT%H%M%SZ)
dest="$BACKUP_DIR/$stamp"
mkdir "$dest"

app=$("${compose[@]}" ps -q zelda)
[[ -n $app ]] || fail "zelda container is not running"

log "dumping database"
"${compose[@]}" exec -T postgres pg_dump -U zelda -Fc zelda > "$dest/database.dump"
# A dump that pg_restore can list is structurally complete.
"${compose[@]}" exec -T postgres pg_restore --list < "$dest/database.dump" > /dev/null \
  || fail "pg_restore could not read the dump"
"${compose[@]}" exec -T postgres psql -U zelda -d zelda -At -F ' ' > "$dest/counts.txt" <<'SQL'
SELECT 'users', count(*) FROM users UNION ALL
SELECT 'pages', count(*) FROM pages UNION ALL
SELECT 'blocks', count(*) FROM blocks UNION ALL
SELECT 'uploads', count(*) FROM uploads UNION ALL
SELECT 'domains', count(*) FROM domains;
SQL

log "archiving uploads"
# The app image is distroless, so borrow the postgres image's tar.
pg_image=$(docker inspect -f '{{.Config.Image}}' "$("${compose[@]}" ps -q postgres)")
docker run --rm --network none --volumes-from "$app:ro" "$pg_image" \
  tar -C /data -czf - uploads > "$dest/uploads.tar.gz"
files=$(tar -tzf "$dest/uploads.tar.gz" | grep -vc '/$' || true)
echo "upload_files $files" >> "$dest/counts.txt"

git rev-parse HEAD > "$dest/revision.txt" 2>/dev/null || true
(cd "$dest" && sha256sum database.dump uploads.tar.gz > checksums.txt)
log "saved $dest ($(du -sh "$dest" | cut -f1); $(tr '\n' ' ' < "$dest/counts.txt"))"

prune() { find "$1" -mindepth 1 -maxdepth 1 -type d -name '2*' | sort | head -n -"$KEEP" | xargs -r rm -rf; }
prune "$BACKUP_DIR"

if [[ -n $REMOTE ]]; then
  host=${REMOTE%%:*} path=${REMOTE#*:}
  ssh -o BatchMode=yes -o ConnectTimeout=10 "$host" "umask 077; mkdir -p '$path'"
  rsync -a "$dest" "$REMOTE/"
  ssh -o BatchMode=yes "$host" "cd '$path' && (cd '$stamp' && sha256sum -c --quiet checksums.txt) \
    && ls -1d 2* | sort | head -n -$KEEP | xargs -r rm -rf" || fail "remote copy failed verification"
  log "copied to $REMOTE/$stamp"
fi
