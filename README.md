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

When running from source, pass `--bbs-node-path ./backend-go/bbs-node` (or start `bbs-node` yourself).

When entering multi-line text in the UI, finish with a single `.` line.

CLI examples:

```bash
./bbs-client boards
./bbs-client threads bbs.general
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
- To avoid editing `flexible-ipfs-base/kadrtt.properties` for every environment, override `ipfs.endpoint` via `FLEXIPFS_GW_ENDPOINT` (or `bbs-node --flexipfs-gw-endpoint ...`) when starting Flexible‑IPFS.
- The Go backend exposes the BBS HTTP API under `/api/v1` (see `docs/flexible_ipfs_bbs_仕様書.md` for semantics).
