### Changed
- [sdk] The `type` of every error response now points to `https://beta.prusa3d.com/connect-local/docs/error-codes/` instead of `https://prusa-connect.org/docs/error-codes/`, a domain Prusa does not own. The anchor after `#` is unchanged. A client that compares the whole `type` updates the base, or compares `code`, which is the stable identifier.
