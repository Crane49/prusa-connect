package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// persistence.postgres.module_admin_dsn is read only on Postgres, and only as
// a postgres:// URL, and a refusal never quotes it (ADR-20261021-bqc).
func TestTheModuleAdminConnectionIsHeldToAPostgresURL(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		block string
		want  string
	}{
		"on sqlite": {
			block: "persistence:\n  type: sqlite\n  postgres:\n    module_admin_dsn: postgres://admin:s3cret@db:5432/postgres\n",
			want:  "Remove the key",
		},
		"a key=value string": {
			block: "persistence:\n  type: postgres\n  postgres:\n    dsn: postgres://db:5432/connect\n" +
				"    module_admin_dsn: host=db user=admin password=s3cret dbname=postgres\n",
			want: "not a postgres:// or postgresql:// URL",
		},
		"no database": {
			block: "persistence:\n  type: postgres\n  postgres:\n    dsn: postgres://db:5432/connect\n" +
				"    module_admin_dsn: postgres://admin:s3cret@db:5432\n",
			want: "with a host and a database name",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			core, err := config.Load(write(t, tc.block))
			require.NoError(t, err)

			err = core.Validate()
			require.ErrorIs(t, err, config.ErrInvalid)
			require.ErrorContains(t, err, tc.want)
			require.NotContains(t, err.Error(), "s3cret")
		})
	}

	core, err := config.Load(
		write(t, "persistence:\n  type: postgres\n  postgres:\n    dsn: postgres://db:5432/connect\n"+
			"    module_admin_dsn: postgres://admin:s3cret@db:5432/postgres?sslmode=require\n"),
	)
	require.NoError(t, err)
	require.NoError(t, core.Validate())
}
