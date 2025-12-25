#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-docker/compose/two-nodes.yml}"
PROJECT_NAME="${PROJECT_NAME:-flexbbs-ci-${GITHUB_RUN_ID:-$$}}"

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
  echo >&2
  echo "== flex-ipfs logs (/data/logs/flex-ipfs.log) ==" >&2
  for svc in full client; do
    echo "-- ${svc} --" >&2
    dc exec -T "${svc}" sh -lc 'test -f /data/logs/flex-ipfs.log && tail -n 200 /data/logs/flex-ipfs.log || echo "no /data/logs/flex-ipfs.log"' >&2 || true
  done
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
    if curl -fsS "${url}" >/dev/null 2>&1; then
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

echo "Starting 2-node docker compose..." >&2
dc up -d --build full

echo "Waiting for full node health..." >&2
wait_http_ok "http://127.0.0.1:${FULL_HTTP_PORT}/healthz" 120 1
echo "Waiting for full flex-ipfs api..." >&2
wait_flexipfs_api full 180 1

full_cid="$(dc ps -q full)"
if [[ -z "${full_cid}" ]]; then
  echo "container ID not found: full" >&2
  exit 1
fi

full_ip="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${full_cid}")"
if [[ -z "${full_ip}" ]]; then
  echo "container IP not found: full=${full_ip}" >&2
  exit 1
fi

full_id_json="$(dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id")"
full_peer="$(printf '%s' "${full_id_json}" | json_get ID)"
if [[ -z "${full_peer}" ]]; then
  echo "peer ID not found: full=${full_peer}" >&2
  exit 1
fi

full_ma="/ip4/${full_ip}/tcp/4001/ipfs/${full_peer}"

FULL_SET_SELF_ENDPOINT="${FULL_SET_SELF_ENDPOINT:-1}"

if [[ "${FULL_SET_SELF_ENDPOINT}" == "1" ]]; then
  echo "Configuring full flex-ipfs ipfs.endpoint to self (${full_ma})..." >&2
  dc exec -T full sh -lc '
set -e
ma="$1"
prop="/app/flexible-ipfs-base/kadrtt.properties"
if test -f "${prop}"; then
  sed -i "s|^ipfs.endpoint=.*$|ipfs.endpoint=${ma}|" "${prop}"
fi
' sh "${full_ma}"

  echo "Restarting full to apply ipfs.endpoint..." >&2
  dc restart full >/dev/null
  echo "Waiting for full node health (after restart)..." >&2
  wait_http_ok "http://127.0.0.1:${FULL_HTTP_PORT}/healthz" 120 1
  echo "Waiting for full flex-ipfs api (after restart)..." >&2
  wait_flexipfs_api full 180 1

  full_cid="$(dc ps -q full)"
  full_ip="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${full_cid}")"
  full_id_json="$(dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id")"
  full_peer="$(printf '%s' "${full_id_json}" | json_get ID)"
  full_ma="/ip4/${full_ip}/tcp/4001/ipfs/${full_peer}"
else
  echo "Skipping full flex-ipfs ipfs.endpoint self override (FULL_SET_SELF_ENDPOINT=${FULL_SET_SELF_ENDPOINT@Q})" >&2
fi

export CLIENT_FLEXIPFS_GW_ENDPOINT="${full_ma}"
echo "Starting client node with CLIENT_FLEXIPFS_GW_ENDPOINT=${CLIENT_FLEXIPFS_GW_ENDPOINT}" >&2
dc up -d --build --force-recreate client

echo "Waiting for client node health..." >&2
wait_http_ok "http://127.0.0.1:${CLIENT_HTTP_PORT}/healthz" 120 1
echo "Waiting for client flex-ipfs api..." >&2
wait_flexipfs_api client 180 1

client_cid="$(dc ps -q client)"
if [[ -z "${client_cid}" ]]; then
  echo "container ID not found: client" >&2
  exit 1
fi
client_ip="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${client_cid}")"
if [[ -z "${client_ip}" ]]; then
  echo "container IP not found: client=${client_ip}" >&2
  exit 1
fi

client_id_json="$(dc exec -T client curl -fsS -X POST "http://127.0.0.1:5001/api/v0/id")"
client_peer="$(printf '%s' "${client_id_json}" | json_get ID)"
if [[ -z "${client_peer}" ]]; then
  echo "peer ID not found: client=${client_peer}" >&2
  exit 1
fi
client_ma="/ip4/${client_ip}/tcp/4001/ipfs/${client_peer}"

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

echo "Warming up DHT putvaluewithattr on client..." >&2
client_warm_ok=0
for _ in {1..60}; do
  if dc exec -T client curl -fsS -X POST "http://127.0.0.1:5001/api/v0/dht/putvaluewithattr?value=ping&tags=ci_test" >/dev/null 2>&1; then
    client_warm_ok=1
    break
  fi
  sleep 1
done
if [[ "${client_warm_ok}" -ne 1 ]]; then
  echo "DHT putvaluewithattr warmup failed on client" >&2
  exit 1
fi

echo "Warming up DHT putvaluewithattr on full..." >&2
full_warm_ok=0
for _ in {1..60}; do
  if dc exec -T full curl -fsS -X POST "http://127.0.0.1:5001/api/v0/dht/putvaluewithattr?value=ping&tags=ci_test" >/dev/null 2>&1; then
    full_warm_ok=1
    break
  fi
  sleep 1
done
if [[ "${full_warm_ok}" -ne 1 ]]; then
  echo "WARN: DHT putvaluewithattr warmup failed on full (continuing; bbs-node has its own retry logic)" >&2
fi

MAGIC="FLEXBBS_E2E_TRUST_DELETE_20251225"
RUN_TAG="${MAGIC}-$(date +%s)-${RANDOM:-0}"

echo "Generating keys..." >&2
client_key_json="$(dc exec -T client /app/bbs-node gen-key)"
client_priv="$(printf '%s' "${client_key_json}" | json_get priv)"
full_key_json="$(dc exec -T full /app/bbs-node gen-key)"
full_priv="$(printf '%s' "${full_key_json}" | json_get priv)"

echo "Configuring client trusted indexer: http://full:18080 ..." >&2
dc exec -T client /app/bbs-node add-trusted-indexer --base-url "http://full:18080" --data-dir /data >/dev/null
dc exec -T client /app/bbs-node list-trusted-indexers --data-dir /data \
  | python3 -c 'import json,sys; want=sys.argv[1]; items=json.load(sys.stdin); raise SystemExit(0 if want in items else 1)' \
    "http://full:18080" >/dev/null

echo "Creating board on client..." >&2
board_id="bbs.e2e.$(date +%s)"
board_title="client-board-${RUN_TAG}"
board_cid=""
for attempt in {1..5}; do
  set +e
  out="$(dc exec -T client /app/bbs-node init-board --board-id "${board_id}" --title "${board_title}" --author-priv-key "${client_priv}" --data-dir /data --autostart-flexipfs=false 2>&1)"
  code="$?"
  set -e
  if [[ "${code}" -eq 0 ]]; then
    board_cid="$(printf '%s' "${out}" | tr -d '\r' | tail -n1)"
    break
  fi
  echo "init-board failed (attempt ${attempt}/5), retrying..." >&2
  echo "${out}" >&2
  sleep 2
done
if [[ -z "${board_cid}" ]]; then
  echo "boardMeta CID not returned" >&2
  exit 1
fi

echo "Verifying board appears on client /api/v1/boards..." >&2
for _ in {1..30}; do
  if curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/boards" \
    | python3 -c 'import json,sys; board_id=sys.argv[1]; items=json.load(sys.stdin); raise SystemExit(0 if any(i.get("board", {}).get("boardId")==board_id for i in items) else 1)' \
      "${board_id}" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/boards" \
  | python3 -c '
import json, sys
board_id = sys.argv[1]
items = json.load(sys.stdin)
if not any(i.get("board", {}).get("boardId") == board_id for i in items):
  raise SystemExit(f"board not found on client: {board_id}")
' "${board_id}" >/dev/null

echo "Creating thread on client (should announce to full)..." >&2
client_thread_title="client-thread-${RUN_TAG}"
client_root_body="client-root-${RUN_TAG}"
client_thread_resp="$(
  BOARD_ID="${board_id}" TITLE="${client_thread_title}" BODY="${client_root_body}" PRIV="${client_priv}" \
    python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/threads"
import os, json
print(json.dumps({
  "boardId": os.environ["BOARD_ID"],
  "title": os.environ["TITLE"],
  "displayName": "client",
  "body": {"format": "markdown", "content": os.environ["BODY"]},
  "attachments": [],
  "threadMeta": {},
  "postMeta": {},
  "authorPrivKey": os.environ["PRIV"],
}))
PY
)"
client_thread_id="$(printf '%s' "${client_thread_resp}" | json_get threadId)"
client_root_post_cid="$(printf '%s' "${client_thread_resp}" | json_get rootPostCid)"
if [[ -z "${client_thread_id}" || -z "${client_root_post_cid}" ]]; then
  echo "client createThread did not return threadId/rootPostCid" >&2
  echo "${client_thread_resp}" >&2
  exit 1
fi

echo "Waiting for full to learn the board via announce..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/boards" \
    | python3 -c 'import json,sys; board_id=sys.argv[1]; items=json.load(sys.stdin); raise SystemExit(0 if any(i.get("board", {}).get("boardId")==board_id for i in items) else 1)' \
      "${board_id}" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/boards" \
  | python3 -c '
import json, sys
board_id = sys.argv[1]
items = json.load(sys.stdin)
if not any((i.get("board") or {}).get("boardId") == board_id for i in items):
  raise SystemExit(f"board not found on full via announce: {board_id}")
' "${board_id}" >/dev/null

echo "Adding reply on client..." >&2
client_reply_body="client-reply-${RUN_TAG}"
client_reply_resp="$(
  THREAD_ID="${client_thread_id}" PARENT_CID="${client_root_post_cid}" BODY="${client_reply_body}" PRIV="${client_priv}" \
    python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/posts"
import os, json
print(json.dumps({
  "threadId": os.environ["THREAD_ID"],
  "parentPostCid": os.environ["PARENT_CID"],
  "displayName": "client",
  "body": {"format": "markdown", "content": os.environ["BODY"]},
  "attachments": [],
  "meta": {},
  "authorPrivKey": os.environ["PRIV"],
}))
PY
)"
client_reply_post_cid="$(printf '%s' "${client_reply_resp}" | json_get postCid)"
if [[ -z "${client_reply_post_cid}" ]]; then
  echo "client addPost did not return postCid" >&2
  echo "${client_reply_resp}" >&2
  exit 1
fi

echo "Replying on full (to client's reply)..." >&2
full_reply_body="full-reply-${RUN_TAG}"
full_reply_resp="$(
  THREAD_ID="${client_thread_id}" PARENT_CID="${client_reply_post_cid}" BODY="${full_reply_body}" PRIV="${full_priv}" \
    python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/posts"
import os, json
print(json.dumps({
  "threadId": os.environ["THREAD_ID"],
  "parentPostCid": os.environ["PARENT_CID"],
  "displayName": "full",
  "body": {"format": "markdown", "content": os.environ["BODY"]},
  "attachments": [],
  "meta": {},
  "authorPrivKey": os.environ["PRIV"],
}))
PY
)"
full_reply_post_cid="$(printf '%s' "${full_reply_resp}" | json_get postCid)"
if [[ -z "${full_reply_post_cid}" ]]; then
  echo "full addPost did not return postCid" >&2
  echo "${full_reply_resp}" >&2
  exit 1
fi

echo "Verifying client can see full reply (tag-based fallback is OK)..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/threads/${client_thread_id}" \
    | python3 -c 'import json,sys; cid=sys.argv[1]; tr=json.load(sys.stdin); posts=tr.get("posts") or []; raise SystemExit(0 if any(p.get("cid")==cid and not p.get("tombstoned") for p in posts) else 1)' \
      "${full_reply_post_cid}" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/threads/${client_thread_id}" \
  | python3 -c '
import json, sys
cid = sys.argv[1]
tr = json.load(sys.stdin)
posts = tr.get("posts") or []
if not any(p.get("cid") == cid for p in posts):
  raise SystemExit(f"post not found on client thread view: {cid}")
' "${full_reply_post_cid}" >/dev/null

echo "Creating a thread on full (on client's board)..." >&2
full_thread_title="full-thread-on-client-board-${RUN_TAG}"
full_thread_root_body="full-thread-root-${RUN_TAG}"
full_thread_resp="$(
  BOARD_ID="${board_id}" TITLE="${full_thread_title}" BODY="${full_thread_root_body}" PRIV="${full_priv}" \
    python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/threads"
import os, json
print(json.dumps({
  "boardId": os.environ["BOARD_ID"],
  "title": os.environ["TITLE"],
  "displayName": "full",
  "body": {"format": "markdown", "content": os.environ["BODY"]},
  "attachments": [],
  "threadMeta": {},
  "postMeta": {},
  "authorPrivKey": os.environ["PRIV"],
}))
PY
)"
full_thread_id_on_client_board="$(printf '%s' "${full_thread_resp}" | json_get threadId)"
full_thread_root_cid_on_client_board="$(printf '%s' "${full_thread_resp}" | json_get rootPostCid)"
if [[ -z "${full_thread_id_on_client_board}" || -z "${full_thread_root_cid_on_client_board}" ]]; then
  echo "full createThread did not return threadId/rootPostCid" >&2
  echo "${full_thread_resp}" >&2
  exit 1
fi

echo "Verifying client can open full-created thread..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/threads/${full_thread_id_on_client_board}" \
    | python3 -c 'import json,sys; root=sys.argv[1]; tr=json.load(sys.stdin); posts=tr.get("posts") or []; raise SystemExit(0 if any(p.get("cid")==root for p in posts) else 1)' \
      "${full_thread_root_cid_on_client_board}" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/threads/${full_thread_id_on_client_board}" \
  | python3 -c '
import json, sys
root = sys.argv[1]
tr = json.load(sys.stdin)
posts = tr.get("posts") or []
if not any(p.get("cid") == root for p in posts):
  raise SystemExit(f"root post not found when opening full-created thread: {root}")
' "${full_thread_root_cid_on_client_board}" >/dev/null

echo "Creating board+thread on full, then adding it on client..." >&2
full_board_id="bbs.e2e.full.$(date +%s)"
full_board_title="full-board-${RUN_TAG}"
full_board_cid=""
for attempt in {1..5}; do
  set +e
  out="$(dc exec -T full /app/bbs-node init-board --board-id "${full_board_id}" --title "${full_board_title}" --author-priv-key "${full_priv}" --data-dir /data --autostart-flexipfs=false 2>&1)"
  code="$?"
  set -e
  if [[ "${code}" -eq 0 ]]; then
    full_board_cid="$(printf '%s' "${out}" | tr -d '\r' | tail -n1)"
    break
  fi
  echo "full init-board failed (attempt ${attempt}/5), retrying..." >&2
  echo "${out}" >&2
  sleep 2
done
if [[ -z "${full_board_cid}" ]]; then
  echo "full init-board did not return boardMeta CID" >&2
  exit 1
fi

full_board_thread_title="full-board-thread-${RUN_TAG}"
full_board_thread_root_body="full-board-thread-root-${RUN_TAG}"
full_board_thread_resp="$(
  BOARD_ID="${full_board_id}" TITLE="${full_board_thread_title}" BODY="${full_board_thread_root_body}" PRIV="${full_priv}" \
    python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/threads"
import os, json
print(json.dumps({
  "boardId": os.environ["BOARD_ID"],
  "title": os.environ["TITLE"],
  "displayName": "full",
  "body": {"format": "markdown", "content": os.environ["BODY"]},
  "attachments": [],
  "threadMeta": {},
  "postMeta": {},
  "authorPrivKey": os.environ["PRIV"],
}))
PY
)"
full_board_thread_id="$(printf '%s' "${full_board_thread_resp}" | json_get threadId)"
full_board_root_post_cid="$(printf '%s' "${full_board_thread_resp}" | json_get rootPostCid)"
full_board_meta_cid_latest="$(printf '%s' "${full_board_thread_resp}" | json_get boardMetaCid)"
if [[ -z "${full_board_thread_id}" || -z "${full_board_root_post_cid}" || -z "${full_board_meta_cid_latest}" ]]; then
  echo "full createThread did not return threadId/rootPostCid/boardMetaCid" >&2
  echo "${full_board_thread_resp}" >&2
  exit 1
fi

for attempt in {1..5}; do
  set +e
  out="$(dc exec -T client /app/bbs-node add-board --board-meta-cid "${full_board_meta_cid_latest}" --data-dir /data --autostart-flexipfs=false 2>&1)"
  code="$?"
  set -e
  if [[ "${code}" -eq 0 ]]; then
    break
  fi
  echo "client add-board failed (attempt ${attempt}/5), retrying..." >&2
  echo "${out}" >&2
  sleep 2
done
if [[ "${code}" -ne 0 ]]; then
  echo "client add-board failed after retries" >&2
  exit 1
fi

echo "Verifying full board appears on client /api/v1/boards..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/boards" \
    | python3 -c 'import json,sys; board_id=sys.argv[1]; items=json.load(sys.stdin); raise SystemExit(0 if any((i.get("board") or {}).get("boardId")==board_id for i in items) else 1)' \
      "${full_board_id}" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/boards" \
  | python3 -c '
import json, sys
board_id = sys.argv[1]
items = json.load(sys.stdin)
if not any((i.get("board") or {}).get("boardId") == board_id for i in items):
  raise SystemExit(f"board not found on client after add-board: {board_id}")
' "${full_board_id}" >/dev/null

echo "Client replies on full board thread..." >&2
client_reply_on_full_body="client-reply-on-full-board-${RUN_TAG}"
client_reply_on_full_resp="$(
  THREAD_ID="${full_board_thread_id}" PARENT_CID="${full_board_root_post_cid}" BODY="${client_reply_on_full_body}" PRIV="${client_priv}" \
    python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/posts"
import os, json
print(json.dumps({
  "threadId": os.environ["THREAD_ID"],
  "parentPostCid": os.environ["PARENT_CID"],
  "displayName": "client",
  "body": {"format": "markdown", "content": os.environ["BODY"]},
  "attachments": [],
  "meta": {},
  "authorPrivKey": os.environ["PRIV"],
}))
PY
)"
client_reply_on_full_post_cid="$(printf '%s' "${client_reply_on_full_resp}" | json_get postCid)"
if [[ -z "${client_reply_on_full_post_cid}" ]]; then
  echo "client addPost on full board did not return postCid" >&2
  echo "${client_reply_on_full_resp}" >&2
  exit 1
fi

echo "Verifying full can see client's reply on the full board thread..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/threads/${full_board_thread_id}" \
    | python3 -c 'import json,sys; cid=sys.argv[1]; tr=json.load(sys.stdin); posts=tr.get("posts") or []; raise SystemExit(0 if any(p.get("cid")==cid for p in posts) else 1)' \
      "${client_reply_on_full_post_cid}" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/threads/${full_board_thread_id}" \
  | python3 -c '
import json, sys
cid = sys.argv[1]
tr = json.load(sys.stdin)
posts = tr.get("posts") or []
if not any(p.get("cid") == cid for p in posts):
  raise SystemExit(f"post not found on full thread view: {cid}")
' "${client_reply_on_full_post_cid}" >/dev/null

echo "Verifying full search works..." >&2
curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/search/posts?q=${RUN_TAG}&limit=1&offset=0" \
  | python3 -c 'import json,sys; items=json.load(sys.stdin); raise SystemExit(0 if len(items)>0 else 1)' >/dev/null

echo "Verifying client search proxies to trusted indexer..." >&2
curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/search/posts?q=${RUN_TAG}&limit=1&offset=0" \
  | python3 -c 'import json,sys; items=json.load(sys.stdin); raise SystemExit(0 if len(items)>0 else 1)' >/dev/null

echo "Tombstoning full reply on client thread (post deletion)..." >&2
FULL_PRIV="${full_priv}" \
  python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/posts/${full_reply_post_cid}/tombstone" >/dev/null
import os, json
print(json.dumps({"reason": "e2e", "authorPrivKey": os.environ["FULL_PRIV"]}))
PY

echo "Verifying tombstoned post is visible on client thread view..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/threads/${client_thread_id}" \
    | python3 -c 'import json,sys; cid=sys.argv[1]; tr=json.load(sys.stdin); posts=tr.get("posts") or []; raise SystemExit(0 if any(p.get("cid")==cid and p.get("tombstoned") for p in posts) else 1)' \
      "${full_reply_post_cid}" >/dev/null; then
    break
  fi
  sleep 1
done

echo "Verifying tombstoned post is excluded from search..." >&2
curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/search/posts?q=${full_reply_body}&limit=10&offset=0" \
  | python3 -c 'import json,sys; items=json.load(sys.stdin); raise SystemExit(0 if len(items)==0 else 1)' >/dev/null
curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/search/posts?q=${full_reply_body}&limit=10&offset=0" \
  | python3 -c 'import json,sys; items=json.load(sys.stdin); raise SystemExit(0 if len(items)==0 else 1)' >/dev/null

echo "Tombstoning full thread root (thread deletion = root tombstone)..." >&2
FULL_PRIV="${full_priv}" \
  python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/posts/${full_board_root_post_cid}/tombstone" >/dev/null
import os, json
print(json.dumps({"reason": "e2e-thread-delete", "authorPrivKey": os.environ["FULL_PRIV"]}))
PY

echo "Verifying deleted thread is hidden from full thread list..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/boards/${full_board_id}/threads?limit=200&offset=0" \
    | python3 -c 'import json,sys; tid=sys.argv[1]; items=json.load(sys.stdin); raise SystemExit(0 if all(i.get("threadId")!=tid for i in items) else 1)' \
      "${full_board_thread_id}" >/dev/null; then
    break
  fi
  sleep 1
done

echo "Verifying client can observe the deleted thread root as tombstoned..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/threads/${full_board_thread_id}" \
    | python3 -c 'import json,sys; cid=sys.argv[1]; tr=json.load(sys.stdin); posts=tr.get("posts") or []; raise SystemExit(0 if any(p.get("cid")==cid and p.get("tombstoned") for p in posts) else 1)' \
      "${full_board_root_post_cid}" >/dev/null; then
    break
  fi
  sleep 1
done

echo "Tombstoning client reply on client thread (post deletion)..." >&2
CLIENT_PRIV="${client_priv}" \
  python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/posts/${client_reply_post_cid}/tombstone" >/dev/null
import os, json
print(json.dumps({"reason": "e2e", "authorPrivKey": os.environ["CLIENT_PRIV"]}))
PY

echo "Verifying tombstoned client reply is visible on full thread view..." >&2
for _ in {1..60}; do
  if curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/threads/${client_thread_id}" \
    | python3 -c 'import json,sys; cid=sys.argv[1]; tr=json.load(sys.stdin); posts=tr.get("posts") or []; raise SystemExit(0 if any(p.get("cid")==cid and p.get("tombstoned") for p in posts) else 1)' \
      "${client_reply_post_cid}" >/dev/null; then
    break
  fi
  sleep 1
done

echo "Tombstoning client thread root (thread deletion = root tombstone)..." >&2
CLIENT_PRIV="${client_priv}" \
  python3 - <<'PY' | curl -fsS -H "Content-Type: application/json" -d @- "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/posts/${client_root_post_cid}/tombstone" >/dev/null
import os, json
print(json.dumps({"reason": "e2e-thread-delete", "authorPrivKey": os.environ["CLIENT_PRIV"]}))
PY

echo "Verifying deleted thread is hidden from client and full thread lists..." >&2
for _ in {1..60}; do
  client_ok=0
  full_ok=0
  if curl -fsS "http://127.0.0.1:${CLIENT_HTTP_PORT}/api/v1/boards/${board_id}/threads?limit=200&offset=0" \
    | python3 -c 'import json,sys; tid=sys.argv[1]; items=json.load(sys.stdin); raise SystemExit(0 if all(i.get("threadId")!=tid for i in items) else 1)' \
      "${client_thread_id}" >/dev/null; then
    client_ok=1
  fi
  if curl -fsS "http://127.0.0.1:${FULL_HTTP_PORT}/api/v1/boards/${board_id}/threads?limit=200&offset=0" \
    | python3 -c 'import json,sys; tid=sys.argv[1]; items=json.load(sys.stdin); raise SystemExit(0 if all(i.get("threadId")!=tid for i in items) else 1)' \
      "${client_thread_id}" >/dev/null; then
    full_ok=1
  fi
  if [[ "${client_ok}" -eq 1 && "${full_ok}" -eq 1 ]]; then
    break
  fi
  sleep 1
done

echo "OK: ${MAGIC} e2e flow verified." >&2
