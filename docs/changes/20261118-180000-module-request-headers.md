### Added
- [sdk] `module.yaml` takes `routes.headers`, a list of request headers the module reads, each with a name and a reason. A module may ask only for a header on a list the core keeps, today `Print-After-Upload`.
- An install shows each header under `routes.headers` as `header <name> (<reason>)`, for the operator to approve. An approved header is written to `service.request_headers` and reaches the module on every route. The extension's page lists it with its reason.
- [sdk] A header under `routes.headers` may say `required: true`. The server does not start the module while that header is not granted, and an update that would leave it out is refused.
