// The Prusa Fractal protocol over NATS, as a module of its own.
//
// It carries its own go.mod so that no NATS server, no NATS client and no JWT
// library enters the core's dependencies. `go build ./...` in the core walks
// past this directory, Go refuses an `internal/` import from here, and the
// requirements below belong to this module alone.
//
// packages/ is where source that becomes a repository of its own lives. Until
// that repository exists the replace below binds the sdk to the checkout
// beside it, which is what a development build does before the sdk is tagged.
// A released module requires a published sdk version and carries no replace.
module github.com/prusa3d/fractal-link

go 1.26.11

require (
	github.com/prusa3d/prusa-connect/sdk v0.0.0-00010101000000-000000000000
	github.com/nats-io/jwt/v2 v2.8.2
	github.com/nats-io/nats-server/v2 v2.14.3
	github.com/nats-io/nats.go v1.52.0
	github.com/nats-io/nkeys v0.4.16
	golang.org/x/text v0.41.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/antithesishq/antithesis-sdk-go v0.7.0-default-no-op // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/klauspost/compress v1.18.7 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/prusa3d/prusa-connect/sdk => ../../sdk
