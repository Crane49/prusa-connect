package capability_test

import (
	"context"
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// filesSession is a driver session that can report files and counts what
// reached it, so "the host refused it first" is an observation.
type filesSession struct {
	connectorapi.Session

	storages int
	lists    int
}

func (f *filesSession) Storages(context.Context) ([]connectorapi.Storage, error) {
	f.storages++

	return []connectorapi.Storage{{Mountpoint: "/usb", Name: "usb"}}, nil
}

func (f *filesSession) List(_ context.Context, path string) (connectorapi.Listing, error) {
	f.lists++

	return connectorapi.Listing{Directory: connectorapi.File{Path: path, Folder: true}}, nil
}

// bareSession is a driver session whose dialect has no file vocabulary.
type bareSession struct {
	connectorapi.Session
}

func resolutionOf(keys ...connectorapi.Capability) capability.Resolution {
	set := connectorapi.Set{}
	for _, key := range keys {
		set[key] = connectorapi.Value{Flag: true}
	}

	return capability.Resolution{Capabilities: set, Epoch: 1}
}

func TestAPrinterWhoseSetLacksTheVerbIsNeverAskedForFiles(t *testing.T) {
	t.Parallel()

	driver := &filesSession{}
	session := capability.Wrap(driver, resolutionOf(connectorapi.VerbSendInfo))

	if _, err := session.List(t.Context(), "usb"); err == nil {
		t.Error("a printer whose resolved set has no send_file_info was listed anyway")
	}

	if driver.lists != 0 {
		t.Errorf("%d listings reached the driver, want none", driver.lists)
	}

	storages, err := session.Storages(t.Context())
	if err != nil {
		t.Fatalf("Storages: %v", err)
	}

	if len(storages) != 1 || driver.storages != 1 {
		t.Errorf("the storages half was refused for a set that carries send_info: %+v", storages)
	}
}

func TestAPrinterWhoseSetLacksSendInfoReportsNoStorages(t *testing.T) {
	t.Parallel()

	driver := &filesSession{}
	session := capability.Wrap(driver, resolutionOf(connectorapi.VerbSendFileInfo))

	if _, err := session.Storages(t.Context()); err == nil {
		t.Error("a printer whose resolved set has no send_info was asked for its storages")
	}

	if driver.storages != 0 {
		t.Errorf("%d storage reads reached the driver, want none", driver.storages)
	}
}

// A dialect with no file vocabulary is refused with the code that says the
// printer cannot do it, not with an error nothing above can read.
func TestADialectWithNoFilesystemIsRefusedWithACode(t *testing.T) {
	t.Parallel()

	session := capability.Wrap(&bareSession{},
		resolutionOf(connectorapi.VerbSendInfo, connectorapi.VerbSendFileInfo))

	for name, call := range map[string]func() error{
		"storages": func() error { _, err := session.Storages(t.Context()); return err },
		"list":     func() error { _, err := session.List(t.Context(), "usb"); return err },
	} {
		err := call()
		if err == nil {
			t.Errorf("%s answered for a dialect that cannot report files", name)

			continue
		}

		if !errors.Is(err, connectorapi.ErrNoFilesystem) {
			t.Errorf("%s reported %v, want one carrying ErrNoFilesystem", name, err)
		}

		var refusal *fault.Fault
		if !errors.As(err, &refusal) ||
			refusal.Code() != connectorapi.CapabilityNotSupported.Code() {
			t.Errorf("%s reported %v, want %s", name, err, connectorapi.CapabilityNotSupported.Code())
		}
	}
}

func TestAListingReachesTheDriverWhenTheSetCarriesTheVerb(t *testing.T) {
	t.Parallel()

	driver := &filesSession{}
	session := capability.Wrap(driver, resolutionOf(connectorapi.VerbSendFileInfo))

	listing, err := session.List(t.Context(), "usb/models")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if listing.Directory.Path != "usb/models" || driver.lists != 1 {
		t.Errorf("the driver saw %d listings and returned %+v", driver.lists, listing)
	}
}

// acceptingSession is a driver that would carry out anything it is handed, and
// counts what reached it. It stands for a connector that never called
// connectorapi.RefusePathEscape.
type acceptingSession struct {
	connectorapi.Session

	calls int
}

func (a *acceptingSession) Storages(context.Context) ([]connectorapi.Storage, error) {
	a.calls++

	return nil, nil
}

func (a *acceptingSession) List(context.Context, string) (connectorapi.Listing, error) {
	a.calls++

	return connectorapi.Listing{}, nil
}

func (a *acceptingSession) CreateFolder(context.Context, string) error {
	a.calls++

	return nil
}

func (a *acceptingSession) DeleteFile(context.Context, string) error {
	a.calls++

	return nil
}

func (a *acceptingSession) DeleteFolder(context.Context, string) error {
	a.calls++

	return nil
}

func (a *acceptingSession) Send(context.Context, connectorapi.Upload) error {
	a.calls++

	return nil
}

// A climbing path is refused above the driver, so the rule holds for a
// connector that never called connectorapi.RefusePathEscape.
//
// The published routes say such a path never reaches the printer, and this
// holds the claim to the surface rather than to one driver. Enforcement in a
// driver alone makes the claim true of that driver and of nothing else.
func TestAClimbingPathIsRefusedAboveTheDriver(t *testing.T) {
	t.Parallel()

	everything := resolutionOf(
		connectorapi.VerbSendFileInfo,
		connectorapi.VerbCreateFolder,
		connectorapi.VerbDeleteFile,
		connectorapi.VerbDeleteFolder,
		connectorapi.VerbFileUpload,
	)

	for _, path := range []string{"../secret.gcode", "usb/../../etc/passwd", "..", "usb/.."} {
		driver := &acceptingSession{}
		session := capability.Wrap(driver, everything)

		calls := map[string]func() error{
			"list":          func() error { _, err := session.List(t.Context(), path); return err },
			"create folder": func() error { return session.CreateFolder(t.Context(), path) },
			"delete file":   func() error { return session.DeleteFile(t.Context(), path) },
			"delete folder": func() error { return session.DeleteFolder(t.Context(), path) },
			"send": func() error {
				return session.Send(t.Context(), connectorapi.Upload{Path: path})
			},
		}

		for name, call := range calls {
			err := call()
			if err == nil {
				t.Errorf("%s accepted %q", name, path)

				continue
			}

			var refusal *fault.Fault
			if !errors.As(err, &refusal) || refusal.Code() != connectorapi.PathNotAllowed.Code() {
				t.Errorf("%s refused %q with %v, want %s", name, path, err,
					connectorapi.PathNotAllowed.Code())
			}
		}

		if driver.calls != 0 {
			t.Errorf("%d calls carrying %q reached a driver that would have obeyed them",
				driver.calls, path)
		}
	}
}

// A path that stays inside the storage still reaches the driver, so what was
// added is a refusal and not a wall.
func TestAPathThatStaysInsideStillReachesTheDriver(t *testing.T) {
	t.Parallel()

	driver := &acceptingSession{}
	session := capability.Wrap(driver, resolutionOf(
		connectorapi.VerbSendFileInfo, connectorapi.VerbDeleteFile, connectorapi.VerbFileUpload))

	if _, err := session.List(t.Context(), "usb/models"); err != nil {
		t.Errorf("List: %v", err)
	}

	if err := session.DeleteFile(t.Context(), "usb/models/part.gcode"); err != nil {
		t.Errorf("DeleteFile: %v", err)
	}

	if err := session.Send(t.Context(),
		connectorapi.Upload{Path: "usb/part.gcode"}); err != nil {
		t.Errorf("Send: %v", err)
	}

	if driver.calls != 3 {
		t.Errorf("%d of 3 calls reached the driver", driver.calls)
	}
}

// TestADialectWithNoTransferHalfIsRefusedAndSaysWhy holds the branch that tells
// the two "this printer cannot be sent a file" answers apart.
//
// Session.transfers refuses twice for one reason. The resolved set may not carry
// the verb, which is a printer that cannot take a file. The driver may have no
// Send at all, which is a dialect that cannot put one on any printer. A caller
// sees the same code either way, on purpose, and the wrapped ErrNoTransfers is
// what an operator's log says which happened.
//
// Nothing reached the second branch. Every driver in the fleet and in this
// package satisfies Transfers, so replacing it with `return nil, nil` left the
// printer, driver and fleet suites green.
func TestADialectWithNoTransferHalfIsRefusedAndSaysWhy(t *testing.T) {
	t.Parallel()

	// A driver with no Send, which is what a dialect with no transfer
	// vocabulary looks like from the host. The set says the printer takes a
	// file, so the verb is not what refuses this.
	noTransfers := capability.Wrap(newSpy(), resolutionOf(connectorapi.VerbFileUpload))

	err := noTransfers.Send(t.Context(), connectorapi.Upload{Path: "usb/part.gcode"})
	if err == nil {
		t.Fatal("a driver with no transfer half accepted a file")
	}

	if !errors.Is(err, connectorapi.ErrNoTransfers) {
		t.Errorf("the refusal is %v, and nothing in it wraps ErrNoTransfers, so a reader cannot "+
			"tell a dialect that cannot transfer from a printer that may not be asked", err)
	}

	var refusal *fault.Fault
	if !errors.As(err, &refusal) || refusal.Code() != connectorapi.CapabilityNotSupported.Code() {
		t.Fatalf("the refusal is %v, want %s", err, connectorapi.CapabilityNotSupported.Code())
	}

	if !hasVerbParam(refusal, connectorapi.VerbFileUpload) {
		t.Errorf("the refusal carries %v, and not the verb it refused", refusal.Params())
	}

	// The other branch, so the assertion above is about the driver's shape and
	// not about every refusal this method makes. A driver that can transfer, and
	// a set that does not carry the verb, answers the same code and wraps
	// nothing.
	withTransfers := capability.Wrap(&acceptingSession{}, resolutionOf(connectorapi.VerbSendFileInfo))

	err = withTransfers.Send(t.Context(), connectorapi.Upload{Path: "usb/part.gcode"})
	if err == nil {
		t.Fatal("a printer whose set does not carry the verb was sent a file")
	}

	if errors.Is(err, connectorapi.ErrNoTransfers) {
		t.Error("a driver that can transfer reported that it cannot, so the two refusals are one")
	}

	if !errors.As(err, &refusal) || refusal.Code() != connectorapi.CapabilityNotSupported.Code() {
		t.Errorf("the refusal is %v, want %s", err, connectorapi.CapabilityNotSupported.Code())
	}
}

// hasVerbParam reports whether a refusal names the verb it refused, which is
// the one thing a caller can act on.
func hasVerbParam(refusal *fault.Fault, verb connectorapi.Capability) bool {
	for _, param := range refusal.Params() {
		if param.Key == "verb" && param.Value.String() == verb.String() {
			return true
		}
	}

	return false
}
