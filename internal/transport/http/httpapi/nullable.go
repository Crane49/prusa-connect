package httpapi

import (
	"encoding/json"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// Nullable is a member with three readings, as the public Prusa architecture
// writes some of a printer's tools: left out when the printer did not say,
// null when it said there is none, and a value. A pointer has only two of
// them, so a member of this type carries the json option omitzero, which
// leaves it out while Known is false.
type Nullable[T any] struct {
	// Known is whether the printer said anything. Value is nil when it said
	// there is none.
	Known bool
	Value *T
}

// NullOf is a member the printer said there is none of.
func NullOf[T any]() Nullable[T] { return Nullable[T]{Known: true} }

// ValueOf is a member the printer reported as v.
func ValueOf[T any](v T) Nullable[T] { return Nullable[T]{Known: true, Value: &v} }

// IsZero reports whether the member is left out.
func (n Nullable[T]) IsZero() bool { return !n.Known }

// MarshalJSON writes the value, or null.
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}

	return json.Marshal(n.Value)
}

// UnmarshalJSON reads a member that is present, so it is known either way.
func (n *Nullable[T]) UnmarshalJSON(raw []byte) error {
	n.Known, n.Value = true, nil
	if string(raw) == "null" {
		return nil
	}

	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}

	n.Value = &v

	return nil
}

// Schema is the schema of T, or null.
func (Nullable[T]) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{AnyOf: []*huma.Schema{r.Schema(reflect.TypeFor[T](), true, ""), {Type: "null"}}}
}
