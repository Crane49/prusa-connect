package extension

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The errors a lookup reports. Callers inspect them with errors.Is.
var (
	// ErrUnknown reports a name no registered entry answers to.
	ErrUnknown = errors.New("extension: unknown name")
	// ErrAmbiguous reports a bare name that two namespaces both answer to.
	ErrAmbiguous = errors.New("extension: ambiguous name")
)

// QualifiedSeparator divides a namespace from a name, in the qualified form an
// operator writes when two modules registered the same bare name.
const QualifiedSeparator = "/"

// MaxQualifiedLen is the longest a qualified name can be: a namespace, the
// separator and a name. Bound a qualified name with it, not with MaxNameLen.
const MaxQualifiedLen = MaxNamespaceLen + len(QualifiedSeparator) + MaxNameLen

// IsQualified reports whether a name written in configuration has a
// namespace.
func IsQualified(name string) bool { return strings.Contains(name, QualifiedSeparator) }

// Registry is the table one host owns for one extension point. It is immutable
// once built, so it is safe to share without a lock.
type Registry[T any] struct {
	kind        Kind
	byQualified map[string]Entry[T]
}

// NewRegistry builds a registry for one kind from entries a Set drained. It
// reports ErrDuplicate when two entries hold one qualified name.
func NewRegistry[T any](kind Kind, entries ...Entry[T]) (*Registry[T], error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidKind, kind)
	}

	r := &Registry[T]{kind: kind, byQualified: make(map[string]Entry[T], len(entries))}

	for _, entry := range entries {
		if entry.Descriptor.kind != kind {
			return nil, fmt.Errorf("%w: %s cannot go in the %s registry", ErrInvalidKind, entry.Descriptor, kind)
		}

		qualified := entry.Descriptor.Qualified()
		if existing, taken := r.byQualified[qualified]; taken {
			return nil, fmt.Errorf("%w: %s is registered by %q and again by %q",
				ErrDuplicate, entry.Descriptor, existing.Descriptor.module, entry.Descriptor.module)
		}

		r.byQualified[qualified] = entry
	}

	return r, nil
}

// Kind returns the extension point this registry serves.
func (r *Registry[T]) Kind() Kind { return r.kind }

// Lookup resolves a name written in configuration. A qualified name matches
// exactly. A bare name matches when one namespace answers to it. When more
// than one does, Lookup reports ErrAmbiguous and names every candidate.
func (r *Registry[T]) Lookup(name string) (Entry[T], error) {
	if IsQualified(name) {
		found, ok := r.byQualified[name]
		if !ok {
			return Entry[T]{}, fmt.Errorf("%w: %s %q, this build has %s", ErrUnknown, r.kind, name, r.list())
		}

		return found, nil
	}

	var candidates []Entry[T]

	for _, entry := range r.Entries() {
		if entry.Descriptor.name == name {
			candidates = append(candidates, entry)
		}
	}

	switch len(candidates) {
	case 0:
		return Entry[T]{}, fmt.Errorf("%w: %s %q, this build has %s", ErrUnknown, r.kind, name, r.list())
	case 1:
		return candidates[0], nil
	}

	return Entry[T]{}, fmt.Errorf("%w: %s %q is served by %s", ErrAmbiguous, r.kind, name, qualifiedNames(candidates))
}

// Registered reports whether a name written in configuration resolves here.
func (r *Registry[T]) Registered(name string) bool {
	_, err := r.Lookup(name)

	return err == nil
}

// Entries returns everything registered, sorted by qualified name.
func (r *Registry[T]) Entries() []Entry[T] {
	out := make([]Entry[T], 0, len(r.byQualified))
	for _, entry := range r.byQualified {
		out = append(out, entry)
	}

	slices.SortFunc(out, func(a, b Entry[T]) int {
		return strings.Compare(a.Descriptor.Qualified(), b.Descriptor.Qualified())
	})

	return out
}

// Descriptors returns the documentation of everything registered.
func (r *Registry[T]) Descriptors() []Descriptor {
	entries := r.Entries()

	out := make([]Descriptor, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Descriptor)
	}

	return out
}

// list renders the names this build has, for an unknown-name error.
func (r *Registry[T]) list() string {
	entries := r.Entries()
	if len(entries) == 0 {
		return "none"
	}

	return qualifiedNames(entries)
}

func qualifiedNames[T any](entries []Entry[T]) string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Descriptor.Qualified())
	}

	slices.Sort(names)

	return strings.Join(names, ", ")
}
