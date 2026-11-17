package authz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// printerDirectory is the directory rows a compiler test needs, every one a
// printer. project reads the kind because ADR-20260831-c7b rule 12's mapping from a
// declared permission name onto core action bits is per device kind, and a test
// that says nothing about a kind means a printer: that is what every device in
// these fixtures is.
func printerDirectory(ids ...string) []directoryRow {
	out := make([]directoryRow, 0, len(ids))
	for _, id := range ids {
		out = append(out, directoryRow{id: id, kind: ref.KindPrinter.String()})
	}

	return out
}

// mustTuple reads the notation of the Zanzibar paper and of the research document,
// "object#relation@subject", so a fixture in a test reads the same way as the
// tuples in earlier research.
func mustTuple(tb testing.TB, text string) Tuple {
	tb.Helper()

	objectText, rest, ok := strings.Cut(text, relationSeparator)
	require.Truef(tb, ok, "tuple %q has no #relation", text)

	relation, subjectText, ok := strings.Cut(rest, "@")
	require.Truef(tb, ok, "tuple %q has no @subject", text)

	object, err := ParseObject(objectText)
	require.NoErrorf(tb, err, "tuple %q", text)

	subject, err := ParseSubject(subjectText)
	require.NoErrorf(tb, err, "tuple %q", text)

	out, err := NewTuple(object, Relation(relation), subject)
	require.NoErrorf(tb, err, "tuple %q", text)

	return out
}

func mustTuples(tb testing.TB, texts ...string) []Tuple {
	tb.Helper()

	out := make([]Tuple, 0, len(texts))
	for _, text := range texts {
		out = append(out, mustTuple(tb, text))
	}

	return out
}

// subjectOf is the principal a case is about.
func subjectOf(tb testing.TB, text string) Subject {
	tb.Helper()

	out, err := ParseSubject(text)
	require.NoErrorf(tb, err, "subject %q", text)

	return out
}

func deviceOf(tb testing.TB, id string) Object {
	tb.Helper()

	out, err := NewObject(TypeDevice, id)
	require.NoError(tb, err)

	return out
}

// kindOf is the object a resource grant is written on.
func kindOf(tb testing.TB, kind ref.Kind) Object {
	tb.Helper()

	out, err := NewObject(TypeResourceKind, kind.String())
	require.NoError(tb, err)

	return out
}

// instanceOf is one configured instance, the thing a handler holds.
func instanceOf(tb testing.TB, kind ref.Kind, id string) ref.Ref {
	tb.Helper()

	out, err := ref.New(kind, id)
	require.NoError(tb, err)

	return out
}

// fixtureDirectory is every device the server knows about. p-hidden is
// deliberately absent: it has grants and must still be invisible.
//
// cam-alpha is a camera bound to a printer, written the way cameraSettings
// writes one: a parent edge and no placement of its own. It is here so that the
// sweeping tests cover ADR-20260826-5yj rule 15 at all, and so that they cover it on
// the shape the product builds, where every path to the device runs through the
// edge and there is no org tuple to fall back on.
var fixtureDirectory = []DeviceRow{
	{ID: "p-alpha", Name: "Alpha"},
	{ID: "p-beta", Name: "Beta"},
	{ID: "p-gamma", Name: "Gamma"},
	{ID: "p-delta", Name: "Delta"},
	{ID: "cam-alpha", Name: "Alpha camera", Kind: ref.DeviceKindCamera},
}

// fixtureResources is every configured instance the server knows about.
// sink:orphan is deliberately absent: its kind is granted and it must still be
// invisible, which is the resource half of what p-hidden proves for printers.
func fixtureResources(tb testing.TB) []ref.Ref {
	tb.Helper()

	return []ref.Ref{
		instanceOf(tb, ref.KindStorage, "local"),
		instanceOf(tb, ref.KindStorage, "s3-main"),
		instanceOf(tb, ref.KindSink, "webhook"),
		instanceOf(tb, ref.KindCamera, "front"),
	}
}

// fixtureTuples is one small fleet covering every path a grant can take.
//
//	alice   operates floor-one through a team, and views floor-two through a
//	        team nested inside another team
//	bob     views floor-two only
//	dana    holds one direct grant, plus one on an unregistered printer
//	olga    is an org viewer with one printer blocked
//	sa-ui   is a service account scoped to three printers
//	site-b  is a peer node with one printer
//	root    is an org admin
//	mallory holds nothing at all
//
//	sink:audit            is a fleet-wide sink: one org#viewer tuple, and it
//	                      reaches every printer in the org without a tuple per
//	                      printer
//	service_account:audit is a service account of exactly that name, holding
//	                      manager on one printer. It shares sink:audit's id and
//	                      nothing else: neither may hold what the other was
//	                      granted, and the sweeps decide that on every device
//	                      and every action
var fixtureTuples = []string{
	// structure
	"device:p-alpha#org@org:acme",
	"device:p-alpha#group@device_group:cell-a",
	"device:p-alpha#agent@device_credential:sn-alpha",
	"device:p-beta#org@org:acme",
	"device:p-beta#group@device_group:floor-one",
	"device:p-gamma#org@org:acme",
	"device:p-gamma#group@device_group:floor-two",
	"device:p-delta#org@org:acme",
	"device:p-hidden#org@org:acme",
	// A camera inside a printer, and no org on the camera: whoever reaches
	// p-alpha reaches it, and nobody else does.
	"device:cam-alpha#parent@device:p-alpha",
	"device_group:cell-a#parent@device_group:floor-one",
	"device_group:cell-a#org@org:acme",
	"device_group:floor-one#org@org:acme",
	"device_group:floor-two#org@org:acme",

	// people
	"group:maintenance#member@user:alice",
	"group:all-staff#member@group:maintenance#member",
	"group:all-staff#member@user:bob",

	// grants
	"device_group:floor-one#operator@group:maintenance#member",
	"device_group:floor-two#viewer@group:all-staff#member",
	"device:p-delta#viewer@user:dana",
	"device:p-hidden#manager@user:dana",
	"org:acme#viewer@user:olga",
	"device:p-gamma#blocked@user:olga",
	"device:p-alpha#operator@service_account:sa-ui",
	"device:p-beta#operator@service_account:sa-ui",
	"device:p-delta#operator@service_account:sa-ui",
	"device:p-gamma#viewer@node:site-b",
	"org:acme#admin@user:root",

	// The fleet-wide sink, and a service account sharing its id. One org tuple
	// is the whole of what an operator writes to feed a sink the fleet; the
	// manager grant beside it belongs to the service account alone and must not
	// follow the name across.
	"org:acme#viewer@sink:audit",
	"device:p-alpha#manager@service_account:audit",

	// the configuration surface. Two kinds sit in the org and two do not, so
	// an org role reaching a kind is tenancy and not a blanket.
	"resource_kind:storage#org@org:acme",
	"resource_kind:sink#org@org:acme",
	"resource_kind:storage#manager@user:dana",
	"resource_kind:storage#blocked@user:olga",
	"resource_kind:sink#operator@group:maintenance#member",
	"resource_kind:sink#viewer@node:site-b",
	"resource_kind:camera#viewer@user:bob",
}

// fixturePrincipals is every subject the sweeping tests iterate, including one
// that holds nothing.
var fixturePrincipals = []string{
	"user:alice", "user:bob", "user:dana", "user:olga", "user:root", "user:mallory",
	"service_account:sa-ui", "device_credential:sn-alpha", "device_credential:sn-gamma", "node:site-b",
	"sink:audit", "service_account:audit",
}

// seedFixture returns a store holding the fixture graph.
func seedFixture(tb testing.TB, e engine, options ...Option) *Store {
	tb.Helper()

	store := newStore(tb, e, options...)
	require.NoError(tb, store.UpsertDevices(tb.Context(), fixtureDirectory...))
	require.NoError(tb, store.RegisterResources(tb.Context(), fixtureResources(tb)...))
	require.NoError(tb, store.Write(tb.Context(), mustTuples(tb, fixtureTuples...)...))

	return store
}

// resourceUniverse is every instance the sweeping tests compare the surfaces
// over: the registered ones and one that is granted but not registered.
func resourceUniverse(tb testing.TB) []ref.Ref {
	tb.Helper()

	return append(fixtureResources(tb), instanceOf(tb, ref.KindSink, "orphan"))
}

// directoryIDs returns every registered printer id, the universe the sweeping
// tests compare the two enforcement surfaces over.
func directoryIDs() []string {
	out := make([]string, 0, len(fixtureDirectory))
	for _, row := range fixtureDirectory {
		out = append(out, row.ID)
	}

	return out
}

// requireCode asserts the error carries a registered code, which is what a
// transport branches on.
func requireCode(tb testing.TB, err error, code fault.Code) {
	tb.Helper()

	require.Error(tb, err)
	require.Truef(tb, fault.HasCode(err, code), "want code %s, got %v", code, err)
}

// deviceIDsOf is the ids of a directory read, for a test that cares about which
// devices are there and not about what kind each one is.
func deviceIDsOf(rows []directoryRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.id)
	}

	return out
}
