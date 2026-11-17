package resource

import (
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// GenericCameraType is the core camera type a module's camera type runs on
// (ADR-20261106-km2). The module feeds it pictures over camera.accept, so no
// module code runs in the core.
const GenericCameraType = "core/generic"

// ModuleCameraType is one camera type an installed module offers.
type ModuleCameraType struct {
	// Type is what a camera is declared with: <namespace>/<name>.
	Type  string
	Title string
	// Namespace is the module's, the part of Type before the separator.
	Namespace string
	// Module is the module's own instance, which a camera of the type grants
	// operate to.
	Module ref.Ref
}

// ModuleCameraTypes is every camera type an enabled module offers, by type. A
// module that is switched off feeds nothing, so it offers nothing, and a
// camera of its type stops until the module is switched on again.
func ModuleCameraTypes(declared []Declaration) map[string]ModuleCameraType {
	out := map[string]ModuleCameraType{}

	for _, one := range declared {
		namespace, isModule := one.ModuleNamespace()
		if !isModule || one.Spec.Module == nil || !one.Spec.Enabled {
			continue
		}

		for _, offered := range one.Spec.Module.CameraTypes {
			typeName := namespace + extension.QualifiedSeparator + offered.Name
			out[typeName] = ModuleCameraType{
				Type: typeName, Title: offered.Title, Namespace: namespace, Module: one.Ref,
			}
		}
	}

	return out
}

// DriverType is the type the core builds an instance of kind as: the generic
// camera for a camera of a type an installed module offers, and the declared
// type for every other instance.
func DriverType(kind ref.Kind, typeName string, offered map[string]ModuleCameraType) string {
	if kind != ref.KindCamera {
		return typeName
	}

	if _, ok := offered[strings.TrimSpace(typeName)]; ok {
		return GenericCameraType
	}

	return typeName
}
