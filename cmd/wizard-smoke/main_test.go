package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTheCommandLineIsHeldToWhatAWalkNeeds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		ok   bool
	}{
		{"a binary", []string{"-binary", "server"}, true},
		{"a binary in a container", []string{"-binary", "server", "-docker-platform", "linux/arm/v7",
			"-docker-image", "debian@sha256:00"}, true},
		{"no binary", nil, false},
		{"a platform with no image", []string{"-binary", "server", "-docker-platform", "linux/arm/v7"}, false},
		{"an image with no platform", []string{"-binary", "server", "-docker-image", "debian@sha256:00"}, false},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()

			_, err := parse(each.args)
			require.Equal(t, each.ok, err == nil, err)
		})
	}
}

func TestOneFailedCheckFailsTheWalk(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	report := newChecks(&out)
	report.check("a check that passes", true, nil)
	require.NoError(t, report.result(nil))
	require.Contains(t, out.String(), "RESULT OK")

	report.check("a check that fails", false, "why")
	require.ErrorIs(t, report.result(nil), errFailed)
	require.Contains(t, out.String(), "FAIL a check that fails  (why)")
	require.Contains(t, out.String(), "RESULT FAILED: a check that fails")
}

func TestAWalkThatBrokeOffIsAFailure(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	report := newChecks(&out)
	require.ErrorIs(t, report.result(errors.New("docker run: no such image")), errFailed)
	require.Contains(t, out.String(), "FAIL the walk: docker run: no such image")
}
