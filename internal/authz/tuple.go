package authz

import (
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const (
	typeSeparator     = ":"
	relationSeparator = "#"
)

// Object is one node of the graph: a type from the closed set and an id. The
// fields are unexported and validated at construction, so an object outside the
// vocabulary cannot be written by a struct literal.
type Object struct {
	typ Type
	id  string
}

// Subject is who a tuple grants to. With no relation it names a principal
// directly; with one it names a userset, every principal holding that relation
// on that object. A userset is how a grant reaches the members of a group.
type Subject struct {
	typ Type
	id  string
	rel Relation
}

// Tuple is object#relation@subject. NewTuple is the only way to build one, so
// every tuple in memory is already inside the vocabulary.
type Tuple struct {
	object   Object
	relation Relation
	subject  Subject
}

// NewObject validates the type against the closed set and the id against
// DNS-1123, the identifier grammar the rest of the system uses.
//
// A resource kind takes one further shape: <namespace>/<name>, the kind an
// extension contributes. It reaches no ref.Kind and no switch in the lifecycle
// plane, and a tuple written on one compiles to a row only while the extension
// that owns it is installed.
func NewObject(typ Type, id string) (Object, error) {
	if !typ.Valid() {
		return Object{}, unknownType.New("object type is not in the vocabulary").
			With(paramType(typ))
	}

	if typ == TypeResourceKind {
		if _, ok := contributedOwner(id); ok {
			return Object{typ: typ, id: id}, nil
		}
	}

	if err := ref.ValidateID(id); err != nil {
		return Object{}, invalidID.New("object id is not a DNS-1123 label").
			With(paramID(id)).Wrap(err)
	}

	return Object{typ: typ, id: id}, nil
}

// ParseObject reads the form String renders, "type:id".
func ParseObject(s string) (Object, error) {
	typ, id, found := strings.Cut(s, typeSeparator)
	if !found {
		return Object{}, invalidID.New("object is not type:id").With(paramID(s))
	}

	return NewObject(Type(typ), id)
}

// NewSubject names a principal.
func NewSubject(typ Type, id string) (Subject, error) {
	object, err := NewObject(typ, id)
	if err != nil {
		return Subject{}, err
	}

	return Subject{typ: object.typ, id: object.id}, nil
}

// NewUserset names every principal holding relation on that object, the
// "object#relation" position of a Zanzibar tuple.
func NewUserset(typ Type, id string, relation Relation) (Subject, error) {
	object, err := NewObject(typ, id)
	if err != nil {
		return Subject{}, err
	}

	if _, ok := lookupRelation(typ, relation); !ok {
		return Subject{}, unknownRelation.New("userset names a relation the type does not have").
			With(paramType(typ), paramRelation(relation))
	}

	return Subject{typ: object.typ, id: object.id, rel: relation}, nil
}

// ParseSubject reads the form String renders: "type:id" or "type:id#relation".
func ParseSubject(s string) (Subject, error) {
	head, relation, found := strings.Cut(s, relationSeparator)

	typ, id, split := strings.Cut(head, typeSeparator)
	if !split {
		return Subject{}, invalidID.New("subject is not type:id").With(paramID(s))
	}

	if !found {
		return NewSubject(Type(typ), id)
	}

	return NewUserset(Type(typ), id, Relation(relation))
}

// NewTuple validates the whole edge: the relation must exist on that object
// type, and the subject must be one of the shapes that relation accepts. This
// is where the closed vocabulary is enforced.
func NewTuple(object Object, relation Relation, subject Subject) (Tuple, error) {
	if object.IsZero() || subject.IsZero() {
		return Tuple{}, unknownType.New("tuple has an unconstructed object or subject")
	}

	shapes, ok := lookupRelation(object.typ, relation)
	if !ok {
		return Tuple{}, unknownRelation.New("relation is not defined on this object type").
			With(paramType(object.typ), paramRelation(relation))
	}

	if !permits(shapes, subject) {
		return Tuple{}, invalidSubject.New("relation does not accept this subject shape").
			With(paramType(object.typ), paramRelation(relation), paramSubject(subject))
	}

	return Tuple{object: object, relation: relation, subject: subject}, nil
}

// Type returns the object type.
func (o Object) Type() Type { return o.typ }

// ID returns the object id.
func (o Object) ID() string { return o.id }

// IsZero reports whether o was never constructed.
func (o Object) IsZero() bool { return o == Object{} }

// String renders "type:id".
func (o Object) String() string { return o.typ.String() + typeSeparator + o.id }

// Type returns the subject type.
func (s Subject) Type() Type { return s.typ }

// ID returns the subject id.
func (s Subject) ID() string { return s.id }

// Relation returns the userset relation, empty when the subject is a principal.
func (s Subject) Relation() Relation { return s.rel }

// IsUserset reports whether the subject names a relation rather than a
// principal.
func (s Subject) IsUserset() bool { return s.rel != "" }

// IsZero reports whether s was never constructed.
func (s Subject) IsZero() bool { return s == Subject{} }

// String renders "type:id" or "type:id#relation".
func (s Subject) String() string {
	out := s.typ.String() + typeSeparator + s.id
	if s.rel != "" {
		out += relationSeparator + s.rel.String()
	}

	return out
}

// Object returns the object the tuple is written on.
func (t Tuple) Object() Object { return t.object }

// Relation returns the edge name.
func (t Tuple) Relation() Relation { return t.relation }

// Subject returns who the tuple grants to.
func (t Tuple) Subject() Subject { return t.subject }

// IsZero reports whether t was never constructed.
func (t Tuple) IsZero() bool { return t == Tuple{} }

// String renders "object#relation@subject", the notation of the Zanzibar
// paper.
func (t Tuple) String() string {
	return t.object.String() + relationSeparator + t.relation.String() + "@" + t.subject.String()
}

func permits(shapes []subjectShape, subject Subject) bool {
	for _, shape := range shapes {
		if shape.Type == subject.typ && shape.Relation == subject.rel {
			return true
		}
	}

	return false
}
