#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-docker/compose/two-nodes.yml}"
PROJECT_NAME="${PROJECT_NAME:-flexbbs-ci-${GITHUB_RUN_ID:-$$}}"

compose_cmd=()
if docker compose version >/dev/null 2>&1; then
  compose_cmd=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  compose_cmd=(docker-compose)
else
  echo "docker compose not found; install Docker Compose v2 plugin (docker compose) or docker-compose v1." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not reachable. Start it and retry." >&2
  echo "Examples:" >&2
  echo "  - Docker Desktop: start the app" >&2
  echo "  - Linux: sudo dockerd (or start the docker service)" >&2
  exit 1
fi

dc() { "${compose_cmd[@]}" -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" "$@"; }

dump_debug() {
  echo "== docker compose ps ==" >&2
  dc ps >&2 || true
  echo >&2
  echo "== docker compose logs (full, client) ==" >&2
  dc logs --no-color full client >&2 || true
}

cleanup() {
  dc down -v --remove-orphans >/dev/null 2>&1 || true
}

trap cleanup EXIT
trap dump_debug ERR

wait_http_ok() {
  local url="$1"
  local tries="${2:-60}"
  local wait_s="${3:-1}"
  for ((i=1; i<=tries; i++)); do
    if curl -fsS "${url}" >/dev/null; then
      return 0
    fi
    sleep "${wait_s}"
  done
  echo "timeout waiting for: ${url}" >&2
  return 1
}

wait_flexipfs_api() {
  local svc="$1"
  local tries="${2:-90}"
  local wait_s="${3:-1}"
  for ((i=1; i<=tries; i++)); do
    if dc exec -T "${svc}" curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id" >/dev/null; then
      return 0
    fi
    sleep "${wait_s}"
  done
  echo "timeout waiting for flex-ipfs api in: ${svc}" >&2
  return 1
}

urlencode() {
  python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$1"
}

json_get() {
  python3 -c 'import sys, json; print(json.load(sys.stdin)[sys.argv[1]])' "$1"
}

peerlist_to_string() {
  python3 -c '
import json, sys
s = sys.stdin.read().strip()
if not s:
  print("")
  raise SystemExit(0)
try:
  v = json.loads(s)
  print(v if isinstance(v, str) else s)
except Exception:
  print(s)
'
}

echo "Starting 2-node docker compose..." >&2
dc up -d --build

echo "Waiting for bbs-node health..." >&2
wait_http_ok "http://127.0.0.1:18080/healthz" 90 1
wait_http_ok "http://127.0.0.1:28080/healthz" 90 1

echo "Waiting for flex-ipfs api..." >&2
wait_flexipfs_api full 120 1
wait_flexipfs_api client 120 1

full_cid="$(dc ps -q full)"
client_cid="$(dc ps -q client)"
if [[ -z "${full_cid}" || -z "${client_cid}" ]]; then
  echo "container IDs not found" >&2
  exit 1
fi

full_ip="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${full_cid}")"
client_ip="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${client_cid}")"
if [[ -z "${full_ip}" || -z "${client_ip}" ]]; then
  echo "container IPs not found: full=${full_ip} client=${client_ip}" >&2
  exit 1
fi

full_id_json="$(dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id")"
client_id_json="$(dc exec -T client curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id")"
full_peer="$(printf '%s' "${full_id_json}" | json_get ID)"
client_peer="$(printf '%s' "${client_id_json}" | json_get ID)"

if [[ -z "${full_peer}" || -z "${client_peer}" ]]; then
  echo "peer IDs not found: full=${full_peer} client=${client_peer}" >&2
  exit 1
fi

echo "Connecting peers (swarm/connect)..." >&2
full_ma="/ip4/${full_ip}/tcp/4001/ipfs/${full_peer}"
client_ma="/ip4/${client_ip}/tcp/4001/ipfs/${client_peer}"
dc exec -T client curl -fsS -X POST "http://127.0.0.1:5001/api/v0/swarm/connect?arg=$(urlencode "${full_ma}")" >/dev/null
dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/swarm/connect?arg=$(urlencode "${client_ma}")" >/dev/null

echo "Waiting for peerlist to include the other peer..." >&2
for _ in {1..60}; do
  full_peerlist="$(dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/dht/peerlist" | peerlist_to_string)"
  client_peerlist="$(dc exec -T client curl -fsS -X POST "http://127.0.0.1:5001/api/v0/dht/peerlist" | peerlist_to_string)"
  if [[ "${full_peerlist}" == *"${client_peer}"* && "${client_peerlist}" == *"${full_peer}"* ]]; then
    break
  fi
  sleep 1
done

full_peerlist="$(dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/dht/peerlist" | peerlist_to_string)"
client_peerlist="$(dc exec -T client curl -fsS -X POST "http://127.0.0.1:5001/api/v0/dht/peerlist" | peerlist_to_string)"
if [[ -z "${full_peerlist}" || -z "${client_peerlist}" ]]; then
  echo "peerlist is empty: full=${full_peerlist@Q} client=${client_peerlist@Q}" >&2
  exit 1
fi
if [[ "${full_peerlist}" != *"${client_peer}"* || "${client_peerlist}" != *"${full_peer}"* ]]; then
  echo "peer IDs not found in peerlist: fullPeerlist=${full_peerlist@Q} clientPeerlist=${client_peerlist@Q}" >&2
  exit 1
fi

echo "Creating board on client..." >&2
key_json="$(dc exec -T client /app/bbs-node gen-key)"
author_priv="$(printf '%s' "${key_json}" | json_get priv)"

board_id="bbs.ci.$(date +%s)"
board_title="CI Board"
board_cid="$(dc exec -T client /app/bbs-node init-board --board-id "${board_id}" --title "${board_title}" --author-priv-key "${author_priv}" --data-dir /data --autostart-flexipfs=false | tr -d '\r' | tail -n1)"
if [[ -z "${board_cid}" ]]; then
  echo "boardMeta CID not returned" >&2
  exit 1
fi

echo "Verifying board appears on client /api/v1/boards..." >&2
for _ in {1..30}; do
  if curl -fsS "http://127.0.0.1:28080/api/v1/boards" \
    | python3 -c 'import json,sys; board_id=sys.argv[1]; items=json.load(sys.stdin); raise SystemExit(0 if any(i.get("board", {}).get("boardId")==board_id for i in items) else 1)' \
      "${board_id}" >/dev/null; then
    break
  fi
  sleep 1
done

curl -fsS "http://127.0.0.1:28080/api/v1/boards" \
  | python3 -c '
import json, sys
board_id = sys.argv[1]
items = json.load(sys.stdin)
if not any(i.get("board", {}).get("boardId") == board_id for i in items):
  raise SystemExit(f"board not found on client: {board_id}")
' "${board_id}" >/dev/null

echo "Registering board on full and verifying it can load the meta from the network..." >&2
dc exec -T full /app/bbs-node add-board --board-id "${board_id}" --board-meta-cid "${board_cid}" --data-dir /data >/dev/null

for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:18080/api/v1/boards" \
    | python3 -c '
import json, sys
board_id = sys.argv[1]
title = sys.argv[2]
items = json.load(sys.stdin)
for i in items:
  b = i.get("board") or {}
  if b.get("boardId") == board_id and b.get("title") == title:
    raise SystemExit(0)
raise SystemExit(1)
' "${board_id}" "${board_title}" >/dev/null; then
    break
  fi
  sleep 1
done

curl -fsS "http://127.0.0.1:18080/api/v1/boards" \
  | python3 -c '
import json, sys
board_id = sys.argv[1]
title = sys.argv[2]
items = json.load(sys.stdin)
for i in items:
  b = i.get("board") or {}
  if b.get("boardId") == board_id and b.get("title") == title:
    raise SystemExit(0)
raise SystemExit(f"board not found on full: {board_id}")
' "${board_id}" "${board_title}" >/dev/null

echo "OK: peer connectivity + board creation verified." >&2
