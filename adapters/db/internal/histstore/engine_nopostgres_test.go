//go:build !postgres

package histstore_test

// postgresEngine reports that PostgreSQL is not reachable: its driver lives
// behind the postgres build tag, which a configuration naming that engine
// derives through adapters/tags.json.
func postgresEngine() (engine, bool) { return engine{}, false }
