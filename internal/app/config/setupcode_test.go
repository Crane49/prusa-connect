package config_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestASetupCodeOfYourOwnIsHeldToTheLengthOfAMadeOne is ADR-20260929-a4j rule 4. A
// code from the file is 12 to 64 printable ASCII characters once its hyphens
// and spaces are gone, with at least 6 different ones, and a refusal names
// what is wrong and never the code.
func TestASetupCodeOfYourOwnIsHeldToTheLengthOfAMadeOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		code  string
		valid bool
	}{
		{name: "empty, so the server makes one", code: "", valid: true},
		{name: "twelve characters", code: "ABCD-EFGH-JKMN", valid: true},
		{name: "sixty-four characters", code: strings.Repeat("ABCDEFGH", 8), valid: true},
		{name: "twelve of one letter", code: "aaaaaaaaaaaa"},
		{name: "five different characters", code: "ABCDE-ABCDE-ABCDE"},
		{name: "a long s that upper-cases to S", code: "ABCDEFGHJKMſ"},
		{name: "a dotless i that upper-cases to I", code: "ABCDEFGHJKMı"},
		{name: "a sentence with spaces", code: "our farm first run 2026", valid: true},
		{name: "eleven characters", code: "ABCD-EFGH-JKM"},
		{name: "twelve with hyphens counted", code: "AB-CD-EF-GH-JK"},
		{name: "sixty-five characters", code: strings.Repeat("A", 65)},
		{name: "a letter outside ASCII", code: "ABCD-EFGH-JKMŇ"},
		{name: "a tab", code: "ABCDEFGH\tJKMN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			core := config.Default().WithDefaults(write(t, ""))
			core.Auth.SetupCode = tc.code

			err := core.Validate()
			if tc.valid {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, config.ErrInvalid)
			require.Contains(t, err.Error(), "core.auth.setup_code")
			require.NotContains(t, err.Error(), tc.code, "the refusal printed the code")
		})
	}
}

// TestASetupCodeIsReadFromTheFile proves the key an operator writes is the key
// the server gets.
func TestASetupCodeIsReadFromTheFile(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, "core:\n  auth:\n    setup_code: ABCD-EFGH-JKMN\n"))
	require.NoError(t, err)
	require.Equal(t, "ABCD-EFGH-JKMN", core.Auth.SetupCode)
}

// TestTwoSpellingsOfOneCodeReadTheSame is rule 6: what a person types
// differently reads the same, and nothing else does.
func TestTwoSpellingsOfOneCodeReadTheSame(t *testing.T) {
	t.Parallel()

	require.Equal(t, config.NormalSetupCode("K7QD-3M9X-TP20"), config.NormalSetupCode("k7qd 3m9x tp2o"))
	require.Equal(t, config.NormalSetupCode("1111"), config.NormalSetupCode("ILil"))
	require.NotEqual(t, config.NormalSetupCode("K7QD-3M9X-TP20"), config.NormalSetupCode("K7QD-3M9X-TP21"))
}
