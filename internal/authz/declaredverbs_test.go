package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The shelf is the thing that could not be done before this record: a device
// class from outside this tree, with verbs of its own, that dispatch, are
// checked and are grantable with no edit to the core's verb table.
//
// It is written out here rather than composed, because what this file is about
// is what the graph does with a declaration and not how one reaches it. The
// composition gate is where a module this repository does not compile is built
// into a binary.
const (
	shelfModule = "github.com/acme/prusa-adapter-shelf"
	shelfKind   = DeviceKind("acme.io/filament-shelf")

	shelfAttention = "acme.io.filament-shelf.command.set_slot_attention"
	shelfCalibrate = "acme.io.filament-shelf.command.calibrate_scale"
	shelfReset     = "acme.io.filament-shelf.command.factory_reset"
)

// shelfDeclaration is the adapter's own declaration, as the host hands it to
// the graph. factory_reset is marked by the adapter; the other two are not.
func shelfDeclaration() KindDeclaration {
	return KindDeclaration{
		Module:      shelfModule,
		Kind:        shelfKind,
		DisplayName: "Filament shelf",
		Verbs: map[string]DeclaredVerb{
			shelfAttention: {Class: ActionOperate, Markings: map[Marking]Marker{}},
			shelfCalibrate: {Class: ActionConfigure, Markings: map[Marking]Marker{}},
			// opaque_payload is the host's to compute from the descriptor and
			// never a declaration's to claim, so it is deliberately absent.
			//nolint:exhaustive // an adapter marks irreversible and nothing else
			shelfReset: {Class: ActionConfigure, Markings: map[Marking]Marker{
				MarkingIrreversible: MarkerAdapter,
			}},
		},
	}
}

// verbTableHolder is the process's verb table, handed to one test at a time.
//
// The table is one value for the whole process and forEachEngine runs its
// engine subtests in parallel. Two subtests that install a declaration would
// install over each other, and the first to finish would put the generated
// table back while the other is still running. Every check the other makes then
// answers for a verb the table no longer names: a verb stops being dangerous
// mid-write, so the write stores its row, the recompile projects nothing for
// it, and the read after the write refuses.
//
// A buffered channel rather than a mutex, because a test can ask a channel
// whether the table is taken without blocking on it.
//
//nolint:gochecknoglobals // the process's table is global, so what guards it is too
var verbTableHolder = make(chan struct{}, 1)

// holdVerbTable gives this test the process's verb table until the test ends.
// The cleanup puts the generated table back and only then hands the table on,
// so the next test waits rather than racing.
func holdVerbTable(tb testing.TB) {
	tb.Helper()

	verbTableHolder <- struct{}{}

	tb.Cleanup(func() {
		ResetVerbTable()
		<-verbTableHolder
	})
}

// installShelf puts the declaration in force for one test and takes it out
// again, so a table one case installed does not leak into the next.
func installShelf(tb testing.TB, store *Store, declaration KindDeclaration) KindReport {
	tb.Helper()

	holdVerbTable(tb)

	// The acceptance route takes the grant that administers this server's
	// driver configuration, because a driver is what declares a device kind.
	// The fixture places two kinds in the org and not this one.
	require.NoError(tb, store.Write(tb.Context(),
		mustTuple(tb, "resource_kind:driver#org@org:acme")))

	report, err := store.InstallKindDeclarations(tb.Context(), declaration)
	require.NoError(tb, err)

	return report
}

// A test that installed a declaration holds the process's verb table until it
// ends. This is the regression case for a flake that reached CI.
//
// The engine subtests run in parallel and the table is one value for the whole
// process. The engine that finished first put the generated table back while
// the other engine was still running. A dangerous verb then stopped being
// dangerous in the middle of a write. WriteVerbGrant stored its row and
// answered true, the recompile in the same transaction projected nothing for
// that row, and the CanVerb after it refused.
//
// Both halves are checked: the declaration is in force, and the table is taken,
// which is what keeps the sibling engine out.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestAnInstalledVerbTableIsHeldForTheWholeTest(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		installShelf(t, store, shelfDeclaration())

		require.True(t, Declared(shelfKind, shelfReset),
			"the installed declaration is not in force")

		// Taking the table here must fail. A test that can take it while this
		// one has a declaration installed is a test that can reset it here.
		select {
		case verbTableHolder <- struct{}{}:
			<-verbTableHolder
			t.Fatal("the verb table is free while a declaration is installed")
		default:
		}
	})
}

// TestADeclaredVerbClassifiesOnItsOwnKindAndNowhereElse is the requirement, at
// the graph: the adapter's verbs carry a class, so a caller holding that class
// on a shelf may send them, and they exist for no other kind.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestADeclaredVerbClassifiesOnItsOwnKindAndNowhereElse(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		installShelf(t, store, shelfDeclaration())

		require.Equal(t, ActionOperate, ActionForVerb(shelfKind, shelfAttention))
		require.Equal(t, ActionConfigure, ActionForVerb(shelfKind, shelfCalibrate))

		// The shelf's verb does not exist for kind printer, and no global
		// profile leaks it there.
		require.Empty(t, ActionForVerb(DeviceKindPrinter, shelfAttention),
			"a shelf verb classifies on a printer")
		require.Empty(t, ActionForVerb(DeviceKindCamera, shelfAttention),
			"a shelf verb classifies on a camera")

		// And the printer's verbs do not exist on the shelf.
		require.Empty(t, ActionForVerb(shelfKind, "core.command.start_print"),
			"a printer verb classifies on a shelf")

		// Stage one is the union of both halves: the verb is known to this
		// build, whatever kind the caller names.
		require.True(t, KnownVerb(shelfAttention), "the shelf's verb is outside the published union")
		require.True(t, KnownVerb("core.command.start_print"))
		require.False(t, KnownVerb("acme.io.filament-shelf.command.melt"))
	})
}

// The suggestion comes from the same union stage one reads, so a caller who
// misspells a shelf verb against a printer id is told the shelf's verb. That is
// deliberate: the union is published to a caller holding no credential, so the
// suggestion discloses nothing the vocabulary route would not.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestCanonicalVerbSuggestsFromTheWholeUnion(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		installShelf(t, store, shelfDeclaration())

		got, named := CanonicalVerb("ACME.IO.FILAMENT-SHELF.COMMAND.SET_SLOT_ATTENTION")
		require.True(t, named)
		require.Equal(t, shelfAttention, got)

		_, named = CanonicalVerb("acme.io.filament-shelf.command.melt")
		require.False(t, named, "CanonicalVerb invented a verb")
	})
}

// A declaration is a request the host validates and never a fact. The three
// names beside the class set are refused, and so is anything outside it.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestNoDeclarationMayCarryAReadActionOrAdminister(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		// Every declaration below is refused, so the table should not move. It
		// is still held for the whole case, because the sibling engine subtest
		// installs one and the reads here would otherwise see it.
		holdVerbTable(t)

		for _, class := range []Action{
			ActionObserve, ActionViewAudit, ActionAdminister,
			ActionReportTelemetry, ActionFetchJob, ActionViewResource, Action("king"),
		} {
			declaration := shelfDeclaration()
			declaration.Verbs[shelfAttention] = DeclaredVerb{Class: class}

			report, err := store.InstallKindDeclarations(t.Context(), declaration)
			require.NoError(t, err, "a refused declaration stopped the boot")
			require.Containsf(t, report.Refused, shelfKind,
				"a verb declaring %s was installed", class)
			require.Emptyf(t, ActionForVerb(shelfKind, shelfAttention),
				"a verb declaring %s classifies", class)
		}
	})
}

// A dangerous verb is conferred by no class, role or level. The row is what
// reaches it, and ownership is what makes the first row writable.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestADangerousVerbTakesItsOwnRow(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		installShelf(t, store, shelfDeclaration())

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "shelf-one", Name: "Shelf one", Kind: shelfKind}))
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:shelf-one#org@org:acme"),
			mustTuple(t, "device:shelf-one#owner@user:dana")))

		shelf := deviceOf(t, "shelf-one")
		root := subjectOf(t, "user:root")
		dana := subjectOf(t, "user:dana")

		// The class is held by the org admin, and the class is not the verb.
		allowed, err := store.Can(t.Context(), root, ActionConfigure, shelf)
		require.NoError(t, err)
		require.True(t, allowed, "an org admin does not hold configure on a device in the org")

		reached, err := store.CanVerb(t.Context(), root, shelf, shelfReset)
		require.NoError(t, err)
		require.False(t, reached, "a marked verb was conferred by a level")

		// The unmarked verb of the same class is conferred by the class.
		reached, err = store.CanVerb(t.Context(), root, shelf, shelfCalibrate)
		require.NoError(t, err)
		require.True(t, reached, "an unmarked verb was refused by the tier")

		// Ownership reaches every dangerous verb of the owned device's kind,
		// computed and not stored.
		reached, err = store.CanVerb(t.Context(), dana, shelf, shelfReset)
		require.NoError(t, err)
		require.True(t, reached, "the owner does not reach the dangerous verb on their own machine")

		// And the owner is who can hand it out. So is an admin of an org the
		// device is in: the tier would otherwise have no bootstrap at all,
		// because ownership gives the reach without administer and every other
		// route gives administer without the reach.
		grant, err := NewVerbGrant(shelf, shelfReset, subjectOf(t, "user:bob"))
		require.NoError(t, err)

		// A manager of the device who neither owns it nor administers its org
		// holds administer and no reach, and confers nothing.
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:shelf-one#manager@user:mo")))

		_, err = store.WriteVerbGrant(t.Context(), subjectOf(t, "user:mo"), grant)
		require.Error(t, err, "a delegated manager conferred a verb it does not itself reach")

		wrote, err := store.WriteVerbGrant(t.Context(), dana, grant)
		require.NoError(t, err)
		require.True(t, wrote)

		reached, err = store.CanVerb(t.Context(), subjectOf(t, "user:bob"), shelf, shelfReset)
		require.NoError(t, err)
		require.True(t, reached, "the row conferred nothing")

		// Revocation is a deleted row.
		require.NoError(t, store.WithdrawVerbGrant(t.Context(), dana, grant))

		reached, err = store.CanVerb(t.Context(), subjectOf(t, "user:bob"), shelf, shelfReset)
		require.NoError(t, err)
		require.False(t, reached, "a withdrawn row still confers the verb")
	})
}

// First sight is pinned per (module path, kind, verb), so a rebuild that lowers
// a class does not widen every grant already written against the higher one.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestALoweredClassDoesNotApplyUntilItIsAccepted(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		installShelf(t, store, shelfDeclaration())

		require.Equal(t, ActionConfigure, ActionForVerb(shelfKind, shelfCalibrate))

		// The rebuild: v2 quietly moves calibrate_scale from configure to
		// operate, which every operator on the fleet already holds.
		lowered := shelfDeclaration()
		lowered.Verbs[shelfCalibrate] = DeclaredVerb{Class: ActionOperate}

		report, err := store.InstallKindDeclarations(t.Context(), lowered)
		require.NoError(t, err)
		require.Empty(t, report.Refused, "a lowered class refused the kind instead of holding the verb")
		require.NotEmpty(t, report.Held, "a lowered class applied without being named")
		require.Contains(t, report.Held[0], shelfCalibrate)

		require.Equal(t, ActionConfigure, ActionForVerb(shelfKind, shelfCalibrate),
			"the recorded class did not stand against a rebuild that lowered it")

		// Raising applies silently, because reach only narrows.
		raised := shelfDeclaration()
		raised.Verbs[shelfAttention] = DeclaredVerb{Class: ActionConfigure}

		_, err = store.InstallKindDeclarations(t.Context(), raised)
		require.NoError(t, err)
		require.Equal(t, ActionConfigure, ActionForVerb(shelfKind, shelfAttention),
			"a raised class was held, and reach only narrows")

		// Accepted by name, it applies from the next start.
		require.NoError(t, store.AcceptDeclarationChange(t.Context(),
			subjectOf(t, "user:root"), shelfModule, shelfKind, shelfCalibrate, ActionOperate.String()))

		_, err = store.InstallKindDeclarations(t.Context(), lowered)
		require.NoError(t, err)
		require.Equal(t, ActionOperate, ActionForVerb(shelfKind, shelfCalibrate),
			"an accepted change did not apply")
	})
}

// A marking the record holds and a rebuild removes stands: nothing but the
// marker clears a marking, and a rebuild is not a marker.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestARemovedMarkingDoesNotApply(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		installShelf(t, store, shelfDeclaration())

		require.True(t, Dangerous(shelfKind, shelfReset))

		unmarked := shelfDeclaration()
		unmarked.Verbs[shelfReset] = DeclaredVerb{Class: ActionConfigure}

		report, err := store.InstallKindDeclarations(t.Context(), unmarked)
		require.NoError(t, err)
		require.NotEmpty(t, report.Held)
		require.True(t, Dangerous(shelfKind, shelfReset),
			"a rebuild cleared a marking it did not make")
	})
}

// A kind that reappears from a different module path than first seen is refused
// outright. Two publishers do not share a kind, and the refusal is not
// permanent: an operator accepts a legitimate rename through the acceptance
// route, which moves the record and never a pin.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestAKindFromASecondModuleIsRefusedUntilTheRenameIsAccepted(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		installShelf(t, store, shelfDeclaration())

		imposter := shelfDeclaration()
		imposter.Module = "github.com/someone-else/shelf"

		report, err := store.InstallKindDeclarations(t.Context(), imposter)
		require.NoError(t, err)
		require.Contains(t, report.Refused, shelfKind,
			"a second publisher took over a kind this deployment already knows")
		require.False(t, KnownKind(shelfKind), "the refused kind is in the table")

		require.NoError(t, store.AcceptDeclarationChange(t.Context(),
			subjectOf(t, "user:root"), shelfModule, shelfKind, "", imposter.Module))

		report, err = store.InstallKindDeclarations(t.Context(), imposter)
		require.NoError(t, err)
		require.Empty(t, report.Refused, "an accepted rename was still refused")

		// The pin moved with the record, so the class is still what was first
		// seen and not what the renamed module says.
		lowered := imposter
		lowered.Verbs = map[string]DeclaredVerb{
			shelfCalibrate: {Class: ActionOperate},
			shelfAttention: {Class: ActionOperate},
			shelfReset:     {Class: ActionConfigure},
		}

		_, err = store.InstallKindDeclarations(t.Context(), lowered)
		require.NoError(t, err)
		require.Equal(t, ActionConfigure, ActionForVerb(shelfKind, shelfCalibrate),
			"acceptance of a module rename shed a pin")
	})
}

// A row for a verb this build no longer classifies confers nothing, and it is
// refused before the row is read. That is rule 20's second failure, and it is
// what makes a rebuild that dropped a verb safe rather than quiet.
// parallel would each be reading the other's declaration.
//
//nolint:tparallel,paralleltest // installs the process's verb table; two of these in
func TestARowForAVerbThatStoppedClassifyingIsInert(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		installShelf(t, store, shelfDeclaration())

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "shelf-two", Name: "Shelf two", Kind: shelfKind}))
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:shelf-two#org@org:acme"),
			mustTuple(t, "device:shelf-two#owner@user:dana"),
			mustTuple(t, "device:shelf-two#manager@user:dana")))

		shelf := deviceOf(t, "shelf-two")
		bob := subjectOf(t, "user:bob")

		grant, err := NewVerbGrant(shelf, shelfReset, bob)
		require.NoError(t, err)

		_, err = store.WriteVerbGrant(t.Context(), subjectOf(t, "user:dana"), grant)
		require.NoError(t, err)

		reached, err := store.CanVerb(t.Context(), bob, shelf, shelfReset)
		require.NoError(t, err)
		require.True(t, reached)

		// The rebuild drops the verb.
		dropped := shelfDeclaration()
		delete(dropped.Verbs, shelfReset)

		_, err = store.InstallKindDeclarations(t.Context(), dropped)
		require.NoError(t, err)

		reached, err = store.CanVerb(t.Context(), bob, shelf, shelfReset)
		require.NoError(t, err)
		require.False(t, reached, "a row for a verb nothing classifies still conferred it")

		// The row is still listed, so an operator can see it and delete it.
		rows, err := store.VerbGrants(t.Context(), subjectOf(t, "user:dana"), shelf)
		require.NoError(t, err)
		require.Len(t, rows, 1, "an inert row was hidden rather than shown")
	})
}
