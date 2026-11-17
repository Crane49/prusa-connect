package app

import (
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// declaredSettings is resource.Declared over the type table: the settings each
// compiled-in type says it reads. The declaration says which setting is a
// credential.
//
// It lives in the composition root because the configuration store may not
// import the registry and the registry knows nothing about configuration
// files.
type declaredSettings struct{ types *registry.Registry }

// DeclaredOptions returns the options one registered type declares, and false
// for a name this build has no type for.
func (d declaredSettings) DeclaredOptions(kind ref.Kind, typeName string) ([]extension.Option, bool) {
	if d.types == nil {
		return nil, false
	}

	found, err := d.types.Lookup(kind, typeName)
	if err != nil {
		return nil, false
	}

	return found.Descriptor().Doc().Options, true
}
