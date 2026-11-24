### Changed

- An upload to a storage over the size limit is refused before its body is sent, with
  `core.validation/UPLOAD_TOO_LARGE`, which names the limit

### Added

- `core.api.max_upload_size` sets the largest file one upload to a storage may be, 200 MiB by default, and
  `GET /api/v1/ui` reports it as `max_upload_bytes`
