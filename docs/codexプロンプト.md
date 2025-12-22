# Codex 用プロンプト

以下は、このプロジェクトの `docs/spec-bbs-flexible-ipfs.md` として保存し、GitHub Copilot / OpenAI Codex に渡すことを想定した指示文です。

```markdown
You are an AI coding assistant (Codex) helping to implement a **decentralized BBS** on top of **Flexible-IPFS** + **Go** + **C#**.

## High-level Goal

- Build a BBS system where:
  - All data (boards, threads, posts, logs) are stored as **JSON objects on Flexible-IPFS**.
  - There is **no central server** or admin; any node can be an infra node (indexer / archiver).
  - Posts and logs are **ed25519-signed**, and unsigned / invalid-signed data must be ignored.
  - The user-facing UI is written in **C# (.NET)** and talks to a **Go backend** (BBS node) via HTTP.
  - The Go backend talks to a local Flexible-IPFS node via its HTTP API.

The Flexible-IPFS project is here:
- https://github.com/ncl-teu/flexible-ipfs

We will treat Flexible-IPFS as:
- A content-addressable store + DHT with attribute / tag search,
- Exposed over HTTP at `http://127.0.0.1:5001/api/v0`.

Key endpoints (from README):

- PUT string/file with attributes & tags:
  - `POST /api/v0/dht/putvaluewithattr?value=...&attrs=...&tags=...`
  - `POST /api/v0/dht/putvaluewithattr?file=...&attrs=...&tags=...`
- GET value by CID:
  - `POST /api/v0/dht/getvalue?cid=<cid>`
- Search by attributes / tags:
  - `POST /api/v0/dht/getbyattrs?attrs=...&tags=...&showall=true`
- List attrs / tags:
  - `POST /api/v0/dht/listattrs`
  - `POST /api/v0/dht/listtags`
- Peer list:
  - `POST /api/v0/dht/peerlist`

## Data model (must be implemented exactly)

### Common

- All BBS objects are JSON and saved on Flexible-IPFS.
- Types:
  - `Post`
  - `ThreadMeta`
  - `BoardMeta`
  - `BoardLogEntry`

- Every object has:
  - `version`: integer
  - `type`: string (e.g. `"post"`, `"threadMeta"`, `"boardMeta"`, `"boardLogEntry"`)

### Post

One Post = one JSON object = one CID.

Fields:

- `version: number`
- `type: "post"`
- `postCid: string | null`        // optional, may store its own CID
- `threadId: string`              // CID of ThreadMeta
- `parentPostCid: string | null`  // for replies
- `authorPubKey: string`          // "ed25519:..."
- `displayName: string`
- `body: { format: string; content: string }`
- `attachments: { cid: string; mime: string }[]`
- `createdAt: string (ISO8601)`
- `editedAt: string | null`
- `meta: { [key: string]: any }`
- `signature: string`             // base64(ed25519-signature)

Signature payload (canonical text):

```text
type=post
version=<version>
threadId=<threadId>
parentPostCid=<parentPostCid or "">
authorPubKey=<authorPubKey>
displayName=<displayName>
body.format=<body.format>
body.content=<body.content>
createdAt=<createdAt>
```

`attachments` and `meta` are NOT covered by the signature.

### ThreadMeta

Fields:

- `version: 1`
- `type: "threadMeta"`
- `threadId: string`       // CID of this JSON itself
- `boardId: string`        // e.g. "bbs.general"
- `title: string`
- `rootPostCid: string`
- `createdAt: string`
- `createdBy: string`      // pubkey
- `meta: { [key: string]: any }`
- `signature: string`

### BoardMeta

Fields:

- `version: 1`
- `type: "boardMeta"`
- `boardId: string`         // e.g. "bbs.general"
- `title: string`
- `description: string`
- `logHeadCid: string | null`  // CID of latest BoardLogEntry
- `createdAt: string`
- `createdBy: string`
- `signature: string`

### BoardLogEntry

Logical "ThreadLog" is defined as "BoardLogEntry filtered by (boardId, threadId)".

Fields:

- `version: 1`
- `type: "boardLogEntry"`
- `boardId: string`
- `op: "createThread" | "addPost" | "editPost" | "tombstonePost"`
- `threadId: string`              // ThreadMeta CID
- `postCid: string | null`        // for createThread / addPost
- `oldPostCid: string | null`     // for editPost
- `newPostCid: string | null`     // for editPost
- `targetPostCid: string | null`  // for tombstonePost
- `reason: string | null`         // optional reason for tombstone
- `createdAt: string`
- `authorPubKey: string`
- `prevLogCid: string | null`     // previous BoardLogEntry CID
- `signature: string`

Signature payload:

```text
type=boardLogEntry
version=<version>
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

## Storage conventions on Flexible-IPFS

All BBS objects are stored via `putvaluewithattr?value=...`.

Use the following conventions:

- BoardMeta:
  - attrs: `boardmeta_1`
  - tags:  `board_<boardId>`
- ThreadMeta:
  - attrs: `threadmeta_1`
  - tags:  `board_<boardId>-thread_<threadId>`
- BoardLogEntry:
  - attrs: `boardlogentry_1`
  - tags:  `board_<boardId>-thread_<threadId>`

To retrieve:

- Use `getbyattrs` to list candidate CIDs,
- Then `getvalue?cid=...` to fetch the JSON body.

## BBS operations (semantics)

- Creating a board:
  - Create `BoardMeta` (logHeadCid = null).
  - Save it to Flexible-IPFS and record its CID externally (for now).
- Creating a thread:
  1. Create `ThreadMeta`, save, its CID is `threadId`.
  2. Create root `Post` (with `threadId`), save, get `rootPostCid`.
  3. Create `BoardLogEntry` with `op="createThread"`, `threadId`, `postCid = rootPostCid`, `prevLogCid = current logHeadCid`.
  4. Save BoardLogEntry, get new `logHeadCid`.
  5. Update BoardMeta.logHeadCid and save (CID changes).
- Adding a post:
  1. Create `Post`, save, get `postCid`.
  2. Create `BoardLogEntry` with `op="addPost"`, `threadId`, `postCid`, `prevLogCid`.
- Editing a post:
  1. Create new Post object with updated content and `editedAt`, save as `newPostCid`.
  2. Create `BoardLogEntry` with `op="editPost"`, `oldPostCid`, `newPostCid`.
- Deleting (tombstone):
  1. Create `BoardLogEntry` with `op="tombstonePost"`, `targetPostCid`, optional `reason`.

Clients must:

- Validate signatures on Post / BoardMeta / ThreadMeta / BoardLogEntry.
- Ignore any objects with invalid signatures.
- Support a local list of `blockedPubKeys` and hide content authored by those keys.

## Go backend (BBS node)

### Responsibilities

- Wrap Flexible-IPFS HTTP API in a `FlexibleIPFSClient` Go package.
- Implement domain packages:
  - `bbs/types` for structs and JSON marshalling
  - `bbs/signature` for ed25519 key management and canonical payload generation
  - `bbs/storage` for saving/loading objects via Flexible-IPFS
  - `bbs/log` for BoardLog replay
- Provide HTTP API for C# client under `/api/v1`:
  - `GET /api/v1/boards`
  - `GET /api/v1/boards/{boardId}`
  - `GET /api/v1/boards/{boardId}/threads`
  - `GET /api/v1/threads/{threadId}`
  - `POST /api/v1/threads`
  - `POST /api/v1/posts`
  - `POST /api/v1/posts/{postCid}/edit`
  - `POST /api/v1/posts/{postCid}/tombstone`

### Node roles (CLI)

Implement a single binary `bbs-node` with:

- `--role=client | indexer | archiver | full`
- `--flexipfs-base-url=http://127.0.0.1:5001/api/v0`
- `--http-port=...`
- For `indexer` / `full`, maintain a local DB (e.g. SQLite) to index boards/threads/posts by replaying BoardLogEntries.

## C# client

- Talk only to the Go backend `/api/v1`.
- Manage user key pairs (ed25519) and store them locally as JSON.
- Provide UI for:
  - Boards list, threads list, posts.
  - Creating/editing posts and threads.
  - Managing blocked public keys.

At startup, C# should:

- Ensure Flexible-IPFS and `bbs-node` are running (spawn them as child processes if configured).

## ToDo table (for Codex to follow)

Create a `docs/TODO.md` with a Markdown table like this and keep it updated:

| ID | Area            | Task                                                                                   | Status |
|----|-----------------|----------------------------------------------------------------------------------------|--------|
| 1  | Go: core types  | Define Go structs for Post, ThreadMeta, BoardMeta, BoardLogEntry with JSON tags       | TODO   |
| 2  | Go: signature   | Implement ed25519 key management + canonical payload generators + sign/verify methods | TODO   |
| 3  | Go: IPFS client | Implement FlexibleIPFSClient for putvaluewithattr / getvalue / getbyattrs             | TODO   |
| 4  | Go: storage     | Implement storage layer for saving/loading each object type over Flexible-IPFS        | TODO   |
| 5  | Go: log         | Implement BoardLog replay per boardId/threadId                                        | TODO   |
| 6  | Go: HTTP API    | Implement `/api/v1/...` endpoints with proper JSON schemas and error handling         | TODO   |
| 7  | Go: roles       | Implement CLI flags and modes (client/indexer/archiver/full)                          | TODO   |
| 8  | Go: tests       | Add unit tests for signature, storage, and log replay                                 | TODO   |
| 9  | C#: client API  | Implement a typed client for `/api/v1` endpoints                                      | TODO   |
| 10 | C#: UI          | Implement basic UI for boards/threads/posts + key management                          | TODO   |
| 11 | C#: blocked key | Implement blocked pubkey list and filtering on the UI                                 | TODO   |
| 12 | Packaging       | Add build scripts to bundle Flexible-IPFS, Go backend, and C# UI per OS               | TODO   |

## Important instructions for you (Codex)

- **Never change the JSON field names or types** defined above.
- Keep the signing payload format **exactly** as specified (field order and names must match).
- When implementing HTTP calls to Flexible-IPFS, always:
  - URL-encode query parameters (especially JSON in `value`).
  - Handle network errors and timeouts gracefully.
- Write **unit tests** for:
  - Canonical payload generation
  - Signing + verification
  - BoardLog replay (including tombstones and edits)
- Prefer small, focused packages and functions.
- Preserve existing file structure and style when editing.
- Before large refactors, update `docs/TODO.md` to reflect the change.
```

