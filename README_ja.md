# flex-bbs

**Flexible‑IPFS + Go + (将来の) C# クライアント**で動く分散掲示板の実験実装です。

現時点のリポジトリ内容:

- `flexible-ipfs-base/` – Flexible‑IPFS の jar と起動スクリプト。
- `flexible-ipfs-runtime/` – OS 別に同梱された Java 17 ランタイム（`linux-x64`, `win-x64`, `osx-x64`）。
- `backend-go/` – Go 製バックエンドノード `bbs-node`（現在はヘルスチェックのみの最小スタブ）。
- `src/BbsClient/` – 将来の C# UI 用の置き場（まだ未実装）。

## コンパイル版（配布バンドル）の動かし方（1回のダウンロード）

GitHub Actions で OS 別の「全部入りバンドル」を作成します。内容:

- `bbs-node` バイナリ
- `flexible-ipfs-base/`（jar と起動スクリプト）
- `flexible-ipfs-runtime/<os>/jre`（その OS 用の同梱 Java 17）

1. GitHub Actions から最新の artifact を取得:
   - Linux: `flex-bbs-linux-amd64.tar.gz`
   - Windows: `flex-bbs-windows-amd64.zip`
   - macOS: `flex-bbs-darwin-amd64.tar.gz`
   `main` から切られるタグは安定版、`develop` から切られるタグは pre‑release として扱われます。
2. 展開すると `bbs-node-*` と `flexible-ipfs-*` が同じフォルダに入っています。
3. `bbs-node` を起動します（デフォルトで Flexible‑IPFS を自動起動します）:
   - Linux / macOS:
     ```bash
     ./bbs-node-linux-amd64 --role=client --http 127.0.0.1:8080
     ```
   - Windows:
     ```bat
     bbs-node-windows-amd64.exe --role=client --http 127.0.0.1:8080
     ```
   Flexible‑IPFS を手動で起動したい場合は `--autostart-flexipfs=false` を付けてください。
4. 動作確認:
   ```bash
   curl http://127.0.0.1:8080/healthz
   ```

## ビルド版（git clone したソース）での環境構築と動かし方（WSL）

### 前提

- Go 1.22 以上
- （任意）将来の C# クライアント開発用に .NET 8 SDK
- Java は不要（同梱ランタイムを使用）。

WSL(Ubuntu) で Go を用意する例:

```bash
sudo apt update
sudo apt install -y golang-go
go version
```

`apt` の Go が古い場合は https://go.dev/dl/ から 1.22+ を入れるか、asdf などのバージョン管理を使ってください。

（任意）.NET 8:

```bash
sudo apt install -y dotnet-sdk-8.0
dotnet --version
```

### ビルド

```bash
cd backend-go
go build ./cmd/bbs-node
```

`backend-go/bbs-node` が生成されます。

### 起動

リポジトリ直下から起動する場合も、配布バンドルと同様に Flexible‑IPFS を自動起動します。
ローカルビルドした `bbs-node` を起動:

```bash
./backend-go/bbs-node --role=client --http 127.0.0.1:8080
```

## 補足

- 初回起動時に必要な `providers/`, `getdata/`, `attr` は `run.sh` / `run.bat` が自動生成します。
- Go バックエンドは現状 `/healthz` のみ実装済みで、BBS API は未実装です。
