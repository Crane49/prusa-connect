### Fixed
- A read-scoped API token no longer sees operate actions and verbs in the device list. `GET /api/v1/devices` gives each printer only the `actions` and `verbs` the token's scopes allow.
