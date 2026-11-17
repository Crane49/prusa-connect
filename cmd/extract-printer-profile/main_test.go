package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// The file the command writes is one the profile loader accepts, and it holds
// the one model that was asked for.
func TestTheWrittenFileHoldsTheOneModel(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "profile.json")

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitOK, run([]string{"-model", "mk4s", "-out", out}, &stdout, &stderr), stderr.String())

	data, err := os.ReadFile(out)
	require.NoError(t, err)

	registry, err := profile.Load(data)
	require.NoError(t, err)
	require.Equal(t, []string{"MK4S"}, registry.Models())
}

// A model no profile answers to stops the build and writes no file.
func TestAnUnknownModelWritesNothing(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "profile.json")

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitFailure, run([]string{"-model", "MK5", "-out", out}, &stdout, &stderr))
	require.Contains(t, stderr.String(), "MK4S")

	_, err := os.Stat(out)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// The part of the registry one model uses survives the file the printer build
// embeds. Every model is written out and loaded back, and the loaded profiles
// equal the built-in ones, so any PRINTER_MODEL gives a printer the same
// capabilities the normal build gives it.
func TestEveryModelLoadsBackAsTheBuiltinProfile(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	require.NoError(t, err)

	models := registry.Models()
	require.Greater(t, len(models), 10, "the built-in registry holds too few models to prove anything")

	for _, model := range models {
		data, marshalErr := json.Marshal(only(registry, model))
		require.NoError(t, marshalErr, model)

		loaded, loadErr := profile.Load(data)
		require.NoError(t, loadErr, model)
		require.Equal(t, []string{model}, loaded.Models())

		var want []profile.Profile

		for _, candidate := range registry.Profiles {
			if candidate.Model == model {
				want = append(want, candidate)
			}
		}

		require.Equal(t, want, loaded.Profiles, model)

		for _, bound := range loaded.Bounds {
			require.Equal(t, []string{model}, bound.Models, "a bound row names another model")
		}
	}
}
