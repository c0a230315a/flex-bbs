#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"

RID=""
EXT=""
case "${GOOS}" in
  linux)
    RID="linux-x64"
    EXT=""
    ;;
  darwin)
    RID="osx-x64"
    EXT=""
    ;;
  windows)
    RID="win-x64"
    EXT=".exe"
    ;;
  *)
    echo "unsupported GOOS=${GOOS} (set GOOS/GOARCH explicitly)" >&2
    exit 1
    ;;
esac

mkdir -p dist

echo "Building bbs-node (${GOOS}/${GOARCH})..."
(cd backend-go && CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build -trimpath -ldflags "-s -w" -o "../dist/bbs-node-${GOOS}-${GOARCH}${EXT}" ./cmd/bbs-node)

echo "Publishing BbsClient (${RID})..."
dotnet publish src/BbsClient/BbsClient.csproj \
  -c Release \
  -r "${RID}" \
  --self-contained true \
  -p:PublishSingleFile=true \
  -p:PublishTrimmed=false \
  -p:EnableCompressionInSingleFile=true \
  -o "dist/publish-${RID}"

BUNDLE="dist/bundle-${GOOS}-${GOARCH}"
rm -rf "${BUNDLE}"
mkdir -p "${BUNDLE}"

cp "dist/bbs-node-${GOOS}-${GOARCH}${EXT}" "${BUNDLE}/"
mkdir -p "${BUNDLE}/runtimes/${RID}/bbs-node"
cp "dist/bbs-node-${GOOS}-${GOARCH}${EXT}" "${BUNDLE}/runtimes/${RID}/bbs-node/bbs-node${EXT}"

cp "dist/publish-${RID}/BbsClient${EXT}" "${BUNDLE}/bbs-client${EXT}"

cp -a flexible-ipfs-base "${BUNDLE}/flexible-ipfs-base"
mkdir -p "${BUNDLE}/flexible-ipfs-runtime/${RID}"
cp -a "flexible-ipfs-runtime/${RID}/jre" "${BUNDLE}/flexible-ipfs-runtime/${RID}/"

echo "Bundle created: ${BUNDLE}"

