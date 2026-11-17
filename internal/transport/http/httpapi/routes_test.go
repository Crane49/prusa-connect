package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// errTableInput carries a path parameter because that is what makes huma.Register
// append 422 to the operation's error table. Without one it appends only 500,
// and the probe below would see half of what it is watching for.
type errTableInput struct {
	ID string `path:"id"`
}

type errTableOutput struct {
	Body struct{}
}

// TestRegisteringAnOperationNeverWritesIntoTheErrorTableItWasGiven holds the
// rule register exists to keep: huma.Register appends to op.Errors, so the
// slice it is handed has to be one nothing else holds.
//
// The probe watches the slots past the length rather than the contents, because
// that is where the write lands. A shared table's own elements never change,
// which is why nothing in this package sees the corruption and only the race
// detector does.
func TestRegisteringAnOperationNeverWritesIntoTheErrorTableItWasGiven(t *testing.T) {
	t.Parallel()

	const sentinel = -1

	shared := make([]int, 2, 8)
	shared[0] = http.StatusUnauthorized
	shared[1] = http.StatusForbidden

	spare := shared[len(shared):cap(shared)]
	for i := range spare {
		spare[i] = sentinel
	}

	api := humachi.New(chi.NewMux(), huma.DefaultConfig("probe", "0"))

	register(api, huma.Operation{
		OperationID: "sentinel-probe",
		Method:      http.MethodPost,
		Path:        "/probe/{id}",
		Summary:     "Never served. It exists to be registered.",
		Errors:      shared,
	}, func(context.Context, *errTableInput) (*errTableOutput, error) {
		return &errTableOutput{}, nil
	})

	for i, got := range spare {
		require.Equalf(t, sentinel, got,
			"registering an operation wrote %d into slot %d beyond the error table it was given, "+
				"so two servers built at once write the same array", got, i)
	}
}

// TestServersBuiltAtOnceShareNoMutableState is the race detector's half of the
// same rule. It fails only under -race, which is why the deterministic probe
// above exists beside it: a developer whose toolchain cannot build with the
// detector still gets a gate. Run it with `make test-race`.
func TestServersBuiltAtOnceShareNoMutableState(t *testing.T) {
	t.Parallel()

	for i := range 8 {
		t.Run("server "+strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			require.NotNil(t, newTestServer(t))
		})
	}
}
