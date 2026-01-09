#!/usr/bin/env bash
set -euo pipefail

# Debug harness for reproducing LAN-like issues with 2 dockerized nodes:
# - Optional: bind full HTTP to 127.0.0.1 (unreachable from client container) to reproduce Search/announce failures.
# - Optional: corrupt client getdata/<cid>.txt to reproduce Add board JSON unmarshal failures.
#
# Examples:
#   # Happy path (full reachable, no corruption)
#   bash scripts/debug/docker-two-node-debug.sh
#
#   # Reproduce "connection refused" (full binds loopback inside container)
#   FULL_HTTP_BIND=127.0.0.1:18080 bash scripts/debug/docker-two-node-debug.sh
#
#   # Reproduce + self-heal Add board by corrupting the cached getdata file
#   CORRUPT_GETDATA=1 bash scripts/debug/docker-two-node-debug.sh

COMPOSE_FILE="${COMPOSE_FILE:-docker/compose/two-nodes.yml}"
PROJECT_NAME="${PROJECT_NAME:-flexbbs-debug-${$}}"

FULL_HTTP_BIND="${FULL_HTTP_BIND:-0.0.0.0:18080}"
CLIENT_HTTP_BIND="${CLIENT_HTTP_BIND:-0.0.0.0:18080}"
CORRUPT_GETDATA="${CORRUPT_GETDATA:-0}"

export FULL_HTTP_BIND CLIENT_HTTP_BIND

port_is_free() {
  python3 - "$1" <<'PY'
import socket, sys
p = int(sys.argv[1])
s = socket.socket()
try:
  s.bind(("127.0.0.1", p))
except OSError:
  raise SystemExit(1)
finally:
  s.close()
PY
}

pick_free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

if [[ -z "${FULL_HTTP_PORT:-}" ]]; then
  FULL_HTTP_PORT=18080
  if ! port_is_free "${FULL_HTTP_PORT}"; then
    FULL_HTTP_PORT="$(pick_free_port)"
  fi
fi
if [[ -z "${CLIENT_HTTP_PORT:-}" ]]; then
  CLIENT_HTTP_PORT=28080
  if ! port_is_free "${CLIENT_HTTP_PORT}"; then
    CLIENT_HTTP_PORT="$(pick_free_port)"
  fi
fi
if [[ "${CLIENT_HTTP_PORT}" == "${FULL_HTTP_PORT}" ]]; then
  CLIENT_HTTP_PORT="$(pick_free_port)"
fi
export FULL_HTTP_PORT CLIENT_HTTP_PORT

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
  exit 1
fi

dc() { "${compose_cmd[@]}" -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" "$@"; }

dump_debug() {
  echo "== docker compose ps ==" >&2
  dc ps >&2 || true
  echo >&2
  echo "== docker compose logs (full, client) ==" >&2
  dc logs --no-color full client >&2 || true
  echo >&2
  echo "== flex-ipfs logs (/data/logs/flex-ipfs.log) ==" >&2
  for svc in full client; do
    echo "-- ${svc} --" >&2
    dc exec -T "${svc}" sh -lc 'test -f /data/logs/flex-ipfs.log && tail -n 200 /data/logs/flex-ipfs.log || echo "no /data/logs/flex-ipfs.log"' >&2 || true
  done
  echo >&2
  echo "== getdata sample (client) ==" >&2
  dc exec -T client sh -lc 'ls -la /app/flexible-ipfs-base/getdata 2>/dev/null | tail -n 50 || true' >&2 || true
}

cleanup() {
  dc down -v --remove-orphans >/dev/null 2>&1 || true
}

trap cleanup EXIT
trap dump_debug ERR

wait_flexipfs_api() {
  local svc="$1"
  local tries="${2:-90}"
  local wait_s="${3:-1}"
  for ((i=1; i<=tries; i++)); do
    if dc exec -T "${svc}" curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id" >/dev/null 2>&1; then
      return 0
    fi
    sleep "${wait_s}"
  done
  echo "timeout waiting for flex-ipfs api in: ${svc}" >&2
  return 1
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

echo "FULL_HTTP_BIND=${FULL_HTTP_BIND} CLIENT_HTTP_BIND=${CLIENT_HTTP_BIND} CORRUPT_GETDATA=${CORRUPT_GETDATA}" >&2
echo "Starting full..." >&2
dc up -d --build full
echo "Waiting for full flex-ipfs api..." >&2
wait_flexipfs_api full 180 1

full_cid="$(dc ps -q full)"
full_ip="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${full_cid}")"
full_peer="$(dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id" | json_get ID)"
full_ma="/ip4/${full_ip}/tcp/4001/ipfs/${full_peer}"

export CLIENT_FLEXIPFS_GW_ENDPOINT="${full_ma}"
echo "Starting client with CLIENT_FLEXIPFS_GW_ENDPOINT=${CLIENT_FLEXIPFS_GW_ENDPOINT}" >&2
dc up -d --build --force-recreate client
echo "Waiting for client flex-ipfs api..." >&2
wait_flexipfs_api client 180 1

client_peer="$(dc exec -T client curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id" | json_get ID)"

echo "Waiting for peerlist to include the other peer..." >&2
for _ in {1..60}; do
  full_peerlist="$(dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/dht/peerlist" | peerlist_to_string)"
  client_peerlist="$(dc exec -T client curl -fsS -X POST "http://127.0.0.1:5001/api/v0/dht/peerlist" | peerlist_to_string)"
  if [[ "${full_peerlist}" == *"${client_peer}"* && "${client_peerlist}" == *"${full_peer}"* ]]; then
    break
  fi
  sleep 1
done

echo "full peerlist=${full_peerlist@Q}" >&2
echo "client peerlist=${client_peerlist@Q}" >&2

echo "Creating keys..." >&2
full_priv="$(dc exec -T full /app/bbs-node gen-key | json_get priv)"

echo "Creating a board on full..." >&2
board_id="bbs.debug.$(date +%s)"
board_title="debug-board-$(date +%s)"
board_cid="$(dc exec -T full /app/bbs-node init-board --board-id "${board_id}" --title "${board_title}" --author-priv-key "${full_priv}" --data-dir /data --autostart-flexipfs=false | tr -d '\r' | tail -n1)"
echo "board_id=${board_id} board_meta_cid=${board_cid}" >&2

if [[ "${CORRUPT_GETDATA}" == "1" ]]; then
  echo "Corrupting client getdata cache for boardMeta CID (simulates stale/corrupt file)..." >&2
  dc exec -T client sh -lc 'mkdir -p /app/flexible-ipfs-base/getdata && printf "Not Found\\n" > "/app/flexible-ipfs-base/getdata/$1.txt" && ls -la "/app/flexible-ipfs-base/getdata/$1.txt"' sh "${board_cid}" >&2
fi

echo "Attempting client add-board (should succeed; with CORRUPT_GETDATA it should self-heal)..." >&2
dc exec -T client /app/bbs-node add-board --board-meta-cid "${board_cid}" --data-dir /data --autostart-flexipfs=false

echo "Diagnosing HTTP reachability (client -> full)..." >&2
echo "  full service healthz from client (expected to fail if FULL_HTTP_BIND is loopback):" >&2
set +e
dc exec -T client sh -lc 'curl -fsS "http://'"${full_ip}"':18080/healthz"'
code=$?
set -e
if [[ "${code}" -ne 0 ]]; then
  echo "  healthz unreachable from client (this reproduces Search/announce failures)." >&2
else
  echo "  healthz reachable from client." >&2
fi

echo "OK: debug run completed." >&2

