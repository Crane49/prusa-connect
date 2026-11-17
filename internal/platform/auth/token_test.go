package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMintProducesATokenSplitReads(t *testing.T) {
	t.Parallel()

	token, publicID, digest, err := Mint()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(token), Prefix), "token %q lacks the prefix", token)
	require.Len(t, string(token), tokenLen)

	gotID, secret, err := Split(token)
	require.NoError(t, err)
	require.Equal(t, publicID, gotID)
	require.Equal(t, digest, DigestOf(secret))
}

func TestMintIsUnpredictable(t *testing.T) {
	t.Parallel()

	const draws = 512

	seen := make(map[string]bool, draws)

	for range draws {
		token, _, _, err := Mint()
		require.NoError(t, err)
		require.False(t, seen[string(token)], "Mint repeated a token")

		seen[string(token)] = true
	}
}

func TestSplitRefusesWhatThisServerDidNotIssue(t *testing.T) {
	t.Parallel()

	good, _, _, err := Mint()
	require.NoError(t, err)

	text := string(good)

	// Flip one character of the secret half. The checksum covers it, so this is
	// caught with no store lookup at all.
	tampered := text[:secretAt] + flip(text[secretAt:secretAt+1]) + text[secretAt+1:]

	cases := []struct {
		name  string
		token Token
		want  error
	}{
		{name: "empty", token: "", want: ErrNoToken},
		{name: "no prefix", token: Token(text[len(Prefix):]), want: ErrMalformedToken},
		{name: "wrong prefix", token: Token("gh_" + text[len(Prefix):]), want: ErrMalformedToken},
		{name: "truncated", token: Token(text[:len(text)-1]), want: ErrMalformedToken},
		{name: "no separator", token: Token(text[:separatorAt] + "x" + text[secretAt:]), want: ErrMalformedToken},
		{name: "non base62", token: Token(text[:secretAt] + "-" + text[secretAt+1:]), want: ErrMalformedToken},
		{name: "tampered secret", token: Token(tampered), want: ErrChecksumMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, splitErr := Split(tc.token)
			require.ErrorIs(t, splitErr, tc.want)
		})
	}
}

// flip returns a different base62 character, so a tamper is always a change.
func flip(s string) string {
	if s == "A" {
		return "B"
	}

	return "A"
}
