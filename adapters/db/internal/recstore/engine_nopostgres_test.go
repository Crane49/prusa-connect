//go:build !postgres

package recstore_test

// postgresEngine reports that Postgres is not reachable: its driver lives
// behind the postgres build tag, which a configuration naming that engine
// derives through adapters/tags.json.
func postgresEngine() (engine, bool) { return engine{}, false }
