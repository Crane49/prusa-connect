package interfaces_test

import (
	"testing"

	"github.com/prusa3d/fractal-link/interfaces"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// The interfaces of the specification, by name. A file that stopped being embedded
// or a name that changed is a module that shows the network something other
// than the Prusa interfaces.
var declared = []fractal.InterfaceName{
	"Device1", "UpgradableDevice1", "Printer1", "Controller1", "FDMPrinter1",
	"SLAPrinter1", "FDMPrinterHead1", "PrintJob1", "FDMPrintJob1",
	"FilamentSlot1", "Chamber1",
}

// TestThisModuleServesThePrusaInterfaces: every interface the specification
// declares is in this binary, with its enumerations.
func TestThisModuleServesThePrusaInterfaces(t *testing.T) {
	t.Parallel()

	registry, err := interfaces.Registry()
	if err != nil {
		t.Fatalf("read the interface definitions: %v", err)
	}

	if registry.NumDefinitions() != len(declared) {
		t.Fatalf("this module serves %d interfaces, want %d",
			registry.NumDefinitions(), len(declared))
	}

	for _, name := range declared {
		if _, held := registry.Definition(name); !held {
			t.Errorf("%s is not in this binary", name)
		}
	}

	for _, name := range []fractal.Identifier{
		"FDMMaterial", "SLAMaterial", "PrinterState", "NozzleType",
	} {
		if _, held := registry.Enum(name); !held {
			t.Errorf("the %s enumeration is not in this binary", name)
		}
	}
}

// TestTheDefinitionsAreBuiltOnce: the registry is read-only, so every caller
// shares one and none of them locks.
func TestTheDefinitionsAreBuiltOnce(t *testing.T) {
	t.Parallel()

	first, err := interfaces.Registry()
	if err != nil {
		t.Fatalf("read the interface definitions: %v", err)
	}

	again, err := interfaces.Registry()
	if err != nil {
		t.Fatalf("read the interface definitions again: %v", err)
	}

	if first != again {
		t.Fatal("a second reader was given a registry of its own")
	}
}
