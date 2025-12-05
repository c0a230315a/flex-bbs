#!/usr/bin/env bash
set -euo pipefail

# ★ 実際のパスを固定で書いています
BASE_DIR="/home/c0a230315a/dev/flex-bbs/flexible-ipfs-base"
JAVA_HOME="/home/c0a230315a/dev/flex-bbs/flexible-ipfs-runtime/linux-x64/jre"

cd "$BASE_DIR"

exec "$JAVA_HOME/bin/java" \
  -cp "lib/*" \
  org.peergos.APIServer
