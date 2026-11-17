//go:build !postgres

package app

// postgresHistoryEngine reports that PostgreSQL is not reachable: its driver
// lives behind the postgres build tag, which a configuration naming that engine
// derives through adapters/tags.json.
func postgresHistoryEngine() (historyEngine, bool) { return historyEngine{}, false }
