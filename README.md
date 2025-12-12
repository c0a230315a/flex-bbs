# flex-bbs

Experimental decentralized BBS on top of **Flexible‑IPFS** + **Go** + (future) **C# client**.

This repo currently contains:

- `flexible-ipfs-base/` – prebuilt Flexible‑IPFS jars + run scripts.
- `flexible-ipfs-runtime/` – bundled Java 17 runtimes per OS (`linux-x64`, `win-x64`, `osx-x64`).
- `backend-go/` – Go backend node `bbs-node` (currently a minimal health-check stub).
- `src/BbsClient/` – placeholder for the future C# UI.

## Prebuilt (compiled) binaries

GitHub Actions builds `bbs-node` for Linux / Windows / macOS and uploads them as artifacts.

1. Download the latest artifact from GitHub Actions (or Releases if you publish them):
   - `bbs-node-linux-amd64`
   - `bbs-node-windows-amd64.exe`
   - `bbs-node-darwin-amd64`
2. Keep `flexible-ipfs-base/` and `flexible-ipfs-runtime/` from this repo next to the binary.
3. Start Flexible‑IPFS:
   - Linux / macOS:
     ```bash
     cd flexible-ipfs-base
     ./run.sh
     ```
   - Windows:
     ```bat
     cd flexible-ipfs-base
     run.bat
     ```
   These scripts use the bundled JRE, so you don’t need to install Java separately.
4. In another terminal, start `bbs-node`:
   - Linux / macOS:
     ```bash
     ./bbs-node-linux-amd64 --role=client --http 127.0.0.1:8080
     ```
   - Windows:
     ```bat
     bbs-node-windows-amd64.exe --role=client --http 127.0.0.1:8080
     ```
5. Sanity check:
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

Same as the “Prebuilt binaries” section, but start the local build:

```bash
./backend-go/bbs-node --role=client --http 127.0.0.1:8080
```

## Notes

- On first run, `flexible-ipfs-base/run.sh` and `run.bat` auto‑create `providers/`, `getdata/`, and `attr`.
- The Go backend currently only exposes `/healthz`; BBS APIs are still TODO.

