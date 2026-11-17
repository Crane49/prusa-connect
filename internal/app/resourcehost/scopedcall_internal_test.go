package resourcehost

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// scopeFleet records the scopes each fleet call reached it under.
type scopeFleet struct {
	deviceapi.Fleet

	saw [][]authz.Scope
}

func (f *scopeFleet) Command(ctx context.Context, _ deviceapi.CommandParams) (deviceapi.Result, error) {
	f.saw = append(f.saw, authz.ScopesFrom(ctx))

	return deviceapi.Result{}, nil
}

// A call a module makes for a person while that person's request is open runs
// under the scopes the request's credential carried. A session open beside it
// does not lend the call its reach, and once the limited request ends the call
// runs under what is still open.
func TestAModuleCallIsHeldToTheScopesOfTheRequestItServes(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindSink, "queue")
	require.NoError(t, err)

	inner := &scopeFleet{}
	board := &flights{}
	fleet := vouchedFleet{inner: inner, instance: instance, board: board}
	command := deviceapi.CommandParams{Principal: "user:ada"}

	leaveSession := board.board(instance, "user:ada", nil)
	leaveRead := board.board(instance, "user:ada", []authz.Scope{authz.ScopeRead})

	_, err = fleet.Command(t.Context(), command)
	require.NoError(t, err)

	leaveRead()

	_, err = fleet.Command(t.Context(), command)
	require.NoError(t, err)

	leaveSession()

	_, err = fleet.Command(t.Context(), command)
	require.ErrorIs(t, err, deviceapi.ErrDenied, "a call with no request open was let through")

	require.Equal(t, [][]authz.Scope{{authz.ScopeRead}, nil}, inner.saw)
}
