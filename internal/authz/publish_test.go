package authz

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEveryActionIsDescribed is the first direction of ADR-20260825-bhe rule 8 held at
// the source: an action added to the closed set and not described here would
// publish a row with two empty cells.
func TestEveryActionIsDescribed(t *testing.T) {
	t.Parallel()

	docs := ActionDocs()
	require.Len(t, docs, len(Actions()), "the prose table and the closed set are different sizes")

	for i, action := range Actions() {
		require.Equalf(t, action, docs[i].Action, "entry %d is out of Actions() order", i)
		require.NotEmptyf(t, strings.TrimSpace(docs[i].Summary), "%s has no summary", action)
		require.NotEmptyf(t, strings.TrimSpace(docs[i].Description), "%s has no description", action)
		require.Equalf(t, VocabularyVersion, docs[i].Version, "%s carries the wrong version", action)
	}
}

// TestTheActionProseNamesNoActionOutsideTheClosedSet is the other direction: a
// descriptor left behind by a retired action would publish an action every
// check refuses.
func TestTheActionProseNamesNoActionOutsideTheClosedSet(t *testing.T) {
	t.Parallel()

	for _, doc := range ActionDocs() {
		require.Truef(t, doc.Action.Valid(), "%s is described and is not in the closed set", doc.Action)
		require.Truef(t, doc.Domain.Valid(), "%s is defined on %q, which is not a type", doc.Action, doc.Domain)
	}
}

// TestEveryActionIsConferredBySomething catches the vocabulary's worst silent
// failure: an action that is defined, published and reachable by no grant at
// all, so every caller is refused and the refusal looks like a permission
// problem rather than a hole in the model.
func TestEveryActionIsConferredBySomething(t *testing.T) {
	t.Parallel()

	for _, doc := range ActionDocs() {
		require.NotEmptyf(t, doc.ConferredBy, "%s is conferred by no relation, so nothing can ever hold it", doc.Action)

		for _, relation := range doc.ConferredBy {
			require.Containsf(t, Relations(), relation,
				"%s is published as conferred by %q, which is not a relation", doc.Action, relation)
		}
	}
}

// TestTheConferralListIsWhatTheCheckEnforces compares the published claim with
// the function the write path bounds an escalation with. Two readers of one set
// is the shape that drifts, so there is only one.
func TestTheConferralListIsWhatTheCheckEnforces(t *testing.T) {
	t.Parallel()

	for _, doc := range ActionDocs() {
		for _, relation := range Relations() {
			_, declared := lookupRelation(doc.Domain, relation)
			confers := declared && conferred(doc.Domain, relation)&doc.Action.mask() != 0

			require.Equalf(t, confers, slices.Contains(doc.ConferredBy, relation),
				"%s on %s: published conferral by %s disagrees with conferred()",
				doc.Action, doc.Domain, relation)
		}
	}
}

// TestTheAgentActionsAreOutsideEveryRole holds the security property the
// published page states. An agent action a role confers would make a fleet-wide
// operator grant permission to forge a device's readings.
func TestTheAgentActionsAreOutsideEveryRole(t *testing.T) {
	t.Parallel()

	for _, action := range []Action{ActionReportTelemetry, ActionFetchJob} {
		doc, ok := lookupActionDoc(action)
		require.Truef(t, ok, "%s has no descriptor", action)
		require.Equalf(t, []Relation{RelAgent}, doc.ConferredBy,
			"%s is conferred by more than the agent relation", action)
	}
}

// TestEveryTypeIsDescribed holds both directions on the type half.
func TestEveryTypeIsDescribed(t *testing.T) {
	t.Parallel()

	docs := TypeDocs()
	require.Len(t, docs, len(Types()), "the prose table and the closed set are different sizes")

	for i, typ := range Types() {
		require.Equalf(t, typ, docs[i].Type, "entry %d is out of Types() order", i)
		require.NotEmptyf(t, strings.TrimSpace(docs[i].Summary), "%s has no summary", typ)
		require.NotEmptyf(t, strings.TrimSpace(docs[i].Description), "%s has no description", typ)
		require.Truef(t, docs[i].Type.Valid(), "%s is described and is not in the closed set", typ)
		require.Truef(t, docs[i].Subject || docs[i].Object,
			"%s stands on neither side of a tuple, so nothing can name it", typ)
	}
}

// TestEveryRelationIsDescribed holds both directions on the relation half, and
// with them the accessor Relations() itself: a relation added to relationSpecs
// and not to Relations() would leave the grammar published and the name absent.
func TestEveryRelationIsDescribed(t *testing.T) {
	t.Parallel()

	docs := RelationDocs()
	require.Len(t, docs, len(Relations()), "the prose table and the closed set are different sizes")

	for i, relation := range Relations() {
		require.Equalf(t, relation, docs[i].Relation, "entry %d is out of Relations() order", i)
		require.NotEmptyf(t, strings.TrimSpace(docs[i].Summary), "%s has no summary", relation)
		require.NotEmptyf(t, strings.TrimSpace(docs[i].Description), "%s has no description", relation)
	}

	for _, spec := range relationSpecs {
		require.Containsf(t, Relations(), spec.Relation,
			"the grammar declares %s and Relations() does not name it", spec.Relation)
	}

	for _, relation := range Relations() {
		used := slices.ContainsFunc(relationSpecs, func(spec relationSpec) bool {
			return spec.Relation == relation
		})
		require.Truef(t, used, "%s is published and no permitted shape uses it", relation)
	}
}

// TestTheRoleFlagIsWhatRoleLevelReports keeps the published half of the role
// layer from drifting from the half that resolves a grant.
func TestTheRoleFlagIsWhatRoleLevelReports(t *testing.T) {
	t.Parallel()

	for _, doc := range RelationDocs() {
		_, isRole := roleLevel(doc.Relation)
		require.Equalf(t, isRole, doc.Role, "%s is published role=%v", doc.Relation, doc.Role)
	}
}

// TestTheShapesAreTheGrammarTheDatabaseHolds proves the published grammar is
// the projection the tuple table's composite foreign key points at, rather than
// a second list of the same thing.
func TestTheShapesAreTheGrammarTheDatabaseHolds(t *testing.T) {
	t.Parallel()

	shapes := Shapes()
	expanded := expandShapes(relationSpecs)

	require.Len(t, shapes, len(expanded), "the published grammar and the projected one differ in size")
	require.NotEmpty(t, shapes, "the published grammar is empty")

	for i, shape := range expanded {
		require.Equal(t, shape.ObjectType, shapes[i].ObjectType)
		require.Equal(t, shape.Relation, shapes[i].Relation)
		require.Equal(t, shape.SubjectType, shapes[i].SubjectType)
		require.Equal(t, shape.SubjectRelation, shapes[i].SubjectRelation)
	}
}

// TestTheDescriptorsAreDistinct catches a copied entry whose identity was not
// changed, which would publish one name twice and lose another.
func TestTheDescriptorsAreDistinct(t *testing.T) {
	t.Parallel()

	actions := make(map[Action]struct{}, len(actionProse))
	for _, doc := range ActionDocs() {
		_, taken := actions[doc.Action]
		require.Falsef(t, taken, "%s is described twice", doc.Action)
		actions[doc.Action] = struct{}{}
	}

	types := make(map[Type]struct{}, len(typeProse))
	for _, doc := range TypeDocs() {
		_, taken := types[doc.Type]
		require.Falsef(t, taken, "%s is described twice", doc.Type)
		types[doc.Type] = struct{}{}
	}

	rels := make(map[Relation]struct{}, len(relationProse))
	for _, doc := range RelationDocs() {
		_, taken := rels[doc.Relation]
		require.Falsef(t, taken, "%s is described twice", doc.Relation)
		rels[doc.Relation] = struct{}{}
	}
}

// retiredActionNames are the device action names the class set replaced. Each is
// a single word, so the name pattern below cannot catch it, and a published
// sentence that still says one of them tells a reader about an action they
// cannot be granted.
var retiredActionNames = []string{"manage", "provision", "upload"}

// vocabularyName matches a word written the way the vocabulary spells its
// names, with an underscore between lowercase words.
var vocabularyName = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)

// TestThePublishedProseNamesOnlyRealVocabulary reads every summary and
// description the reference publishes. Each name spelled with an underscore
// must be an action, a type or a relation of this vocabulary, and no retired
// action name may appear as a word.
func TestThePublishedProseNamesOnlyRealVocabulary(t *testing.T) {
	t.Parallel()

	known := make(map[string]struct{})
	for _, action := range Actions() {
		known[action.String()] = struct{}{}
	}

	for _, typ := range Types() {
		known[typ.String()] = struct{}{}
	}

	for _, relation := range Relations() {
		known[relation.String()] = struct{}{}
	}

	texts := make(map[string]string)
	for _, doc := range ActionDocs() {
		texts["action "+doc.Action.String()] = doc.Summary + " " + doc.Description
	}

	for _, doc := range TypeDocs() {
		texts["type "+doc.Type.String()] = doc.Summary + " " + doc.Description
	}

	for _, doc := range RelationDocs() {
		texts["relation "+doc.Relation.String()] = doc.Summary + " " + doc.Description
	}

	for entry, text := range texts {
		for _, name := range vocabularyName.FindAllString(text, -1) {
			_, ok := known[name]
			require.Truef(t, ok, "%s names %s, which is not in the vocabulary", entry, name)
		}

		for _, retired := range retiredActionNames {
			word := regexp.MustCompile(`\b` + retired + `\b`)
			require.Falsef(t, word.MatchString(text),
				"%s names %s, which is a retired action", entry, retired)
		}
	}
}

func lookupActionDoc(action Action) (ActionDoc, bool) {
	for _, doc := range ActionDocs() {
		if doc.Action == action {
			return doc, true
		}
	}

	return ActionDoc{}, false
}
