#!/usr/bin/env bash
# Seeds a demo account and page through the API. Needs the server running
# without SMTP (verification links are read from its log).
# Usage: scripts/seed-demo.sh http://localhost:8080 /path/to/server.log
set -euo pipefail
base=${1:-http://localhost:8080}
log=${2:?server log path}
jar=$(mktemp)
trap 'rm -f "$jar"' EXIT
api() {
  local method=$1 path=$2 body=${3:-}
  curl -fsS -b "$jar" -c "$jar" -X "$method" -H 'Content-Type: application/json' \
    -H 'X-Requested-With: zelda' ${body:+-d "$body"} "$base$path"
}
email="demo-$RANDOM@example.com"
api POST /api/auth/signup "{\"email\":\"$email\",\"password\":\"demo password 123\"}" >/dev/null
sleep 0.5
token=$(grep -o 'verify?token=[A-Za-z0-9_-]*' "$log" | tail -1 | cut -d= -f2)
api POST /api/auth/verify "{\"token\":\"$token\"}" >/dev/null
slug=${SLUG:-demo$RANDOM}
page=$(api POST /api/pages "{\"slug\":\"$slug\",\"title\":\"Patrick Fanella & SUBCULT\"}" | sed -E 's/^\{"id":"([^"]+)".*/\1/')
block() { api POST "/api/pages/$page/blocks" "$1" >/dev/null; }
block '{"type":"service","data":{"service":"x","value":"patrick__eff","label":"Twitter / X — Patrick"}}'
block '{"type":"service","data":{"service":"instagram","value":"subcult_tv","label":"Instagram — SUBCULT"}}'
block '{"type":"service","data":{"service":"youtube","value":"subcult"}}'
block '{"type":"heading","data":{"text":"Support"}}'
block '{"type":"service","data":{"service":"patreon","value":"subcult","label":"Patreon"}}'
block '{"type":"service","data":{"service":"cashapp","value":"$subcult","show_qr":true}}'
block '{"type":"service","data":{"service":"paypal","value":"subcult"}}'
block '{"type":"service","data":{"service":"bitcoin","value":"bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh"}}'
block '{"type":"heading","data":{"text":"Elsewhere"}}'
block '{"type":"link","data":{"url":"https://subcult.tv","title":"SUBCULT","subtitle":"subcult.tv"}}'
block '{"type":"copy","data":{"label":"Discord","value":"onnwee"}}'
block '{"type":"contact","data":{"name":"Patrick Fanella","org":"SUBCULT","email":"hi@subcult.tv"}}'
block '{"type":"text","data":{"text":"Independent media and software. More at https://subcult.tv"}}'
api PATCH "/api/pages/$page" "{\"bio\":\"Independent media, software, and broadcasts.\",\"theme\":{\"preset\":\"${PRESET:-signal}\"},\"published\":true}" >/dev/null
echo "$base/$slug"
