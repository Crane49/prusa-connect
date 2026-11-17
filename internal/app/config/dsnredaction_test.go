package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestADSNIsPrintedWithoutAPasswordItCarriesInTheQuery is the half of the
// redaction that reads the whole URL.
//
// lib/pq builds its connection settings from a URL's query string as well as
// from its userinfo, so `password=` and `sslpassword=` in the query are working
// credentials and are what a deployment that keeps its DSN in one string
// naturally writes. RedactedDSN reads parsed.User and nothing else, so those
// two reach the start-up line openGraph logs, every error message that names
// the database, a journal, and a bug report.
func TestADSNIsPrintedWithoutAPasswordItCarriesInTheQuery(t *testing.T) {
	t.Parallel()

	for name, dsn := range map[string]string{
		"the password as a query parameter": "postgres://someone@db:5432/connect?password=secret&sslmode=disable",
		"the key password as a query parameter": "postgres://someone@db:5432/connect?" +
			"sslmode=verify-full&sslpassword=secret",
		"both places at once": "postgres://someone:secret@db:5432/connect?password=secret",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			printed := config.PostgresEngine{DSN: dsn}.RedactedDSN()
			require.NotContains(t, printed, "secret", "the redacted dsn still carries the password")
		})
	}
}
