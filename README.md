# flex-bbs

Experimental decentralized BBS on top of **Flexible‑IPFS** + **Go** + **C# client**.

This repo currently contains:

- `flexible-ipfs-base/` – prebuilt Flexible‑IPFS jars + run scripts.
- `flexible-ipfs-runtime/` – bundled Java 17 runtimes per OS (`linux-x64`, `win-x64`, `osx-x64`).
- `backend-go/` – Go backend node `bbs-node` (HTTP API under `/api/v1`).
- `src/BbsClient/` – C# client (CLI + interactive TUI) (`dotnet run --project src/BbsClient`).

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
3. Run `bbs-node` (it autostarts Flexible‑IPFS by default):
   - Linux / macOS:
     ```bash
     ./bbs-node-linux-amd64 --role=client --http 127.0.0.1:8080
     ```
   - Windows:
     ```bat
     bbs-node-windows-amd64.exe --role=client --http 127.0.0.1:8080
     ```
   If you want to start Flexible‑IPFS yourself, add `--autostart-flexipfs=false`.
4. Sanity check:
   ```bash
   curl http://127.0.0.1:8080/healthz
   ```

### Initialize a board (first time)

Generate a key pair:

```bash
./bbs-node-linux-amd64 gen-key
```

Then create/register a board (writes `boards.json` under your OS config dir by default):

```bash
./bbs-node-linux-amd64 init-board --board-id bbs.general --title General --author-priv-key 'ed25519:...'
```

You can also do this from the client TUI: `Browse boards` → `Create board`.

### Use the client

Interactive UI (TUI):

```bash
./bbs-client

# (optional) explicit command:
./bbs-client ui
```

The client auto-starts the backend by default; disable with `--no-start-backend` or via the TUI Settings menu.
The TUI Settings menu can also edit backend and Flexible-IPFS settings (including `flexible-ipfs-base/kadrtt.properties`).
On Windows, double-click `bbs-client.exe` to open the TUI.

Note: `Search posts` requires `bbs-node` role `indexer` or `full` (TUI: Settings → Client / Backend → Backend role).

When running from source, pass `--bbs-node-path ./backend-go/bbs-node` (or start `bbs-node` yourself).

When entering multi-line text in the UI, finish with a single `.` line.

CLI examples:

```bash
./bbs-client boards
./bbs-client threads bbs.general
```

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
