package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// fakeAdmission is one connector's registration table, holding whatever a case
// puts in it.
type fakeAdmission struct {
	waiting  []admission.Registration
	approved []string
	rejected []string
	// admittedBy is who the surface said had decided, so a case can hold that
	// the caller's identity reaches the table rather than being dropped on the
	// way.
	admittedBy string
	fail       error
}

func (f *fakeAdmission) Pending() []admission.Registration { return f.waiting }

func (f *fakeAdmission) Approve(
	_ context.Context, code, by string,
) (admission.Registration, error) {
	if f.fail != nil {
		return admission.Registration{}, f.fail
	}

	for _, item := range f.waiting {
		if item.Code != code {
			continue
		}

		f.approved = append(f.approved, code)
		f.admittedBy = by
		item.Approved = true

		return item, nil
	}

	return admission.Registration{}, fmt.Errorf("%w: %s", admission.ErrUnknownCode, code)
}

func (f *fakeAdmission) Reject(_ context.Context, code string) error {
	if f.fail != nil {
		return f.fail
	}

	for _, item := range f.waiting {
		if item.Code == code {
			f.rejected = append(f.rejected, code)

			return nil
		}
	}

	return fmt.Errorf("%w: %s", admission.ErrUnknownCode, code)
}

// TestADecisionReachesTheConnectorHoldingTheCode is what makes the code the
// whole address. A deployment with two connectors has two tables, and a caller
// who read a code off a machine does not know which of them minted it.
func TestADecisionReachesTheConnectorHoldingTheCode(t *testing.T) {
	t.Parallel()

	first := &fakeAdmission{waiting: []admission.Registration{{Code: "AAAA", Serial: "SN-1"}}}
	second := &fakeAdmission{waiting: []admission.Registration{{Code: "BBBB", Serial: "SN-2"}}}

	surface := newRegistrations([]admissionTable{
		{ref: connectorapi.Ref("second"), driver: second},
		{ref: connectorapi.Ref("first"), driver: first},
	}, nil)

	waiting, err := surface.Pending(t.Context())
	require.NoError(t, err)
	require.Equal(t, []httpapi.Registration{
		{Code: "AAAA", Connector: "first", Serial: "SN-1"},
		{Code: "BBBB", Connector: "second", Serial: "SN-2"},
	}, waiting, "the listing is not in connector order, so two readings can disagree")

	approved, err := surface.Approve(t.Context(), "BBBB")
	require.NoError(t, err)
	require.Equal(t, "second", approved.Connector)
	require.True(t, approved.Approved)
	require.Equal(t, []string{"BBBB"}, second.approved)
	require.Empty(t, first.approved, "the wrong connector was asked to approve")

	require.NoError(t, surface.Reject(t.Context(), "AAAA"))
	require.Equal(t, []string{"AAAA"}, first.rejected)
	require.Empty(t, second.rejected, "the wrong connector was asked to reject")
}

// TestACodeNoTableHoldsIsTheVocabularysOwnRefusal is what the transport reads to
// answer 404. A walk that ran out of tables must report the same sentinel a
// single table would, or the refusal arrives as this server's own failure.
func TestACodeNoTableHoldsIsTheVocabularysOwnRefusal(t *testing.T) {
	t.Parallel()

	surface := newRegistrations([]admissionTable{
		{ref: connectorapi.Ref("only"), driver: &fakeAdmission{}},
	}, nil)

	_, err := surface.Approve(t.Context(), "NOSUCHCODE")
	require.ErrorIs(t, err, admission.ErrUnknownCode)

	require.ErrorIs(t, surface.Reject(t.Context(), "NOSUCHCODE"), admission.ErrUnknownCode)
}

// TestAConnectorThatCouldNotDecideIsNotReadAsAnUnknownCode holds the other
// branch. A table that failed for its own reasons must not be walked past, or a
// caller is told the code does not exist when it does.
func TestAConnectorThatCouldNotDecideIsNotReadAsAnUnknownCode(t *testing.T) {
	t.Parallel()

	broken := errors.New("the connector is not answering")

	surface := newRegistrations([]admissionTable{
		{ref: connectorapi.Ref("broken"), driver: &fakeAdmission{fail: broken}},
	}, nil)

	_, err := surface.Approve(t.Context(), "AAAA")
	require.ErrorIs(t, err, broken)
	require.NotErrorIs(t, err, admission.ErrUnknownCode)

	require.ErrorIs(t, surface.Reject(t.Context(), "AAAA"), broken)
}

// TestTheShippingDialectReachesTheOperatorSurface is the wiring. The default
// connector holds a registration table, so a deployment that declares nothing
// still publishes what is waiting to be let in.
func TestTheShippingDialectReachesTheOperatorSurface(t *testing.T) {
	t.Parallel()

	built, err := mountConnectors(t.Context(), config.Default(), "", discard(), newPlane(t), nil, nil, "")
	require.NoError(t, err)

	require.Len(t, built.admissions, 1,
		"the shipping dialect reached no operator surface, so nothing can approve a printer")
	require.Equal(t, connectorapi.Ref(builtin.ShippingConnector()), built.admissions[0].ref)

	waiting, err := newRegistrations(built.admissions, nil).Pending(t.Context())
	require.NoError(t, err)
	require.Empty(t, waiting, "a driver nothing has registered with reported a device waiting")
}
