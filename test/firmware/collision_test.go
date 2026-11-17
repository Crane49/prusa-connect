package firmware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The two names BUG-014 was found with, kept verbatim so the bug file and this
// scenario name the same thing. Both shorten to BUG014~1.GCO: FatFs's
// gen_numname appends ~1 after six body characters (ff.c:1998-2013), so the
// seventh character on is what the two names differ by and the short name
// cannot tell them apart.
const (
	collidingSource  = "s08-colliding.gcode"
	collidingProbe   = printerStorage + "/bug014-probe.gcode"
	collidingControl = printerStorage + "/bug014-control.gcode"
)

// transferNotOnStorage is the fault the fixed readback refuses a transfer with
// when the printer's own listing does not hold the name that was sent.
const transferNotOnStorage = "core.printer/TRANSFER_NOT_ON_STORAGE"

// TestAShortNameCollisionIsNotReportedDoneOnRealFirmware is BUG-014's readback
// against a real FatFs.
//
// The live CORE One L answered a clean `done` for a second file that landed
// nowhere: the firmware's pre-flight `stat` cannot see a long name that shares
// an 8.3 short name (ff.c:2414 skips the short-name comparison for a lossy
// name), its landing sequence failed part way, and it reported success
// regardless (transfer.cpp:527-541). The host half of the fix stopped believing
// that report and reads the destination directory back.
//
// What this scenario establishes is which of the two honest answers the fix
// gives on firmware whose FatFs is the real one, and what that FatFs actually
// did. It is deliberately written to accept either: the fault, when the listing
// conclusively does not hold the name, or `done` with a warning, when the board
// could not be asked in time. What it does not accept is the answer the bug was
// filed for, a clean `done` for a file the printer does not have, and it
// requires the listing afterwards to agree with whatever the caller was told.
//
// A board that disambiguates to BUG014~2 and lands both files satisfies it too,
// with both rows done and both names listed. That outcome would say the live
// CORE One L behaves differently from this emulated MK4, which is worth as much
// as a reproduction.
func TestAShortNameCollisionIsNotReportedDoneOnRealFirmware(t *testing.T) {
	requireNotBlocked(t)

	if err := shortNameCollisionIsNotReportedDone(t); err != nil {
		t.Fatalf("%v", err)
	}
}

// shortNameCollisionIsNotReportedDone reports whether the collision can be
// measured on this machine, and what it did.
//
// It is written as a probe because the whole scenario rests on one thing the
// emulated board may not do: answer SEND_FILE_INFO about its own stick. Every
// assertion here compares what the caller was told against that listing, so a
// machine that will not produce one leaves nothing to assert rather than
// something to fail.
func shortNameCollisionIsNotReportedDone(t *testing.T) error {
	t.Helper()

	rg := startRigWithStick(t)

	view, err := awaitConnected(t, rg)
	if err != nil {
		return err
	}

	// The precondition, taken before anything is transferred: the machine has to
	// be able to say what is on its stick. This is the readback BUG-014's fix is
	// built on, and a board that cannot answer it is the fix's own
	// could-not-be-asked branch rather than a printer this scenario can judge.
	before, err := rg.server.deviceFiles(t.Context(), view.ID)
	if err != nil {
		return fmt.Errorf("the machine would not say what is on its stick before any transfer, so "+
			"there is no listing for a landing to be judged against: %w", err)
	}

	t.Logf("observed: %s holds %s before any transfer", printerStorage, describe(before))

	// The body is the travel print, because a file a printer will not accept as
	// a print file is a refusal for a reason that is not this bug's.
	body := travelPrint()

	if err = os.WriteFile(filepath.Join(rg.server.storage, collidingSource), body, 0o600); err != nil {
		return fmt.Errorf("put the source in the storage: %w", err)
	}

	// Each send gets its own budget. They are two transfers and the second one
	// is the measurement, so it does not inherit whatever the first spent.
	sending, cancel := context.WithTimeout(t.Context(), transferBudget)
	defer cancel()

	// The first name is uncontested, so it is the control: a fixed readback that
	// fails this is a readback that fails everything.
	status, probe, fault, err := rg.server.sendFile(sending, view.ID, collidingSource, collidingProbe)
	if err != nil {
		return fmt.Errorf("send %s: %w", collidingProbe, err)
	}

	if status != http.StatusOK || probe.State != "done" {
		watchTheStick(t, rg, view.ID)

		return fmt.Errorf("the uncontested %s answered %d and settled %q (%s), %s %v, so the "+
			"collision cannot be measured against it", collidingProbe, status, probe.State,
			probe.Reason, fault.Code, fault.Params)
	}

	t.Logf("observed: %s settled %q, warning %q, %d of %d bytes", collidingProbe, probe.State,
		probe.Warning, probe.BytesSent, probe.BytesTotal)

	between, err := rg.server.deviceFiles(t.Context(), view.ID)
	if err != nil {
		return fmt.Errorf("the machine would not say what is on its stick after the first transfer: %w", err)
	}

	t.Logf("observed: %s holds %s after the first transfer", printerStorage, describe(between))

	colliding, cancelColliding := context.WithTimeout(t.Context(), transferBudget)
	defer cancelColliding()

	second, control, fault, err := rg.server.sendFile(colliding, view.ID, collidingSource, collidingControl)
	if err != nil {
		return fmt.Errorf("send %s: %w", collidingControl, err)
	}

	after, err := rg.server.deviceFiles(t.Context(), view.ID)
	if err != nil {
		return fmt.Errorf("the machine would not say what is on its stick after the colliding "+
			"transfer, which is the reading the whole scenario rests on: %w", err)
	}

	t.Logf("observed: sending the colliding %s answered %d, transfer %q, warning %q, fault %s %v",
		collidingControl, second, control.State, control.Warning, fault.Code, fault.Params)
	t.Logf("observed: %s holds %s after the colliding transfer", printerStorage, describe(after))

	faults := judgeTheCollision(t, second, control, fault, after, int64(len(body)))

	// The first file is required to be there in every branch: a collision that
	// took the survivor with it is a worse bug than the one being measured.
	if _, listed := entryFor(after, collidingProbe); !listed {
		faults = append(faults, fmt.Errorf("the uncontested %s was answered done and is not in the "+
			"listing afterwards: %s", collidingProbe, describe(after)))
	}

	// The live run found the survivor undeletable, permanently, because FatFs's
	// lock table keys an open object by directory entry slot rather than by name
	// (ff.c:927-931). Whether that reproduces here is recorded rather than
	// asserted: it is the firmware's behaviour and no fix of this project's
	// changes it.
	deleted, refusal, err := rg.server.deleteFile(t.Context(), view.ID, collidingProbe)
	if err != nil {
		t.Logf("observed: deleting the survivor could not be asked: %v", err)
	} else {
		t.Logf("observed: DELETE %s answered %d, %s %q", collidingProbe, deleted, refusal.Code,
			refusal.Detail)
	}

	return errors.Join(faults...)
}

// judgeTheCollision is the scenario's assertion: whatever the caller was told
// about the colliding transfer, the printer's own listing has to agree with it.
// That agreement is README item 3, and it is the whole of what the host half of
// BUG-014's fix owes.
func judgeTheCollision(
	t *testing.T, status int, control transferRow, fault faultDoc, after []deviceFile, size int64,
) []error {
	t.Helper()

	var faults []error

	switch {
	case status == http.StatusConflict:
		if fault.Code != transferNotOnStorage {
			faults = append(faults, fmt.Errorf("the colliding send was refused with %q, want %q",
				fault.Code, transferNotOnStorage))
		}

		if _, listed := entryFor(after, collidingControl); listed {
			faults = append(faults, fmt.Errorf("the server refused %s as not on the storage and the "+
				"printer lists it: %s", collidingControl, describe(after)))
		}

		t.Logf("verdict: the real FatFs behaved as the live CORE One L did. The second long name "+
			"sharing BUG014~1.GCO did not land under its own name, and the fixed readback refused "+
			"the transfer with %s rather than answering done.", fault.Code)
	case status == http.StatusOK && control.State == "done" && control.Warning != "":
		t.Logf("verdict: the readback could not conclude (%q), so the transfer kept done and carries "+
			"the warning. This is the inconclusive branch of the fix, not a confirmation of the "+
			"landing.", control.Warning)
	case status == http.StatusOK && control.State == "done":
		// The board disambiguated. Then the claim is the strong one: both names
		// are listed, each at the length that was sent.
		faults = append(faults, landed(after, collidingProbe, size)...)
		faults = append(faults, landed(after, collidingControl, size)...)

		t.Logf("verdict: this firmware disambiguated the short name and landed both files, so the "+
			"live CORE One L behaviour differs from the MK4 under MINI404. The readback confirmed "+
			"both names at %d bytes.", size)
	default:
		faults = append(faults, fmt.Errorf("the colliding send answered %d with transfer %q (%s), "+
			"which is neither the fault nor a finished transfer", status, control.State, control.Reason))
	}

	return faults
}

// landed is what "the file is on the printer" means: the name is in the
// listing, at the length that was sent, and not read-only. The firmware lists a
// payload still inside its staging directory read-only at full size
// (render.cpp, DirRenderer), so the flag is part of the reading.
func landed(after []deviceFile, path string, size int64) []error {
	entry, listed := entryFor(after, path)
	if !listed {
		return []error{fmt.Errorf("%s was answered done and is not in the listing: %s",
			path, describe(after))}
	}

	var faults []error

	if entry.Size != size {
		faults = append(faults, fmt.Errorf("%s is listed at %d bytes and %d were sent",
			path, entry.Size, size))
	}

	if entry.ReadOnly {
		faults = append(faults, fmt.Errorf("%s is listed read-only, which is how the firmware lists "+
			"a payload that is still inside its staging directory", path))
	}

	return faults
}

// watchTheStick logs what the printer says its storage holds, repeatedly, for
// long enough that a board still finishing a landing has finished it.
//
// It is a diagnostic and it asserts nothing. A readback that judged too early
// and a file that genuinely never landed produce the same answer at one moment
// and different answers over half a minute, and a failure that cannot say which
// is a failure nobody can act on.
func watchTheStick(t *testing.T, rg *rig, id string) {
	t.Helper()

	const (
		readings = 6
		apart    = 5 * time.Second
	)

	for reading := range readings {
		entries, err := rg.server.deviceFiles(t.Context(), id)
		if err != nil {
			t.Logf("stick reading %d: %v", reading, err)
		} else {
			t.Logf("stick reading %d, %s after the transfer: %s", reading,
				time.Duration(reading)*apart, describe(entries))
		}

		time.Sleep(apart)
	}
}
