// The GraphQL surface of this core, as a module of its own (ADR-20260910-cqq rule 1).
//
// It carries its own go.mod for the reason ADR-20260910-cqq's consequences give: no
// GraphQL runtime enters the core's dependencies. `go build ./...` in the core
// skips this directory, Go itself refuses an `internal/` import from here, and
// the one third-party requirement below is this module's and never the core's.
//
// packages/ is where source that becomes a repository of its own lives, and
// ADR-20260910-cqq rule 14 says one repository per module. Until that repository
// exists the replace below binds the sdk to the checkout beside it, which is
// what a development build does before the sdk is tagged. A released module
// requires a published sdk version and carries no replace.
module github.com/prusa3d/graphql

go 1.26.11

require (
	github.com/prusa3d/prusa-connect/sdk v0.0.0-00010101000000-000000000000
	github.com/graphql-go/graphql v0.8.1
	go.yaml.in/yaml/v3 v3.0.5
)

replace github.com/prusa3d/prusa-connect/sdk => ../../sdk
