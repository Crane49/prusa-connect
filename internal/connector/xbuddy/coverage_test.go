package xbuddy_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// firmwareFloor is the websocket floor every shipped band starts at.
const firmwareFloor = "6.4.0"

func driverBaseline() connectorapi.Set {
	return xbuddy.NewDriver(xbuddy.ModeToken, 1, 0, "", slog.New(slog.DiscardHandler)).Baseline()
}

func identityOf(model string) connectorapi.Identity {
	return connectorapi.Identity{
		Vendor:   "Prusa",
		Model:    model,
		Firmware: connectorapi.NewFirmwareLabel(firmwareFloor),
		Serial:   "SN-" + model,
	}
}

// A printer no profile covers is held to what this driver's profiles describe
// between them, and never to the whole compiled baseline. It is measured
// against the real driver and one real profile because that is the shape a
// dialect has before its second model is recorded, and because a fixture
// cannot show which way the numbers move.
func TestAnUnrecognisedPrinterIsHeldToWhatTheProfilesCover(t *testing.T) {
	t.Parallel()

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	mini, ok := profiles.Match("MINI", connectorapi.NewFirmwareLabel(firmwareFloor))
	if !ok {
		t.Fatalf("no profile covers MINI at %s", firmwareFloor)
	}

	one := profile.Registry{Schema: profile.CurrentSchema, Profiles: []profile.Profile{mini}}
	baseline := driverBaseline()
	resolver := capability.NewResolver(one, slog.New(slog.DiscardHandler))

	recognised := resolver.Resolve(t.Context(), baseline, identityOf("MINI"), nil, 1)
	unknown := resolver.Resolve(t.Context(), baseline, identityOf("MK9000"), nil, 1)

	if !recognised.ProfileMatched {
		t.Fatal("the model the registry covers reported no profile")
	}

	if unknown.ProfileMatched {
		t.Fatal("a model nothing covers reported a profile")
	}

	if len(recognised.Capabilities) >= len(baseline) {
		t.Fatalf("the profile resolves %d of the baseline's %d keys, so it subtracts nothing "+
			"and this test compares two identical sets",
			len(recognised.Capabilities), len(baseline))
	}

	if len(unknown.Capabilities) > len(recognised.Capabilities) {
		t.Errorf("an unrecognised printer resolves %d capabilities where the one model this driver "+
			"knows resolves %d, so the unknown device is offered the wider set",
			len(unknown.Capabilities), len(recognised.Capabilities))
	}

	for _, key := range unknown.Capabilities.Keys() {
		if !mini.Capabilities.Has(key) {
			t.Errorf("%s reaches a printer nothing recognises and no profile this driver ships declares it", key)
		}
	}
}

// Every key this driver can put on the wire is declared by at least one shipped
// profile, so the thirteen of them cover the whole baseline and an unrecognised
// printer is narrowed by nothing. That is a property of the data rather than of
// the merge, and it is pinned here because the day it stops holding is the day
// a verb starts being withheld from a stranger's printer.
func TestTheShippedProfilesCoverTheWholeDriverBaseline(t *testing.T) {
	t.Parallel()

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	baseline := driverBaseline()
	declared := map[connectorapi.Capability]bool{}

	for _, candidate := range profiles.Profiles {
		for _, key := range candidate.Capabilities.Keys() {
			declared[key] = true
		}
	}

	if len(baseline) == 0 || len(declared) == 0 {
		t.Fatalf("the baseline holds %d keys and the profiles declare %d, so this compares nothing",
			len(baseline), len(declared))
	}

	for _, key := range baseline.Keys() {
		if !declared[key] {
			t.Errorf("%s is in this driver's baseline and no shipped profile declares it, so a printer "+
				"nothing recognises is no longer offered it. That is the intended direction: record the "+
				"profile that should carry it, or record here that the verb is deliberately unrecorded",
				key)
		}
	}
}

// verbsNoCallerCanSend is every command verb a shipped profile declares that
// this driver does not publish, with the reason for each.
//
// Two of the three are deliberate and their reason is in sentNotInvokable
// (verbs.go): the driver sends them itself, inside the transfer exchange, and a
// caller reaches that exchange through Transfers.Send. The third has no wire
// form at all and is in notPorted (limits_test.go).
//
// The profile data is extracted from the shipping firmware matrix, which
// records what a printer supports and not what this server can send, so a verb
// here is a divergence between the two and never a defect in the data.
var verbsNoCallerCanSend = map[connectorapi.Capability]string{
	"core.command.start_inline_download":  "sent inside the transfer exchange, so a caller reaches it through Send",
	"core.command.start_connect_download": "the same exchange under this dialect's other name for it",
	"core.command.start_encrypted_download": "" +
		"no wire form at all: it names a second, encrypted channel this host does not serve, so " +
		"starting it would leave a device fetching from somewhere nothing is listening",
}

// TestNoShippedProfileDeclaresACommandThisDriverCannotSend is the direction
// TestTheShippedProfilesCoverTheWholeDriverBaseline does not read.
//
// That one checks baseline ⊆ profiles, so a verb in the profile data and in no
// build of this driver passes it. Such a verb is published: it takes an action
// in internal/authz, it is served by GET /api/v1/vocabularies/capabilities, and
// the capabilities page documents its arguments. A reader has no way to tell
// that nothing can put it on a wire.
//
// It reads command verbs alone. A feature key describes the machine rather than
// a frame this driver writes, so a profile is free to declare one this driver
// does not read, and the old backlog carries what those sixteen keys are worth.
func TestNoShippedProfileDeclaresACommandThisDriverCannotSend(t *testing.T) {
	t.Parallel()

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	sendable := driverBaseline()
	explained := map[connectorapi.Capability]bool{}
	compared := 0

	for _, candidate := range profiles.Profiles {
		for _, key := range candidate.Capabilities.Keys() {
			verb, isCommand := commandVerbOf(key)
			if !isCommand {
				continue
			}

			compared++

			if sendable.Has(key) {
				continue
			}

			reason, known := verbsNoCallerCanSend[verb]
			if !known {
				t.Errorf("%s declares %s and this driver publishes no such key, so a caller who sends "+
					"it is refused with CapabilityNotSupported for a verb the server cannot put on a "+
					"wire. Give it a wire form, or record it here with the reason", candidate.Model, key)

				continue
			}

			explained[verb] = true

			if reason == "" {
				t.Errorf("%s is listed as a verb no caller can send, with no reason", verb)
			}
		}
	}

	for verb := range verbsNoCallerCanSend {
		if !explained[verb] {
			t.Errorf("%s is listed as a verb no caller can send, and every shipped profile that "+
				"declares it is now answered. Either it gained a wire form, or the profile data "+
				"stopped declaring it, and this row is stale either way", verb)
		}
	}

	if compared == 0 {
		t.Error("no shipped profile declares a command verb, so this test compared nothing")
	}
}

// commandVerbOf returns the verb a capability key belongs to, and whether it is
// a command at all. An argument key is written verb#argument, so it answers to
// its verb's own row rather than needing one.
func commandVerbOf(key connectorapi.Capability) (connectorapi.Capability, bool) {
	verb, _, _ := strings.Cut(string(key), "#")

	if !strings.HasPrefix(verb, "core.command.") {
		return "", false
	}

	return connectorapi.Capability(verb), true
}
