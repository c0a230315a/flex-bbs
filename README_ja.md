# flex-bbs

**Flexible‑IPFS + Go + C# クライアント**で動く分散掲示板の実験実装です。

現時点のリポジトリ内容:

- `flexible-ipfs-base/` – Flexible‑IPFS の jar と起動スクリプト。
- `flexible-ipfs-runtime/` – OS 別に同梱された Java 17 ランタイム（`linux-x64`, `win-x64`, `osx-x64`）。
- `backend-go/` – Go 製バックエンドノード `bbs-node`（`/api/v1` に HTTP API）。
- `src/BbsClient/` – C# クライアント（CLI + 対話 UI(TUI)）（`dotnet run --project src/BbsClient`）。

## コンパイル版（配布バンドル）の動かし方（1回のダウンロード）

GitHub Actions で OS 別の「全部入りバンドル」を作成します。内容:

- `bbs-node` バイナリ
- `bbs-client` バイナリ
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

### 板の作成（初回のみ）

鍵を生成:

```bash
./bbs-node-linux-amd64 gen-key
```

板を作成・登録（デフォルトでは OS の設定ディレクトリ配下に `boards.json` を作成/更新します）:

```bash
./bbs-node-linux-amd64 init-board --board-id bbs.general --title General --author-priv-key 'ed25519:...'
```

### クライアント

対話 UI(TUI):

```bash
./bbs-client

# （任意）明示的にコマンドを指定:
./bbs-client ui
```

クライアントはデフォルトでバックエンドを自動起動します。無効化する場合は `--no-start-backend` または TUI の Settings から変更してください。
また、TUI の Settings からバックエンドおよび Flexible-IPFS の設定（`flexible-ipfs-base/kadrtt.properties` 含む）を編集できます。
Windows では `bbs-client.exe` をダブルクリックすると TUI が起動します。

ソースから実行する場合は `--bbs-node-path ./backend-go/bbs-node` を指定するか、別で `bbs-node` を起動してください。

UI で複数行テキストを入力する場合は、1 行だけの `.` を入力すると確定します。

CLI 例:

```bash
./bbs-client boards
./bbs-client threads bbs.general
```

## ビルド版（git clone したソース）での環境構築と動かし方（WSL）

### 前提

- Go 1.22 以上
- （任意）C# クライアント用に .NET 8 SDK
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

クライアント UI:

```bash
dotnet run --project src/BbsClient -- ui
```

CLI:

```bash
dotnet run --project src/BbsClient -- boards
```

### C# クライアントのビルド

```bash
dotnet build src/BbsClient/BbsClient.csproj -c Release
```

## 補足

- 初回起動時に必要な `providers/`, `getdata/`, `attr` は `run.sh` / `run.bat` が自動生成します。
- `kadrtt.properties` の `ipfs.endpoint` を毎回手で編集せずに済むよう、起動時に `FLEXIPFS_GW_ENDPOINT`（または `bbs-node --flexipfs-gw-endpoint ...`）で上書きできます。
- Go バックエンドは `/api/v1` で BBS API を提供します（動作仕様は `docs/flexible_ipfs_bbs_仕様書.md` を参照）。
