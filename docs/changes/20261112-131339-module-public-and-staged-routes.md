### Added
- [sdk] `module.yaml` takes a `routes` block with a `public` and a `staged` list, and `routesapi.BodyFileOf` reads the file a staged request body was written to. Each entry names a method and a path under the module's mount, where `{name}` matches one segment, and no route is both public and staged.
- A module's manifest may name routes under `routes.public`, and an install lists each one as `public <method> <path>` among its requests. Once the operator approves it, the route is written to `service.public_routes` and answers without a signed-in caller.
- An install shows each route under `routes.staged` in a module's manifest as `upload <method> <path> up to <n> MiB`, for the operator to approve. An approved route is written to `service.staged_routes` with its limit, and its request body goes into the module's data directory and not to the module.
- An operator changes the largest body of an approved staged route in the extension's Uploads tab, or with `staged_routes` on `PUT /api/v1/resources/{kind}/{name}`, from 1 to 4096 MiB. The extension restarts with the new limit, and a body over it is refused with `BODY_TOO_LARGE` before the module runs.
- [sdk] `wirerpc.Grants` carries `Staged`, each staged route the operator approved with its limit, and `Grants.StagedMiB` reads the limit of one route.
- A module receives at most 4 large uploads at once, and one more is refused with `MODULE_UPLOADS_BUSY` and `Retry-After`. An upload the disk has no room for is refused with `MODULE_STORAGE_FULL` before any of it is read.
- [sdk] `manifest.Route.Overlaps` reports whether one request could be on two routes, and a manifest whose public route reaches one of its staged routes is refused. A manifest with two staged routes one request can reach is refused too.

### Fixed
- [sdk] `routesapi.Request.HTTP` passes the path and the query to a module handler as the host sent them, without parsing them again. A request whose decoded path holds `?`, `#`, `%` or a control character reaches no module, and the host answers 404.
