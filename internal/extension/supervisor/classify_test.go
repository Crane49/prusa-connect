package supervisor_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

//nolint:gochecknoglobals // registration happens at init time; these are the fixtures the table reads.
var (
	classifyDomain = must(fault.NewDomain("test.classify"))

	retryable = fault.Register(classifyDomain, "UPSTREAM_BUSY", 503, fault.Document(
		"The remote asked us to come back later",
		"Retry with backoff.",
		fault.RetryAllowed,
		fault.ShowDetail,
	))

	forbidden = fault.Register(classifyDomain, "CREDENTIAL_REJECTED", 403, fault.Document(
		"The remote rejected the credential",
		"Fix the credential.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	missing = fault.Register(classifyDomain, "KEY_NOT_FOUND", 404, fault.Document(
		"No object is stored under that key",
		"Check the key.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	broken = fault.Register(classifyDomain, "DRIVER_BROKEN", 500, fault.Document(
		"The driver cannot work in this configuration",
		"Fix the configuration and restart the resource.",
		fault.RetryNever,
		fault.ShowDetail,
	))
)

func TestClassifyDefaultsToRecoverable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want supervisor.Class
	}{
		{name: "no error", err: nil, want: supervisor.ClassSuccess},
		{name: "the caller went away", err: context.Canceled, want: supervisor.ClassCancelled},
		{name: "a deadline", err: context.DeadlineExceeded, want: supervisor.ClassRecoverable},
		{name: "a missing file", err: fs.ErrNotExist, want: supervisor.ClassClient},
		{name: "a retryable code", err: retryable.New("429"), want: supervisor.ClassRecoverable},
		{name: "a rejected credential", err: forbidden.New("403"), want: supervisor.ClassPermanent},
		{name: "a missing key", err: missing.New("404"), want: supervisor.ClassClient},
		{name: "a broken driver", err: broken.New("500"), want: supervisor.ClassPermanent},
		{
			name: "an error nobody has classified",
			err:  errors.New("the vendor library said something new"),
			want: supervisor.ClassRecoverable,
		},
		{
			name: "a wrapped code still carries its verdict",
			err:  fmt.Errorf("list objects: %w", forbidden.New("403")),
			want: supervisor.ClassPermanent,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, supervisor.Classify(testCase.err))
		})
	}
}

func TestOnlyRealFailuresCountAgainstTheBreaker(t *testing.T) {
	t.Parallel()

	// A 404 from storage and a cancelled request mean the system worked
	// correctly. Counting them would trip the breaker on user input.
	assert.False(t, supervisor.ClassClient.Failure())
	assert.False(t, supervisor.ClassCancelled.Failure())
	assert.False(t, supervisor.ClassSuccess.Failure())
	assert.True(t, supervisor.ClassRecoverable.Failure())
	assert.True(t, supervisor.ClassPermanent.Failure())
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}
