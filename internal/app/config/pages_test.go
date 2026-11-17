package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestEveryKeyHasAPageThatExists keeps every link the wizard draws on a page
// the documentation has.
func TestEveryKeyHasAPageThatExists(t *testing.T) {
	t.Parallel()

	slugs := map[string]bool{}
	for _, page := range config.Pages {
		slugs[page.Slug] = true
	}

	for path := range config.Keys {
		require.True(t, slugs[config.PageOf(path)], "%s goes to %s, which is no page", path, config.PageOf(path))
	}

	require.Equal(t, "config/encryption/#key-core-api-tls-certificate",
		config.KeyReference("core.api.tls.certificate"))
	require.Equal(t, "config/network/#key-core-api-addr", config.KeyReference("core.api.addr"))
	require.Equal(t, "config/database/#key-persistence-type", config.KeyReference("persistence.type"))
}
