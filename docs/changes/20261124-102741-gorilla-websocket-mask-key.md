### Security
- The server is built with `github.com/gorilla/websocket` at commit d67f418, which fixes GO-2026-6278: a WebSocket client drew its frame mask keys from a weak random source.
