# TODO

| ID | Area            | Task                                                                                   | Status |
|----|-----------------|----------------------------------------------------------------------------------------|--------|
| 1  | Go: core types  | Define Go structs for Post, ThreadMeta, BoardMeta, BoardLogEntry with JSON tags       | DONE   |
| 2  | Go: signature   | Implement ed25519 key management + canonical payload generators + sign/verify methods | DONE   |
| 3  | Go: IPFS client | Implement FlexibleIPFSClient for putvaluewithattr / getvalue / getbyattrs             | DONE   |
| 4  | Go: storage     | Implement storage layer for saving/loading each object type over Flexible-IPFS        | DONE   |
| 5  | Go: log         | Implement BoardLog replay per boardId/threadId                                        | DONE   |
| 6  | Go: HTTP API    | Implement `/api/v1/...` endpoints with proper JSON schemas and error handling         | DONE   |
| 7  | Go: roles       | Implement CLI flags and modes (client/indexer/archiver/full)                          | DONE   |
| 8  | Go: tests       | Add unit tests for signature, storage, and log replay                                 | DONE   |
| 9  | C#: client API  | Implement a typed client for `/api/v1` endpoints                                      | DONE   |
| 10 | C#: UI          | Implement basic UI for boards/threads/posts + key management                          | DONE   |
| 11 | C#: blocked key | Implement blocked pubkey list and filtering on the UI                                 | DONE   |
| 12 | Packaging       | Add build scripts to bundle Flexible-IPFS, Go backend, and C# UI per OS               | DONE   |
