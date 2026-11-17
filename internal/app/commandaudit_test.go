package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// auditedPast is two printers, three principals with three different audit
// grants over them, and a store holding one state fact and one audit fact for
// each machine.
//
// The rows are written for both machines whatever any caller may read, because
// the property under test is what a query answers and not what a store holds:
// a fixture that stored only what the caller was allowed would pass whether or
// not the restriction happened.
//
// The grants are the ones this build resolves audit sight through. ADR-20260901-b2z
// rule 3 defines view_audit and confers it at manager; until that action exists
// the seam at internal/app/audit.go checks manage, which enters the device mask
// at manager level and nowhere lower, so a manager here is a principal that
// holds audit sight and a viewer is one that does not.
func auditedPast(t *testing.T) (reader historyReader, viewer, manager, mixed authz.Subject) {
	t.Helper()

	graph := newGraph(t)

	owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
	require.NoError(t, err)

	viewer, err = authz.NewSubject(humanSubjectType, "vera")
	require.NoError(t, err)

	manager, err = authz.NewSubject(humanSubjectType, "mona")
	require.NoError(t, err)

	mixed, err = authz.NewSubject(humanSubjectType, "milo")
	require.NoError(t, err)

	for _, printer := range []string{alphaPrinter, betaPrinter} {
		require.NoError(t, graph.UpsertDevice(t.Context(), printer, printer))
		require.NoError(t, graph.Write(t.Context(), tuple(t,
			authz.TypeDevice, printer, authz.RelOrg, "org:"+bootstrapOrg)))
	}

	// vera sees both machines and audits neither. mona manages both. milo
	// manages alpha and only views beta, which is the mixed grant F2 exists
	// for.
	for _, grant := range []struct {
		printer  string
		relation authz.Relation
		who      authz.Subject
	}{
		{alphaPrinter, authz.RelViewer, viewer},
		{betaPrinter, authz.RelViewer, viewer},
		{alphaPrinter, authz.RelManager, manager},
		{betaPrinter, authz.RelManager, manager},
		{alphaPrinter, authz.RelManager, mixed},
		{betaPrinter, authz.RelViewer, mixed},
	} {
		wrote, grantErr := graph.WriteGrant(t.Context(), owner, tuple(t,
			authz.TypeDevice, grant.printer, grant.relation, grant.who.String()))
		require.NoError(t, grantErr)
		require.True(t, wrote)
	}

	store := newHistoryStore(t)

	facts := make([]historyapi.Fact, 0, 4)

	for i, printer := range []string{alphaPrinter, betaPrinter} {
		facts = append(facts,
			historyapi.Fact{
				At:       historyBase,
				Ref:      printerOrigin(t, printer),
				Type:     event.TypeDeviceArrived,
				Sequence: uint64(2*i + 1),
				Epoch:    "epoch-a",
				Payload:  json.RawMessage(`{"device_id":"` + printer + `"}`),
				Class:    event.ClassState,
			},
			historyapi.Fact{
				At:       historyBase,
				Ref:      printerOrigin(t, printer),
				Type:     event.TypeDeviceCommandAuthorized,
				Sequence: uint64(2*i + 2),
				Epoch:    "epoch-a",
				Payload: json.RawMessage(
					`{"command_id":"c-` + printer + `","verb":"core.command.send_info",` +
						`"auth_type":"user","auth_id":"operator"}`),
				Class: event.ClassAudit,
			})
	}

	require.NoError(t, store.AppendFacts(t.Context(), facts))

	return historyReader{feed: oneProvider{provider: store}, seen: visibility{graph: graph}}, viewer, manager, mixed
}

// typesOf renders one page as the set of event types it carried.
func typesOf(page httpapi.HistoryFactPage) map[string]int {
	seen := make(map[string]int, len(page.Items))
	for _, item := range page.Items {
		seen[item.Type]++
	}

	return seen
}

// TestAnAuditFactIsUnreadableWithoutAuditSight is the history half of the
// equivalence gate for ADR-20260827-wgc rule 11. The caller asks for the audit type by
// name, which is the query that leaked before rules 12 and 13: a fact query
// reduces by origin alone and the fact path stores every audit frame.
func TestAnAuditFactIsUnreadableWithoutAuditSight(t *testing.T) {
	t.Parallel()

	reader, vera, mona, _ := auditedPast(t)

	refused, err := reader.Facts(t.Context(), vera.String(), httpapi.HistoryFactQuery{
		Types: []string{event.TypeDeviceCommandAuthorized},
	})
	require.NoError(t, err)
	require.Empty(t, refused.Items,
		"a caller holding no audit sight read the operator-to-command mapping")

	// The same caller still reads everything else about the same printers, so
	// the restriction is one class and not a refusal of the query.
	ordinary, err := reader.Facts(t.Context(), vera.String(), httpapi.HistoryFactQuery{})
	require.NoError(t, err)
	require.Equal(t, map[string]int{event.TypeDeviceArrived: 2}, typesOf(ordinary))

	served, err := reader.Facts(t.Context(), mona.String(), httpapi.HistoryFactQuery{
		Types: []string{event.TypeDeviceCommandAuthorized},
	})
	require.NoError(t, err)
	require.Len(t, served.Items, 2, "a caller holding audit sight was refused the audit facts")

	t.Logf("observed: the audit type answered %d rows to a viewer and %d to a manager",
		len(refused.Items), len(served.Items))
}

// TestAMixedGrantQueryYieldsNoAuditFromEitherOrigin is F2, the hole the first
// fix left. Classes is one list over a plural Refs, so a caller holding audit
// sight of one printer and not the other must read no audit facts from a query
// naming both, and must read the granted one's when it names that origin alone.
func TestAMixedGrantQueryYieldsNoAuditFromEitherOrigin(t *testing.T) {
	t.Parallel()

	reader, _, _, milo := auditedPast(t)

	both, err := reader.Facts(t.Context(), milo.String(), httpapi.HistoryFactQuery{
		HistoryWindow: httpapi.HistoryWindow{
			Origins: []event.Origin{
				printerOrigin(t, alphaPrinter), printerOrigin(t, betaPrinter),
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]int{event.TypeDeviceArrived: 2}, typesOf(both),
		"a mixed-grant query over two origins answered an audit fact")

	alone, err := reader.Facts(t.Context(), milo.String(), httpapi.HistoryFactQuery{
		HistoryWindow: httpapi.HistoryWindow{
			Origins: []event.Origin{printerOrigin(t, alphaPrinter)},
		},
	})
	require.NoError(t, err)
	require.Equal(t,
		map[string]int{event.TypeDeviceArrived: 1, event.TypeDeviceCommandAuthorized: 1},
		typesOf(alone),
		"the audit-granted origin queried alone withheld its own audit fact")

	// And the origin the grant does not cover stays closed when it is the only
	// one named.
	beta, err := reader.Facts(t.Context(), milo.String(), httpapi.HistoryFactQuery{
		HistoryWindow: httpapi.HistoryWindow{
			Origins: []event.Origin{printerOrigin(t, betaPrinter)},
		},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]int{event.TypeDeviceArrived: 1}, typesOf(beta))

	t.Logf("observed: two origins answered %v, alpha alone %v, beta alone %v",
		typesOf(both), typesOf(alone), typesOf(beta))
}

// TestTheReaderDropsAClassTheQueryExcluded is the second half of rule 13. A
// provider that ignores FactQuery.Classes must show up as missing rows rather
// than as leaked ones, so the reader drops what it was answered outside the
// restriction whatever the provider did.
func TestTheReaderDropsAClassTheQueryExcluded(t *testing.T) {
	t.Parallel()

	reader, vera, _, _ := auditedPast(t)

	// A provider that answers every row whatever the query said, wrapped around
	// the conforming one so the rows are real.
	leaking := historyReader{
		feed: oneProvider{provider: ignoringClasses{inner: reader.feed.(oneProvider).provider}},
		seen: reader.seen,
	}

	page, err := leaking.Facts(t.Context(), vera.String(), httpapi.HistoryFactQuery{})
	require.NoError(t, err)
	require.Equal(t, map[string]int{event.TypeDeviceArrived: 2}, typesOf(page),
		"a provider that ignored the class restriction leaked an audit fact through the reader")

	t.Logf("observed: a non-conforming provider answered %d rows and the reader served %d",
		4, len(page.Items))
}

// ignoringClasses is a provider that stores conformingly and answers without
// honouring FactQuery.Classes. It exists to drive the reader's own drop.
type ignoringClasses struct{ inner historyapi.Provider }

func (i ignoringClasses) Caps() historyapi.Caps { return i.inner.Caps() }

func (i ignoringClasses) AppendSamples(ctx context.Context, samples []historyapi.Sample) error {
	return i.inner.AppendSamples(ctx, samples)
}

func (i ignoringClasses) AppendFacts(ctx context.Context, facts []historyapi.Fact) error {
	return i.inner.AppendFacts(ctx, facts)
}

func (i ignoringClasses) QuerySamples(
	ctx context.Context, query historyapi.SampleQuery,
) (historyapi.SampleResult, error) {
	return i.inner.QuerySamples(ctx, query)
}

func (i ignoringClasses) QueryFacts(
	ctx context.Context, query historyapi.FactQuery,
) (historyapi.FactResult, error) {
	query.Classes = nil

	return i.inner.QueryFacts(ctx, query)
}

func (i ignoringClasses) QueryCommands(
	ctx context.Context, query historyapi.CommandQuery,
) (historyapi.CommandResult, error) {
	return i.inner.QueryCommands(ctx, query)
}

func (i ignoringClasses) Retention() []historyapi.Resolution { return i.inner.Retention() }
