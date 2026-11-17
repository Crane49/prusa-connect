// The public contracts an extension implements. Separate from the application
// module so an extension author inherits the standard library and nothing else,
// and so the contracts can reach a stable major version on their own schedule.
//
// ADR-20260824-gfr rule 2 forbids third-party imports here, and there are none: this
// file requires nothing. gin was the one violation, reached through
// routesapi.RouteDeps.Router. That contract was the in-process plugin surface
// of the legacy server; it is deleted, and with it the precondition ADR-20260824-gfr
// records against sdk/v1.
module github.com/prusa3d/prusa-connect/sdk

go 1.26.11
