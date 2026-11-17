package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// A request that presented nothing is how every browser starts, so it is
// logged at debug. A credential that was presented and refused is a warning.
func TestARefusalIsLoggedAtTheLevelItDeserves(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	refusal := fault.AuthTokenInvalid.New("the credential is not one this server accepts")

	logRefusal(t.Context(), logger, "/api/v1/principal",
		fmt.Errorf("%w: %w", ErrNoCredential, refusal.Wrap(errors.New("auth: no credential presented"))))
	require.Contains(t, out.String(), "level=DEBUG")
	require.NotContains(t, out.String(), "level=WARN")

	out.Reset()

	logRefusal(t.Context(), logger, "/api/v1/principal", refusal.Wrap(errors.New("auth: malformed token")))
	require.Contains(t, out.String(), "level=WARN")
}
