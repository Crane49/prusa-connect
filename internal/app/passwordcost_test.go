package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
)

// The account store hashes a new password at the cost the configuration
// names. The printer build names 7 MiB with 5 passes when the file names none.
func TestTheAccountStoreHashesAtTheConfiguredPasswordCost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		memory int
		passes int
		want   string
	}{
		{name: "the default of this build", want: defaultCostPrefix()},
		{name: "12 MiB with 3 passes", memory: 12 << 20, passes: 3, want: "$argon2id$v=19$m=12288,t=3,p=1$"},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()

			core := config.Default()
			core.Auth.PasswordMemoryBytes = each.memory
			core.Auth.PasswordPasses = each.passes
			core = core.WithDefaults(filepath.Join(t.TempDir(), "config.yaml"))
			require.NoError(t, core.Validate())

			options, err := accountOptions(core)
			require.NoError(t, err)

			store := local.NewMemoryStore()
			accounts := local.New(store, nil, options...)

			_, err = accounts.CreateFirst(t.Context(), "operator", local.NewAccount{
				Username: "owner@example.com",
				Password: []byte("correct horse battery"),
			})
			require.NoError(t, err)

			row, err := store.ByID(t.Context(), "operator")
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(row.Hash, each.want), "the password was hashed as %s", row.Hash)
		})
	}
}

// defaultCostPrefix is the start of a hash made at this build's default cost.
func defaultCostPrefix() string {
	if buildprofile.Name == "printer" {
		return "$argon2id$v=19$m=7168,t=5,p=1$"
	}

	return "$argon2id$v=19$m=19456,t=2,p=1$"
}
