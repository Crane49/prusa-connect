package authz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// theNamespace is what extension.NamespaceOf derives for github.com/acme/mk4.
const theNamespace = "com.github.aux.acme.mk4"

// contributed is one kind an extension under this namespace contributes.
func contributed(tb testing.TB, namespace string) Object {
	tb.Helper()

	id, err := ContributedKind(namespace, "spool")
	require.NoError(tb, err)

	object, err := NewObject(TypeResourceKind, id)
	require.NoError(tb, err)

	return object
}

// TestAContributedKindMayNotTakeACoreKindsName stops the id space from having
// two owners for one name.
func TestAContributedKindMayNotTakeACoreKindsName(t *testing.T) {
	t.Parallel()

	for _, kind := range ref.Kinds() {
		_, err := ContributedKind(theNamespace, kind.String())
		requireRefusal(t, err, "may not take the name of a core one")
	}
}

// TestAContributedKindRefusesAMalformedNamespace holds the grammar the host
// derives. The namespace is never chosen by the extension, so every case here
// is a host bug rather than an author's, and it fails loudly for that reason.
func TestAContributedKindRefusesAMalformedNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
	}{
		{name: "empty", namespace: ""},
		{name: "core", namespace: "core"},
		{name: "under core", namespace: "core.printer"},
		{name: "one label", namespace: "acme"},
		{name: "uppercase label", namespace: "com.github.aux.Acme.mk4"},
		{name: "empty label", namespace: "com.github..mk4"},
		{name: "slash", namespace: "com.github/acme"},
		{name: "over the length limit", namespace: "com.github.aux.acme." + strings.Repeat("a", 80)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ContributedKind(tc.namespace, "spool")
			requireRefusal(t, err, "namespace")
		})
	}
}

// TestAContributedKindIsNotAConfigurableInstance is the boundary between the
// two id spaces. A contributed kind is an authorization object and nothing
// else: no lifecycle plane switch reads it, and the resource surface refuses to
// register an instance of one.
func TestAContributedKindIsNotAConfigurableInstance(t *testing.T) {
	t.Parallel()

	id, err := ContributedKind(theNamespace, "spool")
	require.NoError(t, err)

	require.False(t, ref.Kind(id).Valid())

	_, err = ref.New(ref.Kind(id), "reel-01")
	require.ErrorIs(t, err, ref.ErrUnknownKind)
}

// TestOnlyAResourceKindTakesAQualifiedID is the containment on the widened id
// grammar. NewObject accepts <namespace>/<name> for one type, so a slash in any
// other object id is still refused and no second id space opens by accident.
func TestOnlyAResourceKindTakesAQualifiedID(t *testing.T) {
	t.Parallel()

	id, err := ContributedKind(theNamespace, "spool")
	require.NoError(t, err)

	for _, typ := range Types() {
		object, buildErr := NewObject(typ, id)

		if typ == TypeResourceKind {
			require.NoError(t, buildErr)
			require.Equal(t, "resource_kind:"+id, object.String())

			parsed, parseErr := ParseObject(object.String())
			require.NoError(t, parseErr)
			require.Equal(t, object, parsed)

			continue
		}

		require.Errorf(t, buildErr, "NewObject(%q, %q) was accepted", typ, id)
	}
}

// TestAContributedKindTakesAGrantOnTheRealGraph is the half that proves the
// object is usable rather than merely constructible. It writes a manager tuple
// on a contributed kind through the store a server runs, on every engine the
// build can reach.
func TestAContributedKindTakesAGrantOnTheRealGraph(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		object := contributed(t, theNamespace)

		grant, err := NewTuple(object, RelManager, subjectOf(t, "user:ada"))
		require.NoError(t, err)

		require.NoError(t, store.Write(t.Context(), grant))

		stored, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.Contains(t, stored, grant)
	})
}

// TestNoCredentialReachesAContributedKindThroughTheGrantSurface is the security
// property of the widened id grammar. NewObject now accepts a qualified
// resource_kind id, so the question is who can write a grant on one.
//
// Nobody can, and two things stop it. Bootstrap writes the org tuple for each
// of ref.ConfigurableKinds and for no contributed kind, and the projection is
// compiled over that same set, so a contributed kind gets no resource_access
// row either way. This case writes the org tuple itself, which is the tuple a
// future registration writes, so the projection's id space is the only barrier
// left standing and the case fails if it moves.
func TestNoCredentialReachesAContributedKindThroughTheGrantSurface(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		owner := subjectOf(t, "user:ada")

		_, err := store.Bootstrap(t.Context(), "acme", owner)
		require.NoError(t, err)

		// The arming half. Without it a store that refuses every grant passes.
		core := kindOf(t, ref.KindStorage)
		onCore, err := NewTuple(core, RelManager, subjectOf(t, "user:grace"))
		require.NoError(t, err)

		written, err := store.WriteGrant(t.Context(), owner, onCore)
		require.NoError(t, err, "the org admin administers a core kind")
		require.True(t, written)

		own := contributed(t, theNamespace)

		tenant, err := NewSubject(TypeOrg, "acme")
		require.NoError(t, err)

		reach, err := NewTuple(own, RelOrg, tenant)
		require.NoError(t, err)
		require.NoError(t, store.Write(t.Context(), reach))

		onOwn, err := NewTuple(own, RelManager, subjectOf(t, "user:grace"))
		require.NoError(t, err)

		_, err = store.WriteGrant(t.Context(), owner, onOwn)
		require.Error(t, err, "the org admin reached a kind the projection does not compile")
	})
}

// requireRefusal holds every refusal on this path to one published code and to
// the correction its message carries. A case that asserted the error alone
// would still pass with its own branch gone, because a later check refuses the
// same input for a different reason.
func requireRefusal(tb testing.TB, err error, detail string) {
	tb.Helper()

	require.Error(tb, err)

	var f *fault.Fault

	require.ErrorAs(tb, err, &f)
	require.Equal(tb, "core.auth/DECLARATION_REFUSED", f.Code().String())
	require.ErrorContains(tb, err, detail)
}
