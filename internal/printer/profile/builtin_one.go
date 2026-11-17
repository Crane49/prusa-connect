//go:build noothermodels

package profile

import (
	"embed"
	"errors"
	"fmt"
)

// ErrNotOneModel reports a printer build whose embedded file does not hold
// exactly one model.
var ErrNotOneModel = errors.New("profile: the printer build embeds one model")

// ErrOtherModel reports a printer build whose embedded profile is not the
// model it was built for. A file left by an earlier build for another model
// would otherwise ship.
var ErrOtherModel = errors.New("profile: the printer build embeds another model's profile")

// BuiltFor is the model PRINTER_MODEL named when the binary was built. `make
// build-printer` sets it with -ldflags -X, from the same variable it writes
// the embedded profile from.
//
//nolint:gochecknoglobals // the linker writes it, and nothing else does.
var BuiltFor string

// printerModelFile is where `make build-printer` writes the profile of the
// model PRINTER_MODEL names, before it compiles the printer build.
const printerModelFile = "printermodel/profile.json"

// The printer build embeds the profile of its own model only. The directory
// holds a committed .gitkeep, so this file compiles before the profile is
// written. `all:` takes the .gitkeep too, and a pattern with no file in it
// does not compile.
//
//go:embed all:printermodel
var printerModelDir embed.FS

// loadBuiltin reads the one model the printer build embeds. The file carries
// its own alias and bound rows, narrowed to that model, so Load validates and
// applies them like a deployment's own profile file.
func loadBuiltin() (Registry, error) {
	data, err := printerModelDir.ReadFile(printerModelFile)
	if err != nil {
		return Registry{}, fmt.Errorf("%w: %s is missing, so build with make build-printer",
			ErrNotOneModel, printerModelFile)
	}

	registry, err := Load(data)
	if err != nil {
		return Registry{}, err
	}

	if wrong := oneModel(registry, BuiltFor); wrong != nil {
		return Registry{}, wrong
	}

	return registry, nil
}

// oneModel refuses a registry that does not hold exactly the one model the
// binary was built for.
func oneModel(registry Registry, builtFor string) error {
	models := registry.Models()
	if len(models) != 1 {
		return fmt.Errorf("%w: %s holds %d models", ErrNotOneModel, printerModelFile, len(models))
	}

	if builtFor == "" {
		return fmt.Errorf("%w: the binary does not say which model it was built for, so build it "+
			"with make build-printer PRINTER_MODEL=<model>", ErrOtherModel)
	}

	if model, ok := registry.Canonical(builtFor); !ok || model != models[0] {
		return fmt.Errorf("%w: the binary was built for %s and embeds the profile of %s. Build it "+
			"again with make build-printer PRINTER_MODEL=%s", ErrOtherModel, builtFor, models[0], builtFor)
	}

	return nil
}
