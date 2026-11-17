package authz

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// forgedTuple builds a tuple by struct literal, the way a row read back from a
// table would if nothing revalidated it. NewTuple refuses every one of these;
// the point of the tests below is what happens when one exists anyway.
func forgedTuple(objectType Type, objectID string, relation Relation, subject Subject) Tuple {
	return Tuple{
		object:   Object{typ: objectType, id: objectID},
		relation: relation,
		subject:  subject,
	}
}

// TestProjectIgnoresAForgedUserset is the fail-closed half of the userset rule.
// The only userset the vocabulary has is group#member. A userset of another
// type that happens to share a group's id must resolve to nobody: resolving it
// by id alone would let one forged row hand a whole team's membership to a
// grant on an unrelated object.
func TestProjectIgnoresAForgedUserset(t *testing.T) {
	t.Parallel()

	printers := []string{"p-alpha"}

	legal := mustTuples(t,
		"group:maintenance#member@user:alice",
		"device:p-alpha#org@org:acme",
	)

	cases := []struct {
		name    string
		subject Subject
		want    bool
	}{
		{
			name:    "the vocabulary's own userset resolves",
			subject: Subject{typ: TypeGroup, id: "maintenance", rel: RelMember},
			want:    true,
		},
		{
			name:    "a printer group borrowing the group's id resolves to nobody",
			subject: Subject{typ: TypeDeviceGroup, id: "maintenance", rel: RelMember},
		},
		{
			name:    "an org borrowing the group's id resolves to nobody",
			subject: Subject{typ: TypeOrg, id: "maintenance", rel: RelMember},
		},
		{
			name:    "a node borrowing the group's id resolves to nobody",
			subject: Subject{typ: TypeNode, id: "maintenance", rel: RelMember},
		},
		{
			name:    "a group userset of another relation resolves to nobody",
			subject: Subject{typ: TypeGroup, id: "maintenance", rel: RelViewer},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tuples := append(append([]Tuple{}, legal...),
				forgedTuple(TypeDevice, "p-alpha", RelViewer, tc.subject))

			var alice bool

			for _, row := range project(tuples, printerDirectory(printers...), configurableKinds(), verbIndex{}).Devices {
				if row.Subject == (principal{typ: TypeUser, id: "alice"}) {
					alice = true
				}
			}

			require.Equal(t, tc.want, alice)
		})
	}
}

// TestProjectIgnoresAForgedMembership is the same rule one level down: a forged
// membership row whose subject is a userset of the wrong shape must not pull
// another group's members into this one.
func TestProjectIgnoresAForgedMembership(t *testing.T) {
	t.Parallel()

	tuples := append(mustTuples(t,
		"group:maintenance#member@user:alice",
		"device:p-alpha#viewer@group:all-staff#member",
	),
		forgedTuple(TypeGroup, "all-staff", RelMember,
			Subject{typ: TypeDeviceGroup, id: "maintenance", rel: RelMember}),
	)

	require.Empty(t, project(tuples, printerDirectory("p-alpha"), configurableKinds(), verbIndex{}).Devices)
}

// TestStoredTupleRefusesForgedRows covers the revalidation every row goes
// through on the way out of the table. A restored backup, an import or a
// hand-edited database is the realistic source, and the answer is to stop
// reading the graph rather than to resolve the row as whatever it resembles.
func TestStoredTupleRefusesForgedRows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		row  [6]string
	}{
		{
			name: "a userset of a type that has no members",
			row:  [6]string{"device", "p-alpha", "viewer", "device_group", "maintenance", "member"},
		},
		{
			name: "a relation the object type does not have",
			row:  [6]string{"device", "p-alpha", "admin", "user", "root", ""},
		},
		{
			name: "a service account in an org role",
			row:  [6]string{"org", "acme", "admin", "service_account", "sa-ui", ""},
		},
		{
			name: "a device granted a role",
			row:  [6]string{"device", "p-alpha", "operator", "device", "sn-alpha", ""},
		},
		{
			name: "a type outside the vocabulary",
			row:  [6]string{"robot", "r2", "viewer", "user", "alice", ""},
		},
		{
			name: "an id that is not a DNS-1123 label",
			row:  [6]string{"device", "P Alpha", "viewer", "user", "alice", ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := storedTuple(tc.row[0], tc.row[1], tc.row[2], tc.row[3], tc.row[4], tc.row[5])
			requireCode(t, err, CodeInvalidStoredTuple())
		})
	}

	t.Run("a legal row is rebuilt", func(t *testing.T) {
		t.Parallel()

		tuple, err := storedTuple("device", "p-alpha", "viewer", "group", "maintenance", "member")
		require.NoError(t, err)
		require.Equal(t, "device:p-alpha#viewer@group:maintenance#member", tuple.String())
	})
}

// TestStoredVocabularyIsEnforced writes every combination of object type,
// relation, subject type and subject relation straight into the table, past
// NewTuple, and holds the database to accepting exactly the shapes the
// vocabulary declares. It is what keeps the rows of authz_relation_shape and
// relationSpecs from drifting apart.
func TestStoredVocabularyIsEnforced(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		legal := legalShapes()

		var accepted, rejected int

		for _, object := range Types() {
			for _, relation := range allRelations() {
				for _, subjectType := range Types() {
					for _, subjectRelation := range []Relation{"", RelMember} {
						shape := shapeKey(object, relation, subjectType, subjectRelation)

						_, err := store.db.ExecContext(t.Context(), store.q(insertTupleSQL),
							object.String(), "probe", relation.String(),
							subjectType.String(), "probe", subjectRelation.String(),
						)

						if legal[shape] {
							require.NoErrorf(t, err, "the database refused a legal shape: %s", shape)

							accepted++

							continue
						}

						require.Errorf(t, err, "the database accepted an illegal shape: %s", shape)

						rejected++
					}
				}
			}
		}

		require.Equal(t, len(legal), accepted, "every declared shape has to be writable")
		require.Positive(t, rejected)
	})
}

// legalShapes is relationSpecs, flattened by hand rather than through
// expandShapes: a bug in the projection the database is loaded from must not
// also decide what this test expects.
func legalShapes() map[string]bool {
	out := make(map[string]bool)
	for _, spec := range relationSpecs {
		for _, subject := range spec.Subjects {
			out[shapeKey(spec.Object, spec.Relation, subject.Type, subject.Relation)] = true
		}
	}

	return out
}

func shapeKey(object Type, relation Relation, subjectType Type, subjectRelation Relation) string {
	return fmt.Sprintf("%s#%s@%s#%s", object, relation, subjectType, subjectRelation)
}

// allRelations is every relation name the vocabulary uses, in one place, for
// the sweep above.
func allRelations() []Relation {
	return []Relation{
		RelMember, RelParent, RelOrg, RelGroup, RelAgent,
		RelBlocked, RelViewer, RelOperator, RelManager, RelAdmin, RelOwner,
	}
}
