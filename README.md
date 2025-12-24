# flex-bbs

Experimental decentralized BBS on top of **Flexible‑IPFS** + **Go** + **C# client**.

This repo currently contains:

- `flexible-ipfs-base/` – prebuilt Flexible‑IPFS jars + run scripts.
- `flexible-ipfs-runtime/` – bundled Java 17 runtimes per OS (`linux-x64`, `win-x64`, `osx-x64`).
- `backend-go/` – Go backend node `bbs-node` (HTTP API under `/api/v1`).
- `src/BbsClient/` – C# client (CLI + interactive TUI) (`dotnet run --project src/BbsClient`).

## Contents

- Quickstart (prebuilt bundle)
  - Start via CLI (`bbs-node`)
  - Start via TUI (`bbs-client`)
- CLI usage (commands)
- TUI usage (interactive)
- Windows 2-PC TUI guide (mDNS, FULL/CLIENT, create board)
- Docker 2-node integration test (CI)
- Build from source

## Prebuilt bundle (one download)

GitHub Actions builds OS‑specific bundles that include everything needed:

- `bbs-node` binary
- `bbs-client` binary
- `flexible-ipfs-base/` (jars + scripts)
- `flexible-ipfs-runtime/<os>/jre` (bundled Java 17 for that OS)

1. Download the latest artifact from GitHub Actions:
   - Linux: `flex-bbs-linux-amd64.tar.gz`
   - Windows: `flex-bbs-windows-amd64.zip`
   - macOS: `flex-bbs-darwin-amd64.tar.gz`
   Releases tagged from `main` are stable; tags from `develop` are marked as pre‑releases.
2. Extract it. The folder contains `bbs-node-*` plus `flexible-ipfs-*` directories.
3. Start via **CLI** or **TUI** (documented separately below).

### Start via CLI (`bbs-node`)

`bbs-node` autostarts Flexible‑IPFS by default (`--autostart-flexipfs=false` to disable).

- Linux / macOS:
  ```bash
  ./bbs-node-linux-amd64 --role=client --http 127.0.0.1:8080
  curl http://127.0.0.1:8080/healthz
  ```
- Windows:
  ```bat
  bbs-node-windows-amd64.exe --role=client --http 127.0.0.1:8080
  curl http://127.0.0.1:8080/healthz
  ```

### Start via TUI (`bbs-client`)

- Windows: double-click `bbs-client.exe`
- Linux/macOS: `./bbs-client` (or `./bbs-client ui`)

## CLI usage (commands)

### `bbs-node`

- Generate a key pair:
  ```bash
  ./bbs-node-linux-amd64 gen-key
  ```
- Create/register a board (updates local `boards.json` and stores BoardMeta in the DHT):
  ```bash
  ./bbs-node-linux-amd64 init-board --board-id bbs.general --title General --author-priv-key 'ed25519:...'
  ```
- Register an existing board (when you know its BoardMeta CID):
  ```bash
  ./bbs-node-linux-amd64 add-board --board-id bbs.general --board-meta-cid bafy...
  ```

### `bbs-client` (CLI mode)

```bash
./bbs-client boards
./bbs-client threads bbs.general
```

## TUI usage (interactive)

- Main menu: `Browse boards` / `Search posts` / `Keys` / `Blocked` / `Settings`
- Create board: `Browse boards` → `Create board`
- Add board: `Browse boards` → `Add board` (enter `Board ID` + `BoardMeta CID`)
- Settings highlights:
  - `Client / Backend` → `Backend role (managed)` (`client|indexer|archiver|full`)
  - `Flexible-IPFS` → `Use mDNS on LAN...`
  - `Flexible-IPFS` → `ipfs.endpoint override` (manual peer connection)

Note: `Search posts` requires backend role `indexer` or `full`.

## LAN / 2-machine setup (peer connectivity)

Flexible‑IPFS needs at least 1 peer connection (see `dht/peerlist`). On a LAN, you can connect peers either by configuring `ipfs.endpoint` manually, or by using mDNS.

1. Start one node as `indexer` or `full` on Machine A.
2. On Machine A, get the PeerID:
   - `curl -X POST http://127.0.0.1:5001/api/v0/id` (look for `ID`)
   - or open `flexible-ipfs-base/.ipfs/config` and read `Identity.PeerID`
3. On Machine B, set the gw endpoint to Machine A (format: `/ip4/<A_LAN_IP>/tcp/4001/ipfs/<PeerID>`):
   - Env: `FLEXIPFS_GW_ENDPOINT=...`
   - TUI: Settings → Flexible‑IPFS → `ipfs.endpoint override`
   - CLI: `bbs-node --flexipfs-gw-endpoint ...`
4. (Optional) mDNS:
   - Advertiser: run with `--flexipfs-mdns=true` and also set `--flexipfs-gw-endpoint ...`
   - Discoverer: run with `--flexipfs-mdns=true` and leave the gw endpoint blank
5. Verify peers:
   - `curl -X POST http://127.0.0.1:5001/api/v0/dht/peerlist` should be non-empty (not `""`)

**Troubleshooting (`peerlist` stays `""`)**

- If port 4001 is reachable but `peerlist` is still empty, check `flexible-ipfs-base/.ipfs/config` → `"Bootstrap"`.
  - On first run, `.ipfs/config` can be generated from the bundled default `kadrtt.properties` endpoint (e.g. `/ip4/10.202...`). Changing `ipfs.endpoint override` later may not update `"Bootstrap"` automatically.
  - Fix: update `"Bootstrap"` to your gw endpoint (`/ip4/<A_LAN_IP>/tcp/4001/ipfs/<PeerID>`) or delete `flexible-ipfs-base/.ipfs/config` and restart.

## Windows 2-PC TUI guide (mDNS, FULL/CLIENT, create board)

Flexible‑IPFS needs at least 1 peer connection. If `dht/peerlist` returns `""`, flows like `Create board` will fail.

**Assumptions**

- PC-A runs `full`, PC-B runs `client`
- Same LAN
- Firewall allows UDP 5353 (mDNS) and TCP 4001 (Flex‑IPFS swarm)

**Steps**

1. On both PCs, extract the Windows bundle and launch `bbs-client.exe`.
2. On PC-A: `Settings` → `Client / Backend` → set `Backend role (managed)` to `full`.
3. On PC-A: compute your endpoint and set it in `Settings` → `Flexible-IPFS`:
   - Enable `Use mDNS...`
   - Set `ipfs.endpoint override` to your own endpoint (`/ip4/<A_LAN_IP>/tcp/4001/ipfs/<PeerID>`)
   - Get `<PeerID>` (PowerShell):
     ```powershell
     (curl.exe -X POST http://127.0.0.1:5001/api/v0/id | ConvertFrom-Json).ID
     ```
4. On PC-B: `Settings` → `Flexible-IPFS`:
   - Enable `Use mDNS...`
   - Leave `ipfs.endpoint override` empty
5. Create and share a board:
   - PC-B: `Browse boards` → `Create board` → note `boardMetaCid=...`
   - PC-A: `Browse boards` → `Add board` → input `Board ID` + `BoardMeta CID`

Note: boards are registered locally (`boards.json`), so sharing requires `Add board` on the other machine.

## Docker 2-node integration test (CI)

Starts 2 containers (`full` + `client`), connects Flex‑IPFS peers (`peerlist`), creates a board, and verifies the board can be loaded from the other node.

- Compose: `docker/compose/two-nodes.yml`
- Script: `scripts/ci/docker-two-node-test.sh`
- GitHub Actions: `.github/workflows/docker-two-node-test.yml`

Run locally:

```bash
# Compose v2:
docker compose -f docker/compose/two-nodes.yml up -d --build
# (or Compose v1):
docker-compose -f docker/compose/two-nodes.yml up -d --build
bash scripts/ci/docker-two-node-test.sh
```

## Build from source (WSL / Ubuntu)

### Prerequisites

- Go 1.22+
- (Optional) .NET 8 SDK for the C# client
- No Java install required – the bundled runtime is used.

Install Go on WSL (example):

```bash
sudo apt update
sudo apt install -y golang-go
go version
```

If `apt` provides an older Go, install 1.22+ from https://go.dev/dl/ or use a version manager (e.g. asdf).

Optional .NET 8:

```bash
sudo apt install -y dotnet-sdk-8.0
dotnet --version
```

### Build

```bash
cd backend-go
go build ./cmd/bbs-node
```

The binary is created at `backend-go/bbs-node`.

Build the C# client:

```bash
dotnet build src/BbsClient/BbsClient.csproj -c Release
```

### Run

If you run from the repo root, `bbs-node` will also autostart Flexible‑IPFS (same as the bundle).
Start the local build:

```bash
./backend-go/bbs-node --role=client --http 127.0.0.1:8080
```

Run the client UI:

```bash
dotnet run --project src/BbsClient -- ui
```

Or use the CLI:

```bash
dotnet run --project src/BbsClient -- boards
```

## Notes

- On first run, `flexible-ipfs-base/run.sh` and `run.bat` auto‑create `providers/`, `getdata/`, and `attr`.
- `flexipfs-base-url` is the local **HTTP API** (default: `http://127.0.0.1:5001/api/v0`). This is **not** the same as `ipfs.endpoint`.
- `ipfs.endpoint` (aka "gw endpoint") is a **libp2p multiaddr** used for peer connectivity/bootstrapping (format: `/ip4/<ip>/tcp/4001/ipfs/<PeerID>`).
- Flexible‑IPFS needs at least 1 peer connection for `putvaluewithattr`. If `curl -X POST http://127.0.0.1:5001/api/v0/dht/peerlist` returns `""`, flows like `Create board` will fail until peers are connected.
- You can set `ipfs.endpoint` either by editing `flexible-ipfs-base/kadrtt.properties`, or by overriding it on autostart via `FLEXIPFS_GW_ENDPOINT` (or `bbs-node --flexipfs-gw-endpoint ...`).
- On LANs, enable mDNS discovery via `bbs-node --flexipfs-mdns=true`. To advertise an endpoint on the LAN, also set `--flexipfs-gw-endpoint ...` (or `FLEXIPFS_GW_ENDPOINT`).
- mDNS requires UDP multicast (typically port 5353) and the Flex‑IPFS swarm port (typically TCP 4001) to be allowed by your firewall.
- Logs are written under `<data-dir>/logs/` (e.g. `bbs-client.log`, `bbs-node.log`, `flex-ipfs.log`).
- The HTTP API contract is in `docs/openapi.yaml` and C# DTOs can be regenerated via `scripts/generate-bbsclient-models.sh`.
- The Go backend exposes the BBS HTTP API under `/api/v1` (see `docs/flexible_ipfs_bbs_仕様書.md` for semantics).
