### Security
- `GET /api/v1/storages/{storage}/content` no longer serves a stored file as a web page that runs script with the session of whoever opens the link. A file this server serves itself now takes its type from its name and is sent as a sandboxed attachment the browser never sniffs.
