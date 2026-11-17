// Package registration holds the parts of a contract package's registration
// wrapper that authapi, cameraapi, eventsapi and storageapi each write the same
// way.
//
// It is internal to the sdk module on purpose. The published Registration types
// keep their unexported fields, so the only way to pair a descriptor with a
// factory stays extension.Declare, and an outside module cannot take a
// built-in's exported Descriptor and register its own code behind it.
package registration

import "fmt"

// Target is what a failed build names: the type an operator wrote in
// configuration, the qualified name of the registration that answered, and the
// sentinel the contract package refuses with.
//
// The sentinel is a field because each contract package owns its own, and its
// text names the package that refused. Passing it keeps errors.Is against
// authapi.ErrNoFactory false for a storageapi failure.
type Target struct {
	// Requested is the type as written in configuration, which is all the
	// caller has when the registration turns out to carry no factory.
	Requested string
	// Qualified is "namespace/name" for the registration that answered.
	Qualified string
	// NoFactory is the contract package's own ErrNoFactory.
	NoFactory error
}

// Build makes one instance and refuses the two ways a registration produces
// none: it carries no factory, or its factory returns nothing without saying
// why. A nil implementation reaching a host is a panic later, at a call site
// that names neither the type nor the module that registered it.
func Build[Spec, Impl any](target Target, factory func(Spec) (Impl, error), spec Spec) (Impl, error) {
	var none Impl

	if factory == nil {
		return none, fmt.Errorf("%w: %s", target.NoFactory, target.Requested)
	}

	built, err := factory(spec)
	if err != nil {
		return none, fmt.Errorf("build %s: %w", target.Qualified, err)
	}

	if any(built) == nil {
		return none, fmt.Errorf("%w: %s built nothing", target.NoFactory, target.Qualified)
	}

	return built, nil
}
