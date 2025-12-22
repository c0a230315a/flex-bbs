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

クライアントの TUI からも実行できます: `Browse boards` → `Create board`。

### クライアント

対話 UI(TUI):

```bash
./bbs-client

# （任意）明示的にコマンドを指定:
./bbs-client ui
```

クライアントはデフォルトでバックエンドを自動起動します。無効化する場合は `--no-start-backend` または TUI の Settings から変更してください。
また、TUI の Settings からバックエンドおよび Flexible-IPFS の設定を編集できます:

- Settings → Flexible‑IPFS:
  - `Use mDNS...`（=`--flexipfs-mdns`）
  - `mDNS discovery timeout (seconds)`（=`--flexipfs-mdns-timeout`）
  - `ipfs.endpoint override`（=`--flexipfs-gw-endpoint` / `FLEXIPFS_GW_ENDPOINT`）
- Settings → kadrtt.properties: `flexible-ipfs-base/kadrtt.properties` を直接編集

設定の保存後、クライアントがバックエンドを管理している場合（`Auto-start backend` が有効）は自動で再起動します。
Windows では `bbs-client.exe` をダブルクリックすると TUI が起動します。

注意: `Search posts` は `bbs-node` の role が `indexer` または `full` の場合のみ利用できます（TUI: Settings → Client / Backend → Backend role）。

ソースから実行する場合は `--bbs-node-path ./backend-go/bbs-node` を指定するか、別で `bbs-node` を起動してください。

UI で複数行テキストを入力する場合は、1 行だけの `.` を入力すると確定します。

CLI 例:

```bash
./bbs-client boards
./bbs-client threads bbs.general
```

## LAN / 2台構成（ピア接続）

Flexible‑IPFS は `putvaluewithattr` のために **最低 1 つのピア接続**が必要です（`dht/peerlist` 参照）。LAN 上では `ipfs.endpoint` を手動設定するか、mDNS で配布してピアを接続します。

1. A 端末で `bbs-node` を `indexer` または `full` で起動します。
2. A 端末で PeerID を取得します:
   - `curl -X POST http://127.0.0.1:5001/api/v0/id`（`ID` を見る）
   - または `flexible-ipfs-base/.ipfs/config` の `Identity.PeerID`
3. B 端末で A 端末を指す gw endpoint を設定します（形式: `/ip4/<AのLAN IP>/tcp/4001/ipfs/<PeerID>`）:
   - 環境変数: `FLEXIPFS_GW_ENDPOINT=...`
   - TUI: Settings → Flexible‑IPFS → `ipfs.endpoint override`
   - CLI: `bbs-node --flexipfs-gw-endpoint ...`
4.（任意）mDNS:
   - 広告側: `--flexipfs-mdns=true` に加えて `--flexipfs-gw-endpoint ...`（または `FLEXIPFS_GW_ENDPOINT`）も指定
   - 探索側: `--flexipfs-mdns=true` を指定し、gw endpoint は空のまま
5. 接続確認:
   - `curl -X POST http://127.0.0.1:5001/api/v0/dht/peerlist` が `""` 以外になれば OK

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
- `flexipfs-base-url` は **HTTP API** です（デフォルト: `http://127.0.0.1:5001/api/v0`）。`ipfs.endpoint` とは別物です。
- `ipfs.endpoint`（gw endpoint）はピア接続/ブートストラップ用の **libp2p multiaddr** です（形式: `/ip4/<ip>/tcp/4001/ipfs/<PeerID>`）。
- `curl -X POST http://127.0.0.1:5001/api/v0/dht/peerlist` が `""` の場合、Flexible‑IPFS はピア未接続のため `Create board` 等が失敗します。
- `ipfs.endpoint` は `flexible-ipfs-base/kadrtt.properties` を編集するか、起動時に `FLEXIPFS_GW_ENDPOINT`（または `bbs-node --flexipfs-gw-endpoint ...`）で上書きできます。
- 学内 LAN などでは `bbs-node --flexipfs-mdns=true` により mDNS で gw endpoint を探索できます（LAN に広告するには `--flexipfs-gw-endpoint ...` または `FLEXIPFS_GW_ENDPOINT` も指定）。
- mDNS は UDP マルチキャスト（一般に 5353 番）と、Flex‑IPFS の swarm ポート（一般に TCP 4001 番）がファイアウォールで許可されている必要があります。
- ログは基本的に `<data-dir>/logs/` 配下に出力します（例: `bbs-client.log`, `bbs-node.log`, `flex-ipfs.log`）。
- HTTP API の契約は `docs/openapi.yaml` にあり、C# DTO は `scripts/generate-bbsclient-models.sh` で再生成できます。
- Go バックエンドは `/api/v1` で BBS API を提供します（動作仕様は `docs/flexible_ipfs_bbs_仕様書.md` を参照）。
