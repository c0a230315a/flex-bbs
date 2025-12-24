# flex-bbs

**Flexible‑IPFS + Go + C# クライアント**で動く分散掲示板の実験実装です。

現時点のリポジトリ内容:

- `flexible-ipfs-base/` – Flexible‑IPFS の jar と起動スクリプト。
- `flexible-ipfs-runtime/` – OS 別に同梱された Java 17 ランタイム（`linux-x64`, `win-x64`, `osx-x64`）。
- `backend-go/` – Go 製バックエンドノード `bbs-node`（`/api/v1` に HTTP API）。
- `src/BbsClient/` – C# クライアント（CLI + 対話 UI(TUI)）（`dotnet run --project src/BbsClient`）。

## 目次

- クイックスタート（配布バンドル）
  - コマンドで起動（CLI）
  - TUI で起動（bbs-client）
- コマンド操作（CLI）
- TUI 操作（bbs-client ui）
- Windows 2台（mDNS）TUI スタートアップガイド（FULL/CLIENT + ボード作成）
- Docker 2ノード疎通テスト（CI/CD）
- ソースからビルド（WSL / Ubuntu）

## クイックスタート（配布バンドル）

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
3. 起動方法は 2 通りあります（CLI / TUI）。以下で分けて説明します。

### 1) コマンドで起動（CLI）

`bbs-node` はデフォルトで Flexible‑IPFS を自動起動します（`--autostart-flexipfs=false` で無効化）。

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

### 2) TUI で起動（bbs-client）

`bbs-client` はデフォルトでバックエンド（ローカル `bbs-node`）を自動起動して管理します。

- 起動:
  - Windows: `bbs-client.exe` をダブルクリック
  - Linux/macOS: `./bbs-client`（または `./bbs-client ui`）

**文字化け対策（UTF-8）**

TUI は UTF-8 前提で、非 ASCII（日本語など）の表示に対応しています。文字化けする場合は以下を確認してください:

- Windows: Windows Terminal / PowerShell 推奨。`cmd.exe` の場合は起動前に `chcp 65001` を実行してください。
- Linux/macOS: ロケールが UTF-8 になっているか確認してください（例: `echo $LANG` に `UTF-8` が含まれる）。

## コマンド操作（CLI）

### bbs-node（バックエンド）

- 鍵生成:
  ```bash
  ./bbs-node-linux-amd64 gen-key
  ```
- 板の作成（ローカル `boards.json` に登録 + BoardMeta を DHT に保存）:
  ```bash
  ./bbs-node-linux-amd64 init-board --board-id bbs.general --title General --author-priv-key 'ed25519:...'
  ```
- 既存の板を追加（BoardMeta CID を知っている場合）:
  ```bash
  ./bbs-node-linux-amd64 add-board --board-id bbs.general --board-meta-cid bafy...
  ```

### bbs-client（CLI モード）

```bash
./bbs-client boards
./bbs-client threads bbs.general
```

## TUI 操作（bbs-client ui）

- 起動: `bbs-client`（Windows は `bbs-client.exe`）
- メインメニュー:
  - `Browse boards`（板一覧/作成/追加）
  - `Keys`（投稿・板作成に使う鍵）
  - `Settings`（バックエンド role / Flexible‑IPFS 設定）
- 板の作成: `Browse boards` → `Create board`
  - 初回は `Keys` メニューで鍵生成（または作成フロー中に生成）します
  - 作成成功すると `boardMetaCid=...` が表示されます
- 板の追加: `Browse boards` → `Add board` → `Board ID` と `BoardMeta CID` を入力
- 重要な設定（Settings）:
  - `Client / Backend` → `Backend role (managed)` を `client|indexer|archiver|full` から選択
  - `Language` → `UI language`（`auto|en|ja`）
  - `Flexible-IPFS` → `Use mDNS on LAN...`（mDNS）
  - `Flexible-IPFS` → `ipfs.endpoint override`（手動でピア接続したい場合）

注意: `Search posts` は `bbs-node` role が `indexer` または `full` の場合のみ利用できます（TUI: `Settings` → `Client / Backend`）。

### Search posts の使い方

`Search posts` は `bbs-node` のローカル index DB（`indexer` / `full` role が維持）を検索します。

- `q`（必須）: フリーテキスト検索
- `Board ID`（任意）: `bbs.general` など boardId で絞り込み
- `Author pubKey`（任意）: `ed25519:...` など author で絞り込み
- `Since` / `Until`（任意）: RFC3339（例: `2025-12-24T12:00:00Z`）
- ページ送り: `Prev page` / `Next page`
- 結果から `Open thread` で該当スレッドを開きます

## Windows 2台（mDNS）TUI スタートアップガイド（FULL / CLIENT + ボード作成）

Flexible‑IPFS は `putvaluewithattr` のために **最低 1 つのピア接続**が必要です。ピア未接続（`dht/peerlist` が `""`）だと `Create board` が失敗します。

この手順では、mDNS で「接続先（gw endpoint）」を LAN に広告し、もう片方が自動で発見して接続します。

### 想定

- PC-A: `full`（インデックス等を持つ側）
- PC-B: `client`（普段操作する側）
- 2台は同じ LAN（同一セグメント推奨）
- ファイアウォール許可:
  - UDP 5353（mDNS）
  - TCP 4001（Flex‑IPFS swarm）

### 手順

#### 1) 両 PC で bbs-client を起動

- PC-A/PC-B 共通: `bbs-client.exe` を起動

#### 2) PC-A（FULL）の設定

1. `Settings` → `Client / Backend`
   - `Backend role (managed)` を `full`
2. `Settings` → `Flexible-IPFS`
   - `Use mDNS on LAN to discover flex-ipfs gw endpoint?` を `true`
   - ここで `ipfs.endpoint override` に **PC-A 自身の endpoint** を設定して「広告側」にします

PC-A の endpoint（例: `/ip4/<AのLAN IP>/tcp/4001/ipfs/<PeerID>`）は、PowerShell で取得できます:

```powershell
# PeerID を取得
$peer = (curl.exe -X POST http://127.0.0.1:5001/api/v0/id | ConvertFrom-Json).ID
# A の LAN IP を自分で確認して入れる（例: 192.168.0.10）
$ip = "192.168.0.10"
"/ip4/$ip/tcp/4001/ipfs/$peer"
```

出てきた文字列を TUI の `ipfs.endpoint override` に貼り付けて保存します（保存後、バックエンドは自動再起動されます）。

#### 3) PC-B（CLIENT）の設定

1. `Settings` → `Client / Backend`
   - `Backend role (managed)` を `client`
2. `Settings` → `Flexible-IPFS`
   - `Use mDNS...` を `true`
   - `ipfs.endpoint override` は **空（none）** のまま

これで PC-B 側が mDNS で PC-A の endpoint を発見し、Flex‑IPFS がピア接続します。

（疎通確認したい場合）

```powershell
curl.exe -X POST http://127.0.0.1:5001/api/v0/dht/peerlist
```

`""` 以外になれば OK です。

#### Troubleshooting（`peerlist` がずっと `""` のまま）

- `Test-NetConnection <AのIP> -Port 4001` が通るのに `peerlist` が空の場合、`flexible-ipfs-base/.ipfs/config` の `"Bootstrap"` が **古いまま**の可能性があります。
  - 初回起動時に `kadrtt.properties` のデフォルト値（例: `/ip4/10.202...`）で `.ipfs/config` が生成され、その後 `ipfs.endpoint override` を変えても `"Bootstrap"` が自動更新されないケースがあります。
  - 対処: `"Bootstrap"` に A の endpoint（`/ip4/<AのLAN IP>/tcp/4001/ipfs/<PeerID>`）を入れる（または `flexible-ipfs-base/.ipfs/config` を削除して再起動）→ `bbs-client`/`bbs-node` を再起動。

- `flex-ipfs.log` に `Database may be already in use: .../.ipfs/datastore/h2.datastore.mv.db` が出る場合、同じ `.ipfs` ディレクトリを複数の Flexible‑IPFS プロセスが同時に使っている状態です。
  - 対処: 余分な `bbs-node`/`java` を停止して再起動（または `flexible-ipfs-base/.ipfs` を削除して初期化）。

#### 4) ボード作成 → 共有（Add board）

1. PC-B: `Browse boards` → `Create board`
   - `Board ID` と `Title` を入力して作成
   - 成功すると `boardMetaCid=...` が表示されます
2. PC-A: `Browse boards` → `Add board`
   - PC-B で作った `Board ID` と `BoardMeta CID` を入力して登録

これで PC-A（full）側でもボードを開けるようになります。

補足: `boards.json` は各 PC のローカル管理なので、ボードを共有したい場合は相手側で `Add board` が必要です。

## LAN / 2台構成（ピア接続: CLI 手動設定）

LAN 上では `ipfs.endpoint` を手動設定するか、上記のように mDNS で配布してピアを接続します。

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

## Docker 2ノード疎通テスト（CI/CD）

`FULL` と `CLIENT` の 2 ノードを Docker で立ち上げ、ピア接続（`peerlist`）とボード作成までを自動で確認します。

- Compose: `docker/compose/two-nodes.yml`
- テストスクリプト: `scripts/ci/docker-two-node-test.sh`
- GitHub Actions: `.github/workflows/docker-two-node-test.yml`

ローカルで実行する場合:

```bash
# Compose v2:
docker compose -f docker/compose/two-nodes.yml up -d --build
# (or Compose v1):
docker-compose -f docker/compose/two-nodes.yml up -d --build
bash scripts/ci/docker-two-node-test.sh
```

## ソースからビルド（WSL / Ubuntu）

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
