//go:build !postgres

package engine

// postgresTestEngine reports that PostgreSQL is not reachable: its driver is
// behind the postgres build tag.
func postgresTestEngine() (testEngine, bool) { return testEngine{}, false }
