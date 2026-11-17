package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// The send address has three states: absent is the beta portal, empty is off,
// and anything else is the operator's own portal.
func TestTheDiagnosticsSendAddressHasThreeStates(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		block string
		want  string
	}{
		"absent":        {block: "core:\n  diagnostics:\n    enabled: true\n", want: config.DefaultDiagnosticsSendURL},
		"written empty": {block: "core:\n  diagnostics:\n    send_url: \"\"\n", want: ""},
		"an own portal": {
			block: "core:\n  diagnostics:\n    send_url: https://support.example/beta/\n",
			want:  "https://support.example/beta",
		},
	}

	for name, each := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			core, err := config.Load(write(t, each.block))
			require.NoError(t, err)
			require.NoError(t, core.Validate())
			require.Equal(t, each.want, core.Diagnostics.SendTarget())
		})
	}
}

// The browser posts to the address with a path appended, so it must be an
// http or https address the path can follow.
func TestASendAddressTheBrowserCannotPostToIsRefused(t *testing.T) {
	t.Parallel()

	for _, address := range []string{
		"ftp://beta.example", "beta.example", "https://beta.example/?a=1", "https://user@beta.example",
	} {
		core, err := config.Load(write(t, "core:\n  diagnostics:\n    send_url: \""+address+"\"\n"))
		require.NoError(t, err)

		err = core.Validate()
		require.ErrorIsf(t, err, config.ErrInvalid, "%s was accepted", address)
		require.ErrorContains(t, err, "send_url")
	}
}
