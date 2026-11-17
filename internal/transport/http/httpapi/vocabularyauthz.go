package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// The authorization vocabulary: its published shapes, its route, and the
// readers that turn what internal/authz declares into them.
//
// It is the largest of the vocabularies, because it publishes four closed sets
// and not one, and it is the only one whose entries a reader has to compare
// with each other to use. It sits here and not in vocabularies.go so the
// registrations there stay short.

// authorizationOutput is the authorization vocabulary: what a caller performs,
// what it is performed on, the edges that join them, and every shape a stored
// edge may take.
type authorizationOutput struct {
	Body struct {
		Vocabulary string                  `json:"vocabulary" doc:"The vocabulary this set belongs to."`
		Version    string                  `json:"version"    doc:"The content version of the set."`
		Format     string                  `json:"format"     doc:"The shape of an entry."`
		Actions    []authorizationAction   `json:"actions"`
		Types      []authorizationType     `json:"types"`
		Relations  []authorizationRelation `json:"relations"`
		Shapes     []authorizationShape    `json:"shapes"`
		// Kinds is the verb table, both halves: every device kind this build
		// carries and every verb of it, with the class, the markings and who
		// marked each.
		//
		// It is one surface and not four. The person who added a module to a
		// build has probably not read its verb table, and this is where they
		// read it afterwards: what the module declared, what the host marked
		// whatever the declaration said, and where a rebuild disagrees with
		// what this deployment first saw.
		Kinds []authorizationKind `json:"kinds"`
	}
}

// authorizationKind is one device kind's verbs, as the inventory renders them.
type authorizationKind struct {
	Kind        string              `json:"kind"             doc:"The device kind. A bare name is this build's own; a qualified one is namespace/name, with the namespace derived from the declaring module."`
	DisplayName string              `json:"display_name"     doc:"What an operator reads in a listing."`
	Module      string              `json:"module,omitempty" doc:"The Go module path that declared it, absent for a kind compiled into the core itself. It is half of the key the first-seen record is pinned by, because it is the part a declaration author cannot rename away from."`
	Verbs       []authorizationVerb `json:"verbs"`
}

// authorizationVerb is one verb of one kind.
type authorizationVerb struct {
	Verb     string                 `json:"verb"               doc:"The verb, as a command names it."`
	Class    string                 `json:"class"              doc:"The class it carries: what a caller must hold to send it. It is operate or configure; no verb carries a read action, because answering any verb writes to a transport that carries one command at a time."`
	Markings []authorizationMarking `json:"markings,omitempty" doc:"Why this verb is in the dangerous tier, empty for a verb its class confers. A marked verb is conferred by no class, role or level: reaching one takes a row naming it, or ownership of the machine."`
}

// authorizationMarking is one marking and the hand that put it there.
type authorizationMarking struct {
	Marking string `json:"marking" doc:"irreversible for an act this server cannot undo, opaque_payload for a payload nothing inspected."`
	Marker  string `json:"marker"  doc:"host or adapter. The host's marking is computed from the capability descriptor whatever the declaration says, and is cleared by nobody."`
}

type authorizationAction struct {
	Action      string   `json:"action"       doc:"The action a handler checks."`
	Domain      string   `json:"domain"       doc:"The object type this action is defined on."`
	Summary     string   `json:"summary"      doc:"What the action permits."`
	Description string   `json:"description"  doc:"The longer description."`
	ConferredBy []string `json:"conferred_by" doc:"The relations that hand it out on an object of that type."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

type authorizationType struct {
	Type        string `json:"type"        doc:"The type, as it is written in a tuple."`
	Summary     string `json:"summary"     doc:"What the type names."`
	Description string `json:"description" doc:"The longer description."`
	Subject     bool   `json:"subject"     doc:"Whether it may stand on the subject side of a tuple."`
	Object      bool   `json:"object"      doc:"Whether grants may be written on it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

type authorizationRelation struct {
	Relation    string `json:"relation"    doc:"The edge name."`
	Summary     string `json:"summary"     doc:"What the edge means."`
	Description string `json:"description" doc:"The longer description."`
	Role        bool   `json:"role"        doc:"Whether it confers actions rather than carrying what it names."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

type authorizationShape struct {
	ObjectType      string `json:"object_type"                doc:"The type a tuple's object must be."`
	Relation        string `json:"relation"                   doc:"The edge name."`
	SubjectType     string `json:"subject_type"               doc:"The type a tuple's subject must be."`
	SubjectRelation string `json:"subject_relation,omitempty" doc:"Set when the subject is a userset, not a principal."`
}

func registerAuthorizationVocabulary(api huma.API) {
	register(api, huma.Operation{
		OperationID: "get-authorization-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyAuthorizationPath,
		Summary:     "The authorization actions, types, relations and permitted shapes",
		Description: "What a caller can perform, what those things are performed on, the edges that join " +
			"them, and every shape a stored grant may take. Which relation confers which action is read " +
			"from the same function the check enforces, so the published answer is the enforced one. " +
			"It also carries the verb table: every device kind this build carries and every verb of " +
			"it, with the class the verb takes and any marking that puts it in the dangerous tier. " +
			"That half is per build rather than per release, because a kind is in it when somebody " +
			"compiled its declaration in, and it is the inventory whoever wrote that build " +
			"configuration reads afterwards.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*authorizationOutput, error) {
		out := &authorizationOutput{}
		out.Body.Vocabulary = authz.VocabularyName
		out.Body.Version = authz.VocabularyVersion
		out.Body.Format = authz.VocabularyFormat
		out.Body.Actions = authorizationActions()
		out.Body.Types = authorizationTypes()
		out.Body.Relations = authorizationRelations()
		out.Body.Shapes = authorizationShapes()
		out.Body.Kinds = authorizationKinds()

		return out, nil
	})
}

func authorizationActions() []authorizationAction {
	docs := authz.ActionDocs()

	out := make([]authorizationAction, 0, len(docs))
	for _, doc := range docs {
		conferred := make([]string, 0, len(doc.ConferredBy))
		for _, relation := range doc.ConferredBy {
			conferred = append(conferred, relation.String())
		}

		out = append(out, authorizationAction{
			Action:      doc.Action.String(),
			Domain:      doc.Domain.String(),
			Summary:     doc.Summary,
			Description: doc.Description,
			ConferredBy: conferred,
			Deprecated:  doc.Deprecated,
		})
	}

	return out
}

func authorizationTypes() []authorizationType {
	docs := authz.TypeDocs()

	out := make([]authorizationType, 0, len(docs))
	for _, doc := range docs {
		out = append(out, authorizationType{
			Type:        doc.Type.String(),
			Summary:     doc.Summary,
			Description: doc.Description,
			Subject:     doc.Subject,
			Object:      doc.Object,
			Deprecated:  doc.Deprecated,
		})
	}

	return out
}

func authorizationRelations() []authorizationRelation {
	docs := authz.RelationDocs()

	out := make([]authorizationRelation, 0, len(docs))
	for _, doc := range docs {
		out = append(out, authorizationRelation{
			Relation:    doc.Relation.String(),
			Summary:     doc.Summary,
			Description: doc.Description,
			Role:        doc.Role,
			Deprecated:  doc.Deprecated,
		})
	}

	return out
}

func authorizationShapes() []authorizationShape {
	docs := authz.Shapes()

	out := make([]authorizationShape, 0, len(docs))
	for _, doc := range docs {
		out = append(out, authorizationShape{
			ObjectType:      doc.ObjectType.String(),
			Relation:        doc.Relation.String(),
			SubjectType:     doc.SubjectType.String(),
			SubjectRelation: doc.SubjectRelation.String(),
		})
	}

	return out
}

// authorizationKinds renders the verb table for the published vocabulary. It
// reads the table the command path reads, so the published verb set and the
// checkable one are the same set.
func authorizationKinds() []authorizationKind {
	inventory := authz.Inventory()

	out := make([]authorizationKind, 0, len(inventory))

	for _, kind := range inventory {
		entry := authorizationKind{
			Kind:        kind.Kind.String(),
			DisplayName: kind.DisplayName,
			Module:      kind.Module,
			Verbs:       make([]authorizationVerb, 0, len(kind.Verbs)),
		}

		for _, verb := range kind.Verbs {
			entry.Verbs = append(entry.Verbs, authorizationVerb{
				Verb:     verb.Verb,
				Class:    verb.Class.String(),
				Markings: markingsOf(verb.Markings),
			})
		}

		out = append(out, entry)
	}

	return out
}

func markingsOf(rows []authz.MarkingRow) []authorizationMarking {
	if len(rows) == 0 {
		return nil
	}

	out := make([]authorizationMarking, 0, len(rows))
	for _, row := range rows {
		out = append(out, authorizationMarking{
			Marking: row.Marking.String(), Marker: row.Marker.String(),
		})
	}

	return out
}
