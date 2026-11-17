package bridge_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// build returns a bridge on the fake host and the fake node, with the clock
// now and the principal "node:hq".
func build(t *testing.T, fleet *fakeFleet, node *fakeNode, now func() time.Time) *bridge.Bridge {
	t.Helper()

	made, err := bridge.New(bridge.Config{
		Fleet:     fleet,
		Node:      node,
		Mapping:   mapping(t),
		Principal: func() string { return "node:hq" },
		Now:       now,
	})
	if err != nil {
		t.Fatalf("build the bridge: %v", err)
	}

	return made
}

// fixed returns a clock that always returns at, so a test knows the timestamp
// of every report.
func fixed(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

// TestABridgeRefusesWhatItCannotRunWithout: New returns ErrNoFleet, ErrNoNode
// or ErrNoMapping for a Config that lacks the Fleet, the Node or the Mapping.
func TestABridgeRefusesWhatItCannotRunWithout(t *testing.T) {
	t.Parallel()

	held := mapping(t)

	for name, tc := range map[string]struct {
		cfg  bridge.Config
		want error
	}{
		"no fleet":   {cfg: bridge.Config{Node: newNode(), Mapping: held}, want: bridge.ErrNoFleet},
		"no node":    {cfg: bridge.Config{Fleet: newFleet(), Mapping: held}, want: bridge.ErrNoNode},
		"no mapping": {cfg: bridge.Config{Fleet: newFleet(), Node: newNode()}, want: bridge.ErrNoMapping},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := bridge.New(tc.cfg); err != tc.want { //nolint:errorlint // the sentinel itself.
				t.Fatalf("New returned %v, want %v", err, tc.want)
			}
		})
	}
}

// TestADeviceBecomesInterfaceInstances: for one device, the bridge registers
// every interface of the mapping table. Each instance id comes from the
// instance template and the device identifier.
func TestADeviceBecomesInterfaceInstances(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	want := map[fractal.InterfaceName]fractal.InstanceID{
		"Device1":           fractal.InstanceID(printerOne),
		"UpgradableDevice1": fractal.InstanceID(printerOne),
		"Printer1":          fractal.InstanceID(printerOne),
		"FDMPrinter1":       fractal.InstanceID(printerOne),
		"FDMPrinterHead1":   fractal.InstanceID(printerOne + "_1"),
		"PrintJob1":         fractal.InstanceID(printerOne),
		"FDMPrintJob1":      fractal.InstanceID(printerOne),
	}

	if len(node.instances) != len(want) {
		t.Fatalf("%d interface instances are registered, want %d", len(node.instances), len(want))
	}

	for _, inst := range node.instances {
		expected, exposed := want[inst.Name]
		if !exposed {
			t.Errorf("%s is registered, and the mapping table does not have it", inst.Name)

			continue
		}

		if inst.Instance != expected {
			t.Errorf("the instance id of %s is %q, want %q", inst.Name, inst.Instance, expected)
		}

		if inst.Definition == nil {
			t.Errorf("%s is registered with no definition, so the Fractal node cannot validate its messages", inst.Name)
		}

		if err := inst.Instance.Validate(); err != nil {
			t.Errorf("%s is not a valid Interface Instance ID: %v", inst.Instance, err)
		}
	}
}

// TestADeviceWhoseIdentifierIsNotAnInstanceIDIsNotShown: when the identifier
// of a device is not a valid Interface Instance ID, the bridge registers no
// interface for that device. The network would refuse such an instance.
func TestADeviceWhoseIdentifierIsNotAnInstanceIDIsNotShown(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer("not a name", telemetry()))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	if len(node.instances) != 0 {
		t.Fatalf("%d interface instances are registered for a device whose identifier is not valid, want 0",
			len(node.instances))
	}
}

// TestTheFirstReportCarriesTheWholeReading: the first flush of a device sends
// every property of every interface, with the values converted.
func TestTheFirstReportCarriesTheWholeReading(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	made.Flush(t.Context())

	for name, want := range map[fractal.InterfaceName]fractal.Props{
		"Printer1": {
			"state": json.RawMessage(`"PRINTING"`),
			"speed": json.RawMessage(`100`),
		},
		"FDMPrinterHead1": {
			"tempNozzle":     json.RawMessage(`214.8`),
			"targetNozzle":   json.RawMessage(`230`),
			"material":       json.RawMessage(`"PETG"`),
			"fanExtruder":    json.RawMessage(`7000`),
			"fanPrint":       json.RawMessage(`5200`),
			"nozzleType":     json.RawMessage(`null`),
			"nozzleDiameter": json.RawMessage(`0.4`),
			"nozzleHighFlow": json.RawMessage(`false`),
		},
		"Device1": {
			"vendor":       json.RawMessage(`"Prusa Research"`),
			"type":         json.RawMessage(`"printer"`),
			"model":        json.RawMessage(`"MK4"`),
			"serialNumber": json.RawMessage(`"SN01K3Q7"`),
			"uuid":         json.RawMessage(`null`),
		},
	} {
		instance := fractal.InstanceID(printerOne)
		if name == "FDMPrinterHead1" {
			instance = fractal.InstanceID(printerOne + "_1")
		}

		seen := node.seenOf(name, instance)
		if len(seen) != 1 {
			t.Fatalf("the bridge sent %d reports for %s, want 1", len(seen), name)
		}

		comparePropsExactly(t, name, seen[0].props, want)

		if seen[0].at != fractal.TimestampOf(at()) {
			t.Errorf("the timestamp of the %s report is %d, want %d", name, seen[0].at, fractal.TimestampOf(at()))
		}
	}
}

// comparePropsExactly fails the test when the report lacks a wanted property,
// has another value for one, or has a property the test did not name.
func comparePropsExactly(t *testing.T, name fractal.InterfaceName, got, want fractal.Props) {
	t.Helper()

	for prop, value := range want {
		held, sent := got[prop]
		if !sent {
			t.Errorf("%s.%s was not reported", name, prop)

			continue
		}

		if string(held) != string(value) {
			t.Errorf("%s.%s is %s, want %s", name, prop, held, value)
		}
	}

	for prop := range got {
		if _, named := want[prop]; !named {
			t.Errorf("%s.%s was reported, and the test does not expect it", name, prop)
		}
	}
}

// TestOnlyWhatMovedTravels: a second flush of a device that did not change
// sends no report. After one reading changes, the flush sends a report with
// that one property and the time of the reading (fractal.md, Reported
// Properties Message).
func TestOnlyWhatMovedTravels(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	made.Flush(t.Context())

	first := len(node.seen())

	made.Flush(t.Context())

	if len(node.seen()) != first {
		t.Fatalf("a flush of a device that did not change sent %d reports, want 0",
			len(node.seen())-first)
	}

	made.Sample(t.Context(), printerOne, "/tools/0/nozzle_temp", json.RawMessage(`215.4`), at().Add(time.Second))
	made.Flush(t.Context())

	sent := node.seenOf("FDMPrinterHead1", fractal.InstanceID(printerOne+"_1"))
	if len(sent) != 2 {
		t.Fatalf("the bridge sent %d reports for the print head, want 2", len(sent))
	}

	comparePropsExactly(t, "FDMPrinterHead1", sent[1].props,
		fractal.Props{"tempNozzle": json.RawMessage(`215.4`)})

	if want := fractal.TimestampOf(at().Add(time.Second)); sent[1].at != want {
		t.Errorf("the timestamp of the report is %d, want the time of the reading %d", sent[1].at, want)
	}
}

// TestABurstBecomesOneReportAndTheLastValueWins: many readings of one path
// between two flushes become one report, with the last value.
func TestABurstBecomesOneReportAndTheLastValueWins(t *testing.T) {
	t.Parallel()

	const samples = 100

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	made.Flush(t.Context())

	before := len(node.seen())

	for i := range samples {
		made.Sample(t.Context(), printerOne, "/tools/0/nozzle_temp",
			json.RawMessage(fmt.Sprintf("%d.5", 200+i)), at().Add(time.Duration(i)*time.Millisecond))
	}

	made.Flush(t.Context())

	sent := node.seen()[before:]
	if len(sent) != 1 {
		t.Fatalf("%d readings became %d reports, want 1", samples, len(sent))
	}

	want := json.RawMessage(fmt.Sprintf("%d.5", 200+samples-1))
	if held := sent[0].props["tempNozzle"]; string(held) != string(want) {
		t.Fatalf("the report has the value %s, want the last value %s", held, want)
	}
}

// TestAPathNothingReadsIsDropped: a reading at a path that no property of the
// mapping table reads causes no report.
func TestAPathNothingReadsIsDropped(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	made.Flush(t.Context())

	before := len(node.seen())

	made.Sample(t.Context(), printerOne, "/dialog/title", json.RawMessage(`"Insert filament"`), at())
	made.Flush(t.Context())

	if len(node.seen()) != before {
		t.Fatalf("a reading at a path no property reads caused %d reports, want 0", len(node.seen())-before)
	}
}

// TestADeviceThatGoesAwayReportsNothingKnown: after the host stops listing a
// printer, the bridge reports its properties as null, and not as the last
// values the printer sent.
func TestADeviceThatGoesAwayReportsNothingKnown(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	made.Flush(t.Context())
	fleet.remove(printerOne)

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices again: %v", err)
	}

	made.Flush(t.Context())

	sent := node.seenOf("FDMPrinter1", fractal.InstanceID(printerOne))
	if len(sent) != 2 {
		t.Fatalf("the bridge sent %d reports for the printer, want 2", len(sent))
	}

	for _, name := range []fractal.PropertyName{"axisX", "axisY", "axisZ", "tempBed", "targetBed"} {
		if held := sent[1].props[name]; string(held) != "null" {
			t.Errorf("%s is %s after the device went away, want null", name, held)
		}
	}

	if present := made.Devices()[printerOne]; present {
		t.Error("Devices still marks as present a device the host does not list")
	}
}

// TestAWindowOfTheEventStreamReachesTheDevices: Window stores the reading of
// each telemetry envelope. It ignores a reading at a path no property reads,
// an envelope that is not valid JSON, and one whose subject is not a device.
func TestAWindowOfTheEventStreamReachesTheDevices(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	made.Flush(t.Context())

	window := busapi.Window{Telemetry: []json.RawMessage{
		envelope(t, bridge.TelemetrySampled, printerOne,
			`{"path":"/speed","value":80}`, at().Add(time.Second)),
		envelope(t, bridge.TelemetrySampled, printerOne,
			`{"path":"/dialog/title","value":"x"}`, at().Add(time.Second)),
		json.RawMessage(`{ not an envelope`),
		envelopeAbout(t, bridge.TelemetrySampled, "storage:"+printerOne,
			`{"path":"/speed","value":90}`, at().Add(2*time.Second)),
		envelopeAbout(t, bridge.TelemetrySampled, printerOne,
			`{"path":"/speed","value":90}`, at().Add(2*time.Second)),
	}}

	if err := made.Window(t.Context(), window); err != nil {
		t.Fatalf("take one window: %v", err)
	}

	made.Flush(t.Context())

	sent := node.seenOf("Printer1", fractal.InstanceID(printerOne))
	if len(sent) != 2 {
		t.Fatalf("the bridge sent %d reports for Printer1, want 2", len(sent))
	}

	comparePropsExactly(t, "Printer1", sent[1].props, fractal.Props{"speed": json.RawMessage(`80`)})
}

// envelope returns one event envelope about a device as the host encodes it,
// with the subject device:<id>.
func envelope(t *testing.T, kind, device, data string, when time.Time) json.RawMessage {
	t.Helper()

	subject, err := event.NewOrigin(event.KindDevice, device)
	if err != nil {
		t.Fatalf("name the device: %v", err)
	}

	return envelopeAbout(t, kind, subject.String(), data, when)
}

// envelopeAbout returns one event envelope with any subject.
func envelopeAbout(t *testing.T, kind, subject, data string, when time.Time) json.RawMessage {
	t.Helper()

	encoded, err := json.Marshal(event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3Q7X8N4B2VJ5R0YH6M9CDEF",
		Type:        kind,
		Time:        when,
		Subject:     subject,
		Data:        json.RawMessage(data),
	})
	if err != nil {
		t.Fatalf("encode one envelope: %v", err)
	}

	return encoded
}

// TestADurableEventReadsThatDeviceAgain: when an event names a device the
// bridge does not know, the bridge reads that device from the host at once and
// does not wait for the next listing.
func TestADurableEventReadsThatDeviceAgain(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	fleet.devices[printerTwo] = printer(printerTwo, telemetry())
	fleet.order = append(fleet.order, printerTwo)

	window := busapi.Window{Events: []json.RawMessage{
		envelope(t, "com.prusa.connect.device.arrived", printerTwo, `{}`, at()),
	}}

	if err := made.Window(t.Context(), window); err != nil {
		t.Fatalf("take one window: %v", err)
	}

	if present, held := made.Devices()[printerTwo]; !held || !present {
		t.Fatal("the bridge does not know the device the event named")
	}
}

// TestAResyncWindowReadsTheWholeListing: a window with Resync set means the
// subscription missed events, so the bridge reads the whole device listing
// again.
func TestAResyncWindowReadsTheWholeListing(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()), printer(printerTwo, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Window(t.Context(), busapi.Window{Resync: true}); err != nil {
		t.Fatalf("take one window: %v", err)
	}

	held := made.Devices()
	if len(held) != 2 {
		t.Fatalf("the bridge knows %d devices after a resync, want 2", len(held))
	}
}

// TestAFullReportIsTheWholeState: Snapshot returns every property of the
// interface, and the result passes the interface definition. The next flush
// sends nothing, because no value changed after the snapshot (fractal.md, Full
// State Report Request Message).
func TestAFullReportIsTheWholeState(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	owner := node.owner(t, "FDMPrinter1", fractal.InstanceID(printerOne))

	props, _, err := owner.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("take a full report: %v", err)
	}

	definition := node.definition(t, "FDMPrinter1", fractal.InstanceID(printerOne))
	if len(props) != definition.NumProperties() {
		t.Fatalf("the snapshot has %d properties, want all %d of the interface",
			len(props), definition.NumProperties())
	}

	if err = definition.ValidateREP(props); err != nil {
		t.Fatalf("the snapshot does not pass the interface definition: %v", err)
	}

	made.Flush(t.Context())

	if sent := node.seenOf("FDMPrinter1", fractal.InstanceID(printerOne)); len(sent) != 0 {
		t.Fatalf("a flush after a snapshot sent %d reports, want 0", len(sent))
	}
}

// TestAFullReportDuringAFlushLeavesTheLatestValue: a full report requested
// while a flush is sending an older value reaches the node above after that
// value. The node above then holds the latest value, and no later flush has
// to resend it.
func TestAFullReportDuringAFlushLeavesTheLatestValue(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	made.Flush(t.Context())

	instance := fractal.InstanceID(printerOne)
	owner := node.owner(t, "Printer1", instance)
	handle := node.handles[key("Printer1", instance)]
	answered := make(chan struct{})

	// The flush has taken speed 50 and is sending it when the speed changes
	// to 120 and the node above asks for the full state. The Fractal node
	// sends the full report as soon as Snapshot returns.
	node.mu.Lock()
	node.beforeReport = func() {
		made.Sample(t.Context(), printerOne, "/speed", json.RawMessage(`120`), at().Add(2*time.Second))

		go func() {
			defer close(answered)

			props, _, err := owner.Snapshot(t.Context())
			if err != nil {
				t.Errorf("take a full report: %v", err)

				return
			}

			if err = handle.Report(t.Context(), props, 0); err != nil {
				t.Errorf("send the full report: %v", err)
			}
		}()

		select {
		case <-answered:
		case <-time.After(100 * time.Millisecond):
		}
	}
	node.mu.Unlock()

	made.Sample(t.Context(), printerOne, "/speed", json.RawMessage(`50`), at().Add(time.Second))
	made.Flush(t.Context())
	<-answered

	made.Flush(t.Context())

	above := fractal.Props{}
	for _, sent := range node.seenOf("Printer1", instance) {
		maps.Copy(above, sent.props)
	}

	latest, _, err := made.Snapshot(printerOne, "Printer1")
	if err != nil {
		t.Fatalf("take a full report: %v", err)
	}

	if !reflect.DeepEqual(above, latest) {
		t.Errorf("the node above holds %s, want the latest state %s", above, latest)
	}
}

// TestAFullReportLeavesItsTimeToTheNode: Snapshot returns no time, so the
// Fractal node stamps the full report with the time it read the state
// (fractal.md, Reported Properties Message). The time of the newest reading is
// when the state last changed, which is earlier.
func TestAFullReportLeavesItsTimeToTheNode(t *testing.T) {
	t.Parallel()

	for name, later := range map[string]bool{
		"after the first reading": false,
		"after a later reading":   true,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			node := newNode()
			made := build(t, newFleet(printer(printerOne, telemetry())), node, fixed(at()))

			if err := made.Sync(t.Context()); err != nil {
				t.Fatalf("read the devices: %v", err)
			}

			if later {
				made.Sample(t.Context(), printerOne, "/speed", json.RawMessage(`80`), at().Add(time.Second))
			}

			owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))

			_, stamp, err := owner.Snapshot(t.Context())
			if stamp != 0 || err != nil {
				t.Fatalf("Snapshot returned the time %d and the error %v, want 0 and no error", stamp, err)
			}
		})
	}
}

// TestAFullReportOfADeviceThatIsNotHeldIsRefused: Snapshot returns an error for
// a device the bridge does not know, and for an interface that is not in the
// mapping table.
func TestAFullReportOfADeviceThatIsNotHeldIsRefused(t *testing.T) {
	t.Parallel()

	fleet := newFleet(printer(printerOne, telemetry()))
	node := newNode()
	made := build(t, fleet, node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	if _, _, err := made.Snapshot(printerTwo, "Printer1"); err == nil {
		t.Fatal("Snapshot returned no error for a device the bridge does not know")
	}

	if _, _, err := made.Snapshot(printerOne, "Chamber1"); err == nil {
		t.Fatal("Snapshot returned no error for an interface that is not in the mapping table")
	}
}

// TestAPrinterWithAUUIDIsShownUnderItsHexadecimalName: the identifier this
// server gives a printer is a UUID, and an Interface Instance ID takes no
// hyphen. The bridge registers the interfaces under the identifier with its
// hyphens removed, and a command from the server above reaches the printer
// under its own identifier.
func TestAPrinterWithAUUIDIsShownUnderItsHexadecimalName(t *testing.T) {
	t.Parallel()

	const device = "0a29c74c-4984-4515-8267-d12b993a5373"

	name, usable := bridge.InstanceName(device)
	if !usable {
		t.Fatalf("a uuid is no name on the network")
	}

	fleet := newFleet(printer(device, telemetry()))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	if len(node.instances) == 0 {
		t.Fatal("no interface is registered for a printer whose identifier is a uuid")
	}

	for _, inst := range node.instances {
		if err := inst.Instance.Validate(); err != nil {
			t.Errorf("%s is not a valid Interface Instance ID: %v", inst.Instance, err)
		}

		if !strings.HasPrefix(string(inst.Instance), name) {
			t.Errorf("the instance id of %s is %q and the printer's name is %q",
				inst.Name, inst.Instance, name)
		}
	}

	// The way back: the server above names the instance, and the command
	// reaches the printer under the identifier this server gave it.
	owner := node.owner(t, "Printer1", fractal.InstanceID(name))

	if _, err := owner.Invoke(t.Context(), fractalnode.Call{Method: "PauseJob", Caller: "hq"}); err != nil {
		t.Fatalf("pause the print: %v", err)
	}

	if len(fleet.commands) != 1 || fleet.commands[0].device != device {
		t.Fatalf("the command went to %+v, and the printer is %s", fleet.commands, device)
	}
}

// TestTwoPrintersThatDifferOnlyByHyphensAreNotBothShown: two identifiers give
// one name on the network. The bridge shows the first and refuses the second,
// so two printers never report into one interface instance.
func TestTwoPrintersThatDifferOnlyByHyphensAreNotBothShown(t *testing.T) {
	t.Parallel()

	const (
		first  = "0a29c74c-4984-4515-8267-d12b993a5373"
		second = "0a29c74c49844515-8267d12b993a5373"
	)

	fleet := newFleet(printer(first, telemetry()), printer(second, telemetry()))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	shown := map[fractal.InstanceID]struct{}{}
	for _, inst := range node.instances {
		shown[inst.Instance] = struct{}{}
	}

	name, _ := bridge.InstanceName(first)
	if _, held := shown[fractal.InstanceID(name)]; !held {
		t.Fatalf("the first printer is not shown under %q", name)
	}

	// Seven interfaces for one printer and none for the other. The count is the
	// whole claim: a second registration under the same name would raise it.
	if len(node.instances) != 7 {
		t.Fatalf("%d interface instances are registered, and one printer has 7",
			len(node.instances))
	}
}

// TestAPrinterThatIsAwayIsBlocked: a printer that is offline, or that the
// host no longer lists, reports BLOCKED, because Printer1.state may not be
// null, and null for every reading that may be. A reading that arrives while
// the printer is offline, or after the host stopped listing it, changes
// nothing.
func TestAPrinterThatIsAwayIsBlocked(t *testing.T) {
	t.Parallel()

	ready := telemetry()
	ready["/state"] = json.RawMessage(`"READY"`)

	want := map[fractal.InterfaceName]fractal.Props{
		"Printer1": {"state": json.RawMessage(`"BLOCKED"`)},
		"FDMPrinter1": {
			"axisX": json.RawMessage(`null`), "axisY": json.RawMessage(`null`), "axisZ": json.RawMessage(`null`),
			"tempBed": json.RawMessage(`null`), "targetBed": json.RawMessage(`null`),
			"activeSlot": json.RawMessage(`null`),
		},
	}

	for name, leave := range map[string]func(*fakeFleet, *bridge.Bridge){
		"offline": func(fleet *fakeFleet, made *bridge.Bridge) {
			away := fleet.devices[printerOne]
			away.Connected = false
			fleet.devices[printerOne] = away
			made.Touch(t.Context(), printerOne)
		},
		"no longer listed": func(fleet *fakeFleet, made *bridge.Bridge) {
			fleet.remove(printerOne)
			made.Touch(t.Context(), printerOne)
		},
		"a late reading while offline": func(fleet *fakeFleet, made *bridge.Bridge) {
			away := fleet.devices[printerOne]
			away.Connected = false
			fleet.devices[printerOne] = away
			made.Touch(t.Context(), printerOne)
			made.Sample(t.Context(), printerOne, "/state", json.RawMessage(`"READY"`), at().Add(time.Second))
		},
		"a late reading after removal": func(fleet *fakeFleet, made *bridge.Bridge) {
			fleet.remove(printerOne)
			made.Touch(t.Context(), printerOne)
			made.Sample(t.Context(), printerOne, "/state", json.RawMessage(`"READY"`), at().Add(time.Second))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet := newFleet(printer(printerOne, ready))
			made := build(t, fleet, newNode(), fixed(at()))

			if err := made.Sync(t.Context()); err != nil {
				t.Fatalf("read the devices: %v", err)
			}

			leave(fleet, made)

			got := map[fractal.InterfaceName]fractal.Props{}

			for iface := range want {
				props, _, err := made.Snapshot(printerOne, iface)
				if err != nil {
					t.Fatalf("take a full report of %s: %v", iface, err)
				}

				got[iface] = props
			}

			if !reflect.DeepEqual(got, want) {
				t.Errorf("the printer reports %s, want %s", got, want)
			}
		})
	}
}

// TestADeviceUUIDIsSixteenBytesInBase64URL: Device1.uuid is the 16 bytes of
// the device's UUID in unpadded base64url (domain-common.md). An identifier
// that is not a UUID gives null.
func TestADeviceUUIDIsSixteenBytesInBase64URL(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		id   string
		want string
	}{
		"a uuid":              {id: "0a29c74c-4984-4515-8267-d12b993a5373", want: `"CinHTEmERRWCZ9ErmTpTcw"`},
		"not a uuid":          {id: printerOne, want: `null`},
		"dashes out of place": {id: "0a29c74c4984-4515-8267-d12b993a-5373", want: `null`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			made := build(t, newFleet(printer(tc.id, telemetry())), newNode(), fixed(at()))
			if err := made.Sync(t.Context()); err != nil {
				t.Fatalf("read the devices: %v", err)
			}

			props, _, err := made.Snapshot(tc.id, "Device1")
			if err != nil {
				t.Fatalf("take a full report: %v", err)
			}

			want := fractal.Props{
				"vendor": json.RawMessage(`"Prusa Research"`), "type": json.RawMessage(`"printer"`),
				"model": json.RawMessage(`"MK4"`), "serialNumber": json.RawMessage(strconv.Quote("SN" + tc.id[:6])),
				"uuid": json.RawMessage(tc.want),
			}
			if !reflect.DeepEqual(props, want) {
				t.Errorf("the device reads %s, want %s", props, want)
			}
		})
	}
}
