#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$SCRIPT_DIR"
RUNTIME_DIR="$(cd "$BASE_DIR/../flexible-ipfs-runtime" && pwd)"

case "$(uname -s)" in
  Darwin)
    JAVA_HOME="$RUNTIME_DIR/osx-x64/jre/Contents/Home"
    ;;
  Linux*)
    JAVA_HOME="$RUNTIME_DIR/linux-x64/jre"
    ;;
  *)
    JAVA_HOME=""
    ;;
esac

if [[ -n "${JAVA_HOME}" && -x "${JAVA_HOME}/bin/java" ]]; then
  JAVA_BIN="${JAVA_HOME}/bin/java"
else
  JAVA_BIN="$(command -v java || true)"
  if [[ -z "${JAVA_BIN}" ]]; then
    echo "java not found. Install Java 17 or use bundled runtime." >&2
    exit 1
  fi
fi

# Ensure required local paths exist for first-run
mkdir -p "${BASE_DIR}/providers" "${BASE_DIR}/getdata"
touch "${BASE_DIR}/attr"

export HOME="${BASE_DIR}"
export IPFS_HOME="${BASE_DIR}/.ipfs"

if [[ -n "${FLEXIPFS_GW_ENDPOINT:-}" ]]; then
  if [[ "${FLEXIPFS_GW_ENDPOINT}" == *$'\n'* || "${FLEXIPFS_GW_ENDPOINT}" == *$'\r'* ]]; then
    echo "FLEXIPFS_GW_ENDPOINT must be a single line" >&2
    exit 1
  fi
  PROPS_FILE="${BASE_DIR}/kadrtt.properties"
  if [[ ! -f "${PROPS_FILE}" ]]; then
    echo "kadrtt.properties not found: ${PROPS_FILE}" >&2
    exit 1
  fi
  TMP_FILE="${PROPS_FILE}.tmp"
  awk -v endpoint="${FLEXIPFS_GW_ENDPOINT}" '
    BEGIN { found=0; crlf=0 }
    {
      line=$0
      if (sub(/\r$/, "", line)) { crlf=1 }
      if (line ~ /^[ \t]*#/ || line ~ /^[ \t]*!/) { print $0; next }
      if (line ~ /^[ \t]*ipfs\.endpoint[ \t]*[=:]/) {
        suffix = (crlf ? "\r" : "")
        print "ipfs.endpoint=" endpoint suffix
        found=1
        next
      }
      print $0
    }
    END {
      if (!found) {
        suffix = (crlf ? "\r" : "")
        print "ipfs.endpoint=" endpoint suffix
      }
    }
  ' "${PROPS_FILE}" > "${TMP_FILE}"
  mv "${TMP_FILE}" "${PROPS_FILE}"
fi

cd "${BASE_DIR}"
exec "${JAVA_BIN}" -cp "lib/*" org.peergos.APIServer
