package extension

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// The errors a registration reports. Callers inspect them with errors.Is.
var (
	// ErrDuplicate reports one qualified name registered twice for one kind.
	// The second registration never replaces the first, so a plugin cannot
	// take over a built-in.
	ErrDuplicate = errors.New("extension: name already registered")
	// ErrSealed reports a registration that arrived after the host drained the
	// gathering list.
	ErrSealed = errors.New("extension: registrations are already drained")
	// ErrNoValue reports a registration with nothing behind the descriptor.
	ErrNoValue = errors.New("extension: registration carries no implementation")
)

// Entry is one registration: what the host knows about it, and what it
// registered.
type Entry[T any] struct {
	// Descriptor is the identity and the documentation.
	Descriptor Descriptor
	// Value is what the registry hands back on a lookup.
	Value T
}

// Set is a gathering list of registrations. Registration happens in an init
// function, before the host exists. The host drains the list once, which
// seals it. The zero value is an empty list, ready to use, and safe for
// concurrent use.
type Set[T any] struct {
	mu      sync.Mutex
	entries []Entry[T]
	sealed  bool
}

// Add appends one registration. It reports ErrSealed once the list has been
// drained, and ErrDuplicate for a qualified name this kind already has, in
// this list or in another one.
func (s *Set[T]) Add(entry Entry[T]) error {
	if entry.Descriptor.IsZero() {
		return fmt.Errorf("%w: an undeclared descriptor cannot be registered", ErrNoValue)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sealed {
		return fmt.Errorf("%w: %s arrived after the registry was built", ErrSealed, entry.Descriptor)
	}

	for _, existing := range s.entries {
		if existing.Descriptor.kind != entry.Descriptor.kind ||
			existing.Descriptor.Qualified() != entry.Descriptor.Qualified() {
			continue
		}

		return fmt.Errorf("%w: %s is registered by %q and again by %q",
			ErrDuplicate, entry.Descriptor,
			existing.Descriptor.module, entry.Descriptor.module)
	}

	if err := catalogue.record(entry.Descriptor); err != nil {
		return err
	}

	s.entries = append(s.entries, entry)

	return nil
}

// Drain seals the list and returns its entries, sorted by qualified name.
// Draining twice returns the same entries.
func (s *Set[T]) Drain() []Entry[T] {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sealed = true

	return s.sorted()
}

// Entries returns the entries of the list without sealing it.
func (s *Set[T]) Entries() []Entry[T] {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sorted()
}

// Descriptors returns the documentation of everything in the list, without
// sealing it.
func (s *Set[T]) Descriptors() []Descriptor {
	entries := s.Entries()

	out := make([]Descriptor, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Descriptor)
	}

	return out
}

// Sealed reports whether the list has been drained.
func (s *Set[T]) Sealed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sealed
}

// sorted returns a copy in catalogue order. The caller holds the lock.
func (s *Set[T]) sorted() []Entry[T] {
	out := slices.Clone(s.entries)
	slices.SortFunc(out, func(a, b Entry[T]) int {
		return strings.Compare(a.Descriptor.String(), b.Descriptor.String())
	})

	return out
}

// SortDescriptors orders descriptors as the published reference does: by kind
// in the order Kinds returns, then by qualified name.
func SortDescriptors(descriptors []Descriptor) {
	order := map[Kind]int{}
	for i, kind := range Kinds() {
		order[kind] = i
	}

	slices.SortFunc(descriptors, func(a, b Descriptor) int {
		if a.kind != b.kind {
			return order[a.kind] - order[b.kind]
		}

		return strings.Compare(a.Qualified(), b.Qualified())
	})
}

// Resolve answers a name against a gathering list that has not been sealed,
// with the rule Registry.Lookup uses: a qualified name is exact, and a bare
// name is answered only when one entry has it. An ambiguous bare name is
// refused, so a plugin cannot take over a built-in by registering its name.
func (s *Set[T]) Resolve(name string) (Entry[T], error) {
	entries := s.Entries()

	if IsQualified(name) {
		for _, entry := range entries {
			if entry.Descriptor.Qualified() == name {
				return entry, nil
			}
		}

		return Entry[T]{}, fmt.Errorf("%w: %q", ErrUnknown, name)
	}

	var candidates []Entry[T]

	for _, entry := range entries {
		if entry.Descriptor.name == name {
			candidates = append(candidates, entry)
		}
	}

	switch len(candidates) {
	case 0:
		return Entry[T]{}, fmt.Errorf("%w: %q", ErrUnknown, name)
	case 1:
		return candidates[0], nil
	}

	return Entry[T]{}, fmt.Errorf("%w: %s %q is served by %s",
		ErrAmbiguous, candidates[0].Descriptor.kind, name, qualifiedNames(candidates))
}
