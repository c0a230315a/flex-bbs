# flex-bbs

Experimental decentralized BBS on top of **Flexible‑IPFS** + **Go** + (future) **C# client**.

This repo currently contains:

- `flexible-ipfs-base/` – prebuilt Flexible‑IPFS jars + run scripts.
- `flexible-ipfs-runtime/` – bundled Java 17 runtimes per OS (`linux-x64`, `win-x64`, `osx-x64`).
- `backend-go/` – Go backend node `bbs-node` (currently a minimal health-check stub).
- `src/BbsClient/` – placeholder for the future C# UI.

## Prebuilt bundle (one download)

GitHub Actions builds OS‑specific bundles that include everything needed:

- `bbs-node` binary
- `flexible-ipfs-base/` (jars + scripts)
- `flexible-ipfs-runtime/<os>/jre` (bundled Java 17 for that OS)

1. Download the latest artifact from GitHub Actions:
   - Linux: `flex-bbs-linux-amd64.tar.gz`
   - Windows: `flex-bbs-windows-amd64.zip`
   - macOS: `flex-bbs-darwin-amd64.tar.gz`
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

## Build from source (WSL / Ubuntu)

### Prerequisites

- Go 1.22+
- (Optional) .NET 8 SDK for the future C# client
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

### Run

If you run from the repo root, `bbs-node` will also autostart Flexible‑IPFS (same as the bundle).
Start the local build:

```bash
./backend-go/bbs-node --role=client --http 127.0.0.1:8080
```

## Notes

- On first run, `flexible-ipfs-base/run.sh` and `run.bat` auto‑create `providers/`, `getdata/`, and `attr`.
- The Go backend currently only exposes `/healthz`; BBS APIs are still TODO.
