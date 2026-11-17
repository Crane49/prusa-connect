package bridge_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/fractal-link/bridge"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// verbPrefix is the start of every command verb of this server. No device
// answers a command whose verb starts with anything else.
const verbPrefix = "core.command."

// TestEveryRowOfTheSpecificationIsAccountedFor: every interface, property and
// method of the definitions has a row in mapping.json, or an "absent" entry
// with a reason.
//
// When somebody adds a property to an interface, this test fails until
// mapping.json has a row for it or a reason why this server cannot provide it.
func TestEveryRowOfTheSpecificationIsAccountedFor(t *testing.T) {
	t.Parallel()

	held := mapping(t)
	registry := registry(t)

	absent := make(map[string]string)
	whole := make(map[fractal.InterfaceName]string)

	for _, entry := range held.Absent() {
		if entry.Reason == "" {
			t.Errorf("an absent entry of %s has no reason", entry.Interface)
		}

		if entry.Whole() {
			whole[entry.Interface] = entry.Reason

			continue
		}

		absent[string(entry.Interface)+"."+string(entry.Property)+string(entry.Method)] = entry.Reason
	}

	for definition := range registry.Definitions() {
		exposed, shown := held.Interface(definition.Name())
		if !shown {
			if _, named := whole[definition.Name()]; !named {
				t.Errorf("%s has no rows and no absent entry", definition.Name())
			}

			continue
		}

		for property := range definition.Properties() {
			if _, mapped := exposed.Property(property.Name()); mapped {
				continue
			}

			if _, named := absent[string(definition.Name())+"."+string(property.Name())]; !named {
				t.Errorf("the property %s.%s has no row and no absent entry", definition.Name(), property.Name())
			}
		}

		for method := range definition.Methods() {
			if _, mapped := exposed.Method(method.Name()); mapped {
				continue
			}

			if _, named := absent[string(definition.Name())+"."+string(method.Name())]; !named {
				t.Errorf("the method %s.%s has no row and no absent entry", definition.Name(), method.Name())
			}
		}
	}
}

// TestEveryRowNamesAVerbThisServerSpells: every verb in mapping.json starts
// with "core.command.", and every write row names the argument for the value.
func TestEveryRowNamesAVerbThisServerSpells(t *testing.T) {
	t.Parallel()

	for _, exposed := range mapping(t).Interfaces() {
		for _, property := range exposed.Properties {
			if property.Write == nil {
				continue
			}

			if !strings.HasPrefix(property.Write.Verb, verbPrefix) {
				t.Errorf("the write verb of %s.%s is %q, want the prefix %s",
					exposed.Name, property.Name, property.Write.Verb, verbPrefix)
			}

			if property.Write.Arg == "" {
				t.Errorf("the write row of %s.%s names no argument for the value", exposed.Name, property.Name)
			}
		}

		for _, method := range exposed.Methods {
			if !strings.HasPrefix(method.Verb, verbPrefix) {
				t.Errorf("the verb of %s.%s is %q, want the prefix %s",
					exposed.Name, method.Name, method.Verb, verbPrefix)
			}
		}
	}
}

// TestAnInstanceIsNamedAfterTheDevice: every instance id contains the device
// identifier of this server and is a valid Interface Instance ID. So the node
// above and this server name a printer by the same identifier.
func TestAnInstanceIsNamedAfterTheDevice(t *testing.T) {
	t.Parallel()

	for _, exposed := range mapping(t).Interfaces() {
		id := exposed.InstanceID(printerOne, 1)

		if !strings.HasPrefix(string(id), printerOne) {
			t.Errorf("the instance id of %s is %q, which does not contain the device identifier", exposed.Name, id)
		}

		if err := id.Validate(); err != nil {
			t.Errorf("the instance id of %s is %q, which is not valid: %v", exposed.Name, id, err)
		}
	}
}

// TestOnlyMappedPathsAreWatched: WatchesPath is true only for a path that a
// property of mapping.json reads, so the bridge drops every other reading.
func TestOnlyMappedPathsAreWatched(t *testing.T) {
	t.Parallel()

	held := mapping(t)

	for path, want := range map[string]bool{
		"/tools/0/nozzle_temp": true,
		"/state":               true,
		"/job/progress":        true,
		"/dialog/title":        false,
		"":                     false,
	} {
		if held.WatchesPath(path) != want {
			t.Errorf("WatchesPath(%q) is %v, want %v", path, held.WatchesPath(path), want)
		}
	}
}

// TestABrokenTableIsRefusedByName: ParseMapping returns a separate error for
// each kind of mistake in a table, so the person who edits mapping.json reads
// which row to change.
func TestABrokenTableIsRefusedByName(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		table string
		want  error
	}{
		"another format version": {
			table: `{"version": 2}`,
			want:  bridge.ErrMappingVersion,
		},
		"an interface nothing declares": {
			table: `{"version":1,"interfaces":[{"interface":"Nothing1","instance":"{device}"}]}`,
			want:  bridge.ErrUnknownInterface,
		},
		"a property the interface does not declare": {
			table: `{"version":1,"interfaces":[{"interface":"Controller1","instance":"{device}",
				"properties":[{"name":"tempBoard","state":"/x"}]}]}`,
			want: bridge.ErrUnknownProperty,
		},
		"a method the interface does not declare": {
			table: `{"version":1,"interfaces":[{"interface":"Controller1","instance":"{device}",
				"methods":[{"name":"Explode","verb":"core.command.beep"}]}]}`,
			want: bridge.ErrUnknownMethod,
		},
		"a state the vocabulary does not have": {
			table: `{"version":1,"interfaces":[{"interface":"Printer1","instance":"{device}",
				"methods":[{"name":"Beep","verb":"core.command.beep","onlyIn":["IDLE"]}]}]}`,
			want: bridge.ErrUnknownState,
		},
		"a value table nothing declares": {
			table: `{"version":1,"interfaces":[{"interface":"Controller1","instance":"{device}",
				"properties":[{"name":"tempCPU","state":"/x","enum":"nothing"}]}]}`,
			want: bridge.ErrUnknownEnum,
		},
		"an argument naming a value table nothing declares": {
			table: `{"version":1,"interfaces":[{"interface":"FDMPrinterHead1","instance":"{device}",
				"methods":[{"name":"LoadFilament","verb":"core.command.load_filament",
				"args":[{"arg":"filament","param":0,"enum":"nothing"}]}]}]}`,
			want: bridge.ErrUnknownEnum,
		},
		"a printer name that converts to another value": {
			table: `{"version":1,"enums":{"fdmMaterial":{"default":"OTHER",
				"values":{"FLEX":"TPU"},"printerNames":{"PLA":"FLEX"}}}}`,
			want: bridge.ErrPrinterName,
		},
		"an instance that names no device": {
			table: `{"version":1,"interfaces":[{"interface":"Controller1","instance":"1"}]}`,
			want:  bridge.ErrBadInstance,
		},
		"a verb for a property the device alone sets": {
			table: `{"version":1,"interfaces":[{"interface":"Controller1","instance":"{device}",
				"properties":[{"name":"tempCPU","state":"/x",
				"write":{"verb":"core.command.beep","arg":"x"}}]}]}`,
			want: bridge.ErrNotWritable,
		},
		"a writable property with no verb": {
			table: `{"version":1,"interfaces":[{"interface":"Chamber1","instance":"{device}",
				"properties":[{"name":"ledIntensity","state":"/x"}]}]}`,
			want: bridge.ErrNoWriteVerb,
		},
		"one property named twice": {
			table: `{"version":1,"interfaces":[{"interface":"Controller1","instance":"{device}",
				"properties":[{"name":"tempCPU","state":"/x"},{"name":"tempCPU","state":"/y"}]}]}`,
			want: bridge.ErrDuplicateRow,
		},
		"an argument naming a parameter the method has not": {
			table: `{"version":1,"interfaces":[{"interface":"Printer1","instance":"{device}",
				"methods":[{"name":"Beep","verb":"core.command.beep",
				"args":[{"arg":"x","param":0}]}]}]}`,
			want: bridge.ErrBadParam,
		},
		"a letter naming a parameter the method has not": {
			table: `{"version":1,"interfaces":[{"interface":"Printer1","instance":"{device}",
				"methods":[{"name":"Beep","verb":"core.command.beep",
				"args":[{"arg":"x","letters":[{"param":3,"letter":"X"}]}]}]}]}`,
			want: bridge.ErrBadParam,
		},
		"an interface neither exposed nor named": {
			table: `{"version":1,"interfaces":[]}`,
			want:  bridge.ErrUncovered,
		},
		"an absent entry for an interface nothing declares": {
			table: `{"version":1,"absent":[{"interface":"Nothing1","reason":"none"}]}`,
			want:  bridge.ErrUnknownInterface,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := bridge.ParseMapping(registry(t), []byte(tc.table))
			if !errors.Is(err, tc.want) {
				t.Fatalf("ParseMapping returned %v, want %v", err, tc.want)
			}
		})
	}
}

// TestAPropertyThatIsNeverNullHasToBeProvided: ParseMapping refuses an "absent"
// entry for a property that may not be null, because the bridge reports an
// absent property as null.
func TestAPropertyThatIsNeverNullHasToBeProvided(t *testing.T) {
	t.Parallel()

	table := `{"version":1,
		"interfaces":[{"interface":"UpgradableDevice1","instance":"{device}"}],
		"absent":[
			{"interface":"UpgradableDevice1","property":"firmwareVersion","reason":"none"},
			{"interface":"UpgradableDevice1","method":"UpgradeFromFile","reason":"none"},
			{"interface":"Device1","reason":"none"},
			{"interface":"Controller1","reason":"none"},
			{"interface":"Printer1","reason":"none"},
			{"interface":"FDMPrinter1","reason":"none"},
			{"interface":"SLAPrinter1","reason":"none"},
			{"interface":"FDMPrinterHead1","reason":"none"},
			{"interface":"PrintJob1","reason":"none"},
			{"interface":"FDMPrintJob1","reason":"none"},
			{"interface":"FilamentSlot1","reason":"none"},
			{"interface":"Chamber1","reason":"none"}
		]}`

	_, err := bridge.ParseMapping(registry(t), []byte(table))
	if !errors.Is(err, bridge.ErrAbsentNotNullable) {
		t.Fatalf("ParseMapping returned %v, want ErrAbsentNotNullable", err)
	}
}

// TestATableWithAKeyThisBuildDoesNotReadIsRefused: ParseMapping refuses a table
// with a JSON member it does not know, so a misspelt member is an error and is
// not ignored.
func TestATableWithAKeyThisBuildDoesNotReadIsRefused(t *testing.T) {
	t.Parallel()

	for name, table := range map[string]string{
		"a key nothing reads": `{"version":1,"interface":[]}`,
		"not json":            `{`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := bridge.ParseMapping(registry(t), []byte(table)); err == nil {
				t.Fatal("ParseMapping accepted a table it must refuse")
			}
		})
	}
}

// TestADeviceNameOnTheNetwork checks the name a device takes on the Fractal
// network. An Interface Instance ID follows the Identifier grammar, and the
// identifier this server gives a printer is a UUID with hyphens.
func TestADeviceNameOnTheNetwork(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name   string
		device string
		want   string
		usable bool
	}{
		{
			name:   "a uuid loses its hyphens",
			device: "0a29c74c-4984-4515-8267-d12b993a5373",
			want:   "0a29c74c49844515" + "8267d12b993a5373",
			usable: true,
		},
		{
			name:   "an identifier the grammar takes is unchanged",
			device: "DEMO_01",
			want:   "DEMO_01",
			usable: true,
		},
		{
			name:   "a digit may start an instance id",
			device: "1",
			want:   "1",
			usable: true,
		},
		{name: "a name with a space is no name on the network", device: "back office"},
		{name: "a name with a dot is no name on the network", device: "printer.one"},
		{name: "an empty identifier is no name on the network", device: ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			got, usable := bridge.InstanceName(row.device)

			if usable != row.usable {
				t.Fatalf("InstanceName(%q) is usable %v and it is %v", row.device, usable, row.usable)
			}

			if got != row.want {
				t.Errorf("InstanceName(%q) = %q, want %q", row.device, got, row.want)
			}
		})
	}
}

// TestTheNameOnTheNetworkIsTheSameEveryTime: the name a device takes is worked
// out from its identifier alone, so the interfaces of one printer keep their
// instance after a restart and a command from the server above still finds the
// printer.
func TestTheNameOnTheNetworkIsTheSameEveryTime(t *testing.T) {
	t.Parallel()

	device := "0a29c74c-4984-4515-8267-d12b993a5373"

	first, _ := bridge.InstanceName(device)
	second, _ := bridge.InstanceName(device)

	if first != second {
		t.Fatalf("the same identifier gave %q and then %q", first, second)
	}

	if again, _ := bridge.InstanceName(first); again != first {
		t.Errorf("the name %q gave %q when it was worked out again", first, again)
	}
}
