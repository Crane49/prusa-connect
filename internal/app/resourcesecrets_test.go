package app

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// secretConfig declares one sink whose type declares a password, and one
// storage instance so the file also holds a type that declares no credential.
func secretConfig(dir string) string {
	return `core:
  log:
    level: error
resources:
  storage:
    local:
      type: filesystem
      config:
        root: ` + slashed(filepath.Join(dir, "storage")) + `
  sink:
    broker:
      type: mqtt
      enabled: false
      config:
        url: tcp://127.0.0.1:1883
        username: bob
        password: hunter2
    hook:
      type: webhook
      enabled: false
      config:
        url: https://example.invalid/hook
        header:
          Authorization: Bearer opensesame
`
}

// TestAReadOfAResourceNeverCarriesADeclaredSecret holds both resource routes to
// what the authorization vocabulary promises: a caller who may view an instance
// reads its settings with every value the type declared as a credential
// replaced by a marker.
func TestAReadOfAResourceNeverCarriesADeclaredSecret(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, secretConfig(dir))

	listed := running.get(t, "/api/v1/resources")
	require.Equal(t, http.StatusOK, listed.status, listed.body)
	require.NotContains(t, listed.body, "hunter2", "the listing returned the broker password")

	one := running.get(t, "/api/v1/resources/sink/broker")
	require.Equal(t, http.StatusOK, one.status, one.body)
	require.NotContains(t, one.body, "hunter2", "the instance route returned the broker password")

	var instance httpapi.ResourceInstance
	require.NoError(t, json.Unmarshal([]byte(one.body), &instance))

	var settings map[string]any
	require.NoError(t, json.Unmarshal(instance.Config, &settings))

	require.Equal(t, "xxxxx", settings["password"], "the password must read as the marker")
	require.Equal(t, "bob", settings["username"], "a key that is not a credential is returned as it is")

	// A block of headers is a block of credentials: the whole of it reads as
	// the marker, the names in it included.
	hook := running.get(t, "/api/v1/resources/sink/hook")
	require.Equal(t, http.StatusOK, hook.status, hook.body)
	require.NotContains(t, hook.body, "opensesame", "the webhook header carried its token")
	require.NotContains(t, hook.body, "Authorization", "the webhook header carried the name of its token")

	running.stopAndWait(t)
}

// TestAMisspeltTypeNameDoesNotHandBackItsSettings: a type this build has no
// implementation for has no option list, so nothing can say which of its keys
// is a credential. The block is still returned to whoever may view the
// instance, so every value in it is replaced and the keys are kept.
func TestAMisspeltTypeNameDoesNotHandBackItsSettings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `core:
  log:
    level: error
resources:
  sink:
    broker:
      type: mqqt
      enabled: false
      config:
        url: tcp://127.0.0.1:1883
        password: hunter2
`

	running := start(t, dir, content)

	for _, path := range []string{"/api/v1/resources", "/api/v1/resources/sink/broker"} {
		read := running.get(t, path)
		require.Equal(t, http.StatusOK, read.status, read.body)
		require.NotContains(t, read.body, "hunter2", "a misspelt type handed back the password")
		require.Contains(t, read.body, `"password":"xxxxx"`, "the keys the operator wrote are kept")
	}

	running.stopAndWait(t)
}
