# Flexible-IPFS + Go + C# 分散掲示板 仕様書 v0.1

## 0. 前提・ゴール

- **目的**  
  Flexible-IPFS 上に JSON オブジェクトとして掲示板データを保存し、中央サーバー・管理者なしで動く分散掲示板を実験的に実装する。

- **構成技術**
  - ストレージ／DHT: Flexible-IPFS（Java 製、HTTP API）
  - アプリケーションロジック: Go
  - UI・クライアント・クロスプラットフォームバイナリ: C# (.NET)

- **非機能要求**
  - Windows / macOS / Linux で動作
  - 掲示板データはすべて Flexible-IPFS 上に保存
  - 任意のノードが「インフラノード（インデクサー／アーカイバ）」になれる
  - 投稿は ed25519 署名必須。署名検証できない投稿は無視
  - クライアントは任意の公開鍵をブロック可能（ローカル設定）

---

## 1. 全体アーキテクチャ

### 1.1 コンポーネント

1. **Flexible-IPFS ノード (Java)**  
   - `run.sh` / `run.bat` で起動し、`http://127.0.0.1:5001/api/v0` に HTTP API を提供
   - DHT / MerkleDAG / 属性・タグ検索などを担当

2. **BBS ノード (Go バックエンド)**  
   役割:
   - 掲示板データ構造（Board / Thread / Post / BoardLogEntry）を管理
   - JSON オブジェクトを Flexible-IPFS へ PUT/GET
   - 署名生成・検証（ed25519）
   - 自身のモードに応じて HTTP API / インデックス更新 / ピン操作を行う

   実行モード（CLI 引数）:
   - `--role=client` : ローカルクライアント用バックエンド
   - `--role=indexer` : インデクサーノード（ローカル DB で検索 API 提供）
   - `--role=archiver` : アーカイバーノード（掲示板データを pin）
   - `--role=full` : indexer + archiver

3. **クライアント UI (C#)**  
   役割:
   - ユーザー操作（板一覧、スレ一覧、投稿／編集／削除）
   - Go BBS ノードの HTTP API を叩く
   - ユーザー鍵の生成・保存・切り替え
   - 公開鍵ブロックリストの管理

   実装案:
   - .NET 8 + クロスプラットフォーム UI フレームワーク（例: Avalonia）
   - 単一 EXE or AppBundle に Go バックエンドを同梱し、起動時にサブプロセスとして起動

---

## 2. Flexible-IPFS とのインタフェース

### 2.1 使用する HTTP API

Flexible-IPFS の HTTP API を以下のように使用する:

- 値の保存（文字列 or ファイル）
  - `POST /api/v0/dht/putvaluewithattr?value=...&attrs=...&tags=...`
  - `POST /api/v0/dht/putvaluewithattr?file=...&attrs=...&tags=...`
  - 戻り値例:
    ```json
    {"CID_file":"<cid>","Addr0":"<peerId>: [...]"}
    ```

- 値の取得
  - `POST /api/v0/dht/getvalue?cid=<cid>`

- 属性／タグ検索
  - `POST /api/v0/dht/getbyattrs?attrs=...&tags=...&showall=true`

- 属性名一覧・タグ名一覧
  - `POST /api/v0/dht/listattrs`
  - `POST /api/v0/dht/listtags`

- ピア一覧
  - `POST /api/v0/dht/peerlist`

BBS ノード（Go）はこれらをラップする `FlexibleIPFSClient` を実装する。

### 2.2 BBS オブジェクトの保存方針

すべての BBS オブジェクト（BoardMeta / ThreadMeta / Post / BoardLogEntry）は:

1. JSON にシリアライズ
2. `putvaluewithattr?value=<urlencoded JSON>` で保存
3. `attrs` / `tags` にはタイプ情報・boardId 等を埋め込む

例:

- BoardMeta:
  - `attrs=objtype_boardmeta_version_1`
  - `tags=board_<boardId>`
- ThreadMeta:
  - `attrs=objtype_threadmeta_version_1`
  - `tags=board_<boardId>-thread_<threadId>`
- BoardLogEntry:
  - `attrs=objtype_boardlogentry_version_1`
  - `tags=board_<boardId>-thread_<threadId>`

取得時:

1. `getbyattrs` で `cid` の一覧を取得
2. 必要な `cid` に対して `getvalue` を呼び JSON を復元

---

## 3. データモデル

### 3.1 共通

- すべて JSON + CID
- 用語:
  - `CID` : Flexible-IPFS が返す CID 文字列
  - `boardId` : 文字列 ID（例: `"bbs.general"`）
  - `threadId` : ThreadMeta の CID（文字列）

#### 3.1.1 署名ペイロード生成ルール（共通）

各オブジェクトタイプごとに **署名対象フィールド** を固定順序で並べたテキストを作る:

```text
type=<type>
version=<version>
...（事前に決めたフィールドを key=value 形式で）...
```

- 改行区切り
- 未使用フィールドは空文字 or "null" に統一
- `signature` フィールドは署名対象に含めない

このテキストに対して ed25519 で署名／検証を行う。

---

### 3.2 Post

1 Post = 1 JSON = 1 CID。

```json
{
  "version": 1,
  "type": "post",
  "postCid": null,
  "threadId": "baf...threadMetaCid",
  "parentPostCid": null,
  "authorPubKey": "ed25519:xxxx",
  "displayName": "conecone",
  "body": {
    "format": "markdown",
    "content": "こんにちは、テスト投稿です。"
  },
  "attachments": [
    { "cid": "baf...img", "mime": "image/png" }
  ],
  "createdAt": "2025-11-28T08:30:00Z",
  "editedAt": null,
  "meta": {
    "tags": ["test"],
    "client": "bbs-csharp/0.1.0"
  },
  "signature": "base64(ed25519-signature)"
}
```

署名対象フィールド:

```text
type=post
version=1
threadId=<threadId>
parentPostCid=<parentPostCid or "">
authorPubKey=<authorPubKey>
displayName=<displayName>
body.format=<body.format>
body.content=<body.content>
createdAt=<createdAt>
```

`attachments`・`meta` は署名対象外。

---

### 3.3 ThreadMeta

```json
{
  "version": 1,
  "type": "threadMeta",
  "threadId": "baf...thisThreadMetaCid",
  "boardId": "bbs.general",
  "title": "はじめてのスレッド",
  "rootPostCid": "baf...rootPostCid",
  "createdAt": "2025-11-28T08:10:00Z",
  "createdBy": "ed25519:xxxx",
  "meta": {
    "tags": ["intro"]
  },
  "signature": "..."
}
```

`threadId` は、この JSON を Flexible-IPFS に保存したときの CID。

---

### 3.4 BoardMeta

```json
{
  "version": 1,
  "type": "boardMeta",
  "boardId": "bbs.general",
  "title": "雑談板",
  "description": "実験用の雑談板",
  "logHeadCid": "baf...latestBoardLogEntryCid",
  "createdAt": "2025-11-28T08:00:00Z",
  "createdBy": "ed25519:xxxx",
  "signature": "..."
}
```

- `logHeadCid` : この板の BoardLogEntry チェーンの「先頭（最新）」の CID。
- ログは片方向リスト (`latest` → `prevLogCid` → ...)。

---

### 3.5 BoardLogEntry

ThreadLog は **BoardLogEntry のフィルタビュー**として表現する。

```json
{
  "version": 1,
  "type": "boardLogEntry",
  "boardId": "bbs.general",
  "op": "addPost",
  "threadId": "baf...threadMetaCid",
  "postCid": "baf...post",
  "oldPostCid": null,
  "newPostCid": null,
  "targetPostCid": null,
  "reason": null,
  "createdAt": "2025-11-28T08:40:00Z",
  "authorPubKey": "ed25519:xxxx",
  "prevLogCid": "baf...prevBoardLogEntry",
  "signature": "..."
}
```

- `op="createThread"`
  - `threadId` = ThreadMeta CID
  - `postCid`  = ルート Post CID
- `op="addPost"`
  - `threadId` = 対象スレ
  - `postCid`  = 新規 Post CID
- `op="editPost"`
  - `oldPostCid` / `newPostCid` の両方をセット
  - `authorPubKey` が `oldPost` の author と一致しない場合、クライアント側で無効
- `op="tombstonePost"`
  - `targetPostCid` = 削除対象 Post の CID
  - `reason` は任意
  - `authorPubKey` が元投稿の author と一致しない tombstone は無視

署名対象フィールド:

```text
type=boardLogEntry
version=1
boardId=<boardId>
op=<op>
threadId=<threadId>
postCid=<postCid or "">
oldPostCid=<oldPostCid or "">
newPostCid=<newPostCid or "">
targetPostCid=<targetPostCid or "">
reason=<reason or "">
createdAt=<createdAt>
authorPubKey=<authorPubKey>
prevLogCid=<prevLogCid or "">
```

---

## 4. 操作フロー

### 4.1 板の作成

1. クライアントが新しい鍵ペア or 既存鍵を選択。
2. `BoardMeta` JSON を生成（`logHeadCid = null`）。
3. BoardMeta を `putvaluewithattr` で保存（attrs: `objtype_boardmeta_version_1`, tags: `board_<boardId>`）。
4. 得られた CID を別途記録（boardId と関連付け）。

板一覧の発見方法は、v0.1 では外部配布（設定ファイル・Web ページ等）でよい。

### 4.2 スレッドの作成

1. ThreadMeta を生成し保存 → `threadId` = ThreadMeta CID。
2. ルート Post を作成・署名（`threadId` をセット）し保存 → `rootPostCid` を得る。
3. BoardLogEntry（`op="createThread"`）を生成:
   - `threadId` = ThreadMeta CID
   - `postCid`  = rootPostCid
   - `prevLogCid` = 現在の BoardMeta.logHeadCid
4. BoardLogEntry を保存 → `newLogCid` を得る。
5. BoardMeta の `logHeadCid` を `newLogCid` に更新し、再保存。

### 4.3 投稿の追加

1. Post JSON を生成・署名。
2. Flexible-IPFS に保存 → `postCid`。
3. BoardLogEntry（`op="addPost"`）を生成:
   - `threadId` = 対象 threadId
   - `postCid`  = postCid
   - `prevLogCid` = 現在の logHeadCid
4. 保存 → 新しい logHeadCid。

### 4.4 編集・削除

- **編集 (`op="editPost"`)**
  1. 新しい Post オブジェクトを作り、`editedAt` をセットして保存 → `newPostCid`。
  2. BoardLogEntry を生成:
     - `oldPostCid` = 旧 CID
     - `newPostCid` = 新 CID

- **削除 (`op="tombstonePost"`)**
  1. BoardLogEntry (`op="tombstonePost"`) を作成:
     - `targetPostCid` = 削除対象の CID
     - `reason` = 任意
  2. クライアントは ThreadLog をリプレイする際に tombstone を解釈し、該当 Post を「削除済み」として表示。

---

## 5. 署名・鍵管理

### 5.1 鍵形式

- 鍵ペア: ed25519
- 公開鍵表現: `"ed25519:<hex or base64>"`

### 5.2 クライアント側管理

- OSごとのユーザー設定ディレクトリに `keys.json` などを保存:
  - `[{ "name": "default", "pub": "...", "priv": "..." }, ...]`
- UI:
  - キーペアの生成／削除
  - 投稿時に使用するキーペアの選択
- 鍵のエクスポート／インポート（JSON）

### 5.3 ブロックリスト

- ローカルに `blockedPubKeys: string[]` を保存。
- 表示ロジック:
  - 署名検証 NG → 常に非表示
  - `authorPubKey` が `blockedPubKeys` に含まれる → 投稿・ログを UI で隠す or 折りたたむ

---

## 6. Go バックエンド API（BBS ノード）

### 6.1 外部公開 API（例）

`/api/v1` プレフィクスで REST:

- `GET /api/v1/boards`  
  既知の BoardMeta の一覧
- `GET /api/v1/boards/{boardId}`  
  BoardMeta 詳細
- `GET /api/v1/boards/{boardId}/threads`  
  スレ一覧（ページング）
- `GET /api/v1/threads/{threadId}`  
  ThreadMeta + ThreadLog リプレイ済みの Post 一覧
- `POST /api/v1/threads`  
  新規スレ作成（板 ID + タイトル + 本文 + 鍵指定）
- `POST /api/v1/posts`  
  既存スレへの新規投稿
- `POST /api/v1/posts/{postCid}/edit`  
  編集
- `POST /api/v1/posts/{postCid}/tombstone`  
  削除トンブストーン

Go 側はこれらのエンドポイントから Flexible-IPFS クライアントを呼ぶ。

### 6.2 インデクサーモード

- ローカル DB（SQLite など）に boards / threads / posts テーブルを持つ。
- BoardLogEntry を順番に再生して DB を更新。
- 検索 API 例:
  - `GET /api/v1/search/posts?q=&boardId=&author=&since=&until=`

インデクサーは任意の有志ノードであり、存在しなくてもプロトコル自体は動作する。

---

## 7. C# クライアント

### 7.1 機能

- BBS ノード HTTP API へのリクエスト
- 投稿一覧の表示・ThreadLog のリプレイ結果表示
- 鍵管理 UI
- 公開鍵ブロック UI
- Flexible-IPFS / BBS ノードの状態モニタ（起動・停止ボタンなど）

### 7.2 Go バックエンドの起動

- C# アプリ起動時:
  1. Flexible-IPFS が動いていなければ（設定に応じて）起動 or 既存のものに接続
  2. Go BBS ノードをサブプロセスとして起動（ポート指定）
- 終了時:
  - BBS ノードプロセスを終了
  - Flexible-IPFS はユーザー設定に応じて終了 or 継続

---

## 8. ビルド・配布（方針）

- Flexible-IPFS:
  - Java 17 + ant で `ipfs-ncl.jar` をビルド、もしくは配布バイナリを利用。
- Go:
  - `GOOS=windows|linux|darwin` へのクロスコンパイル。
- C#:
  - .NET 8 self-contained / single-file publish で OS ごとのバイナリを作成。
- 同梱パッケージ:
  - Flexible-IPFS 実行に必要なファイル
  - Go BBS ノードバイナリ
  - C# UI 実行ファイル
