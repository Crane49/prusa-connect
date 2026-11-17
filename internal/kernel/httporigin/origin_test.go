package httporigin_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/httporigin"
)

func TestSameUsesEffectivePort(t *testing.T) {
	t.Parallel()

	parse := func(raw string) *url.URL {
		t.Helper()
		address, err := url.Parse(raw)
		require.NoError(t, err)

		return address
	}

	require.True(t, httporigin.Same(
		parse("https://beta.example/catalog"),
		parse("https://BETA.example:443/artifact"),
	))
	require.False(t, httporigin.Same(
		parse("https://beta.example/catalog"),
		parse("https://other.example/artifact"),
	))
	require.False(t, httporigin.Same(
		parse("https://beta.example/catalog"),
		parse("http://beta.example/artifact"),
	))
}
