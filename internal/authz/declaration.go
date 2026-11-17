package authz

import (
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const (
	// qualifiedSeparator joins a namespace to the name it qualifies in a
	// contributed resource kind.
	qualifiedSeparator = "/"

	namespaceLabelSeparator = "."

	// minNamespaceLabels and maxNamespaceLen bound the namespace an extension
	// host derives from a Go module path. TestTheNamespaceGrammarMatchesTheDerivedOne
	// holds this validator and extension.NamespaceOf together.
	minNamespaceLabels = 2
	maxNamespaceLen    = 96

	// coreNamespace is the namespace of everything compiled into this binary.
	// It is extension.CoreNamespace, spelled here because the graph imports
	// nothing of the extension host.
	coreNamespace = "core"
)

// ContributedKind renders the resource_kind object id for a kind an extension
// contributes: the host-derived namespace, a slash, and a DNS-1123 name the
// extension chose.
//
// A contributed kind is an authorization object and not a ref.Kind. Nothing
// configures, constructs or supervises one, so it reaches none of the switches
// in the lifecycle plane and ref.Kinds stays a compile-time table.
func ContributedKind(namespace, name string) (string, error) {
	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	if err := ref.ValidateID(name); err != nil {
		return "", invalidID.New("a contributed resource kind is named by a DNS-1123 label").
			With(paramName(name)).Wrap(err)
	}

	if ref.Kind(name).Valid() {
		return "", declarationRefused.New(
			"a contributed resource kind may not take the name of a core one").
			With(paramName(name))
	}

	return namespace + qualifiedSeparator + name, nil
}

// contributedOwner returns the namespace that owns a contributed resource kind
// id, reporting false for a core kind and for anything that is not one
// qualified name.
func contributedOwner(id string) (string, bool) {
	namespace, name, found := strings.Cut(id, qualifiedSeparator)
	if !found {
		return "", false
	}

	if strings.Contains(name, qualifiedSeparator) {
		return "", false
	}

	if validateNamespace(namespace) != nil || ref.ValidateID(name) != nil {
		return "", false
	}

	return namespace, true
}

// validateNamespace holds the rendering extension.NamespaceOf produces: dotted
// DNS-1123 labels, at least two of them, at most 96 bytes, and never the core
// namespace.
//
// The core namespace is refused because a name under it would be
// indistinguishable from a compiled one, and the compiled ones are the only
// actions with a compile-time level mask.
func validateNamespace(namespace string) error {
	if namespace == "" {
		return declarationRefused.New("the declaring namespace is empty, and a host derives it " +
			"from the declaring module rather than reading it from the extension")
	}

	if len(namespace) > maxNamespaceLen {
		return declarationRefused.New("the declaring namespace is longer than a vocabulary row carries").
			With(paramName(namespace))
	}

	if namespace == coreNamespace || strings.HasPrefix(namespace, coreNamespace+namespaceLabelSeparator) {
		return declarationRefused.New("the core namespace is this project's own, and its actions are " +
			"compiled in").With(paramName(namespace))
	}

	labels := strings.Split(namespace, namespaceLabelSeparator)
	if len(labels) < minNamespaceLabels {
		return declarationRefused.New("a namespace of one label is not a collision domain. " +
			"Give the module path a host, as github.com/acme/mk4 has").With(paramName(namespace))
	}

	for _, label := range labels {
		if ref.ValidateID(label) != nil {
			return declarationRefused.New("every label of a namespace is a DNS-1123 label").
				With(paramName(namespace))
		}
	}

	return nil
}
