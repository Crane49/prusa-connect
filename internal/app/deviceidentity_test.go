package app

// An earlier design decision, driven from outside: what a printer's id is, what keeps it, and
// what replaces it.
//
// Every assertion reads the id out of GET /api/v1/devices rather than computing
// one. There is nothing to compute any more, which is the change these cases
// are about: the id is minted and bound, so the only way to know a machine's id
// is to ask the server what it bound.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// decidedAdmissionConfig is a core whose xBuddy driver takes a token and
// approves nothing on its own, which is the only mode with a registration for
// an operator to approve and therefore the only mode a transfer exists in.
func decidedAdmissionConfig(addr string) string {
	return decidedAdmissionConfigAs(addr, "operator")
}

// decidedAdmissionConfigAs is the same core, serving every request as one named
// subject.
//
// Who approves is what decides whether a second approval keeps the printer's id
// or takes the machine off the device that holds it
// (approvalKeepsTheIdentity), and this deployment authenticates nobody, so the
// only way to be somebody else is to be a second process. That is what a
// transfer case does: the person who owns the printer, then the person it went
// to.
func decidedAdmissionConfigAs(addr, principal string) string {
	return lines(
		"core:",
		"  log:",
		"    level: error",
		`  principal: "`+principal+`"`,
		"  api:",
		`    addr: "`+addr+`"`,
		"  connectors:",
		"    xbuddy:",
		"      type: xbuddy",
		"      config:",
		"        mode: token",
		"        auto_register: false",
	)
}

// TestAPrinterThatReconnectsComesBackUnderTheSameID is the property the id
// carries for a client: a fleet screen does not gain a second row because a
// cable came out.
//
// The printer is stopped and started again against the same running core, so
// nothing in this case is about the record store. The binding is in memory and
// the second handshake resolves the same fingerprint to it.
func TestAPrinterThatReconnectsComesBackUnderTheSameID(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	const serial = "SIM-IDENTITY-RECONNECT"

	stop := runPrinter(t, running, serial)

	first := idInListing(t, running, serial)

	t.Logf("observed: the printer first arrived as %s", first)

	stop()

	// The same machine again, a new process's worth of state on the printer
	// side and a new socket on this one.
	runPrinter(t, running, serial)

	require.Eventually(t, func() bool {
		return listedBySerial(t, running, serial) != nil
	}, settle, 25*time.Millisecond, "the printer never came back")

	after := printers(t, running)

	require.Len(t, after, 1,
		"reconnecting produced a second row, so one machine is two devices")
	require.Equal(t, first, after[0].ID,
		"the printer came back under a different id, so its grants and its history are somebody else's")
}

// TestTwoPrintersNeverShareAnIDAndEveryIDIsAUUID is the other half of the same
// promise, and the shape of the string.
//
// The shape is asserted rather than assumed because the id has to be legal in
// four places at once: an authorization key, a URL segment, an event origin and
// a DNS-1123 label. A version 4 UUID in its canonical form is all four, and a
// mint that produced anything else would be found here rather than by a client.
func TestTwoPrintersNeverShareAnIDAndEveryIDIsAUUID(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialInFleet(t, running, "SIM-IDENTITY-ONE", "SIM-IDENTITY-TWO")

	listed := printers(t, running)
	require.Len(t, listed, 2)

	seen := map[string]string{}

	for _, one := range listed {
		t.Logf("observed: %s was listed as %s", one.Serial, one.ID)

		parsed, err := uuid.Parse(one.ID)
		require.NoErrorf(t, err, "the id of %s is not a UUID", one.Serial)
		require.Equalf(t, uuid.Version(4), parsed.Version(),
			"the id of %s is a UUID of version %d, and H11 mints version 4", one.Serial, parsed.Version())
		require.Equalf(t, one.ID, parsed.String(),
			"the id of %s is not the canonical lowercase form", one.Serial)

		require.NotContainsf(t, seen, one.ID,
			"%s and %s were given one id", one.Serial, seen[one.ID])

		seen[one.ID] = one.Serial
	}

	require.NotEqual(t, listed[0].UnitUID, listed[1].UnitUID,
		"the two printers presented one fingerprint, so this case proves nothing about two of them")
}

// TestTheAuthRouteAnswersTheIDTheListingReports is the frozen wire's half of
// the identity. GET /p/auth answers under the key uuid, and what a printer
// learns there is the string it is addressed by at
// GET /api/v1/devices/{device_id}.
//
// It is now a real UUID, which is what a client built around Prusa Connect
// parses. Nothing else on that route changed.
func TestTheAuthRouteAnswersTheIDTheListingReports(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	const serial = "SIM-IDENTITY-AUTH"

	runPrinter(t, running, serial)

	listed := waitForListing(t, running, serial)

	answered := askAuthRoute(t, running, listed.UnitUID)

	t.Logf("observed: GET /p/auth answered uuid %q and the listing reports %q",
		answered, listed.ID)

	_, err := uuid.Parse(answered)
	require.NoError(t, err, "GET /p/auth answered something that is not a UUID")

	require.Equal(t, listed.ID, answered,
		"the printer was told an id the API does not address it by")
}

// TestASecondApprovedRegistrationMintsANewIDAndRetiresTheOldOne is the case the
// whole decision was made for: a printer that changed hands.
//
// The machine is the same machine and presents the same fingerprint. What is
// different is the registration and who approved it, and the registration is
// the identity. So the approval mints a second id, and the first one is retired
// rather than reused, deleted or merged: every grant an operator wrote on it
// stays on it, its history stays readable under it, and none of it reaches the
// id the new owner's printer arrives under.
//
// The approval is not the owner's, which is what makes it a transfer rather than
// the same person re-approving their own machine (approvalKeepsTheIdentity).
// The machine belongs to the person who
// approved it first, and it is written over to somebody else here the way an
// operator hands a printer over: the previous owner's grant on it stays, and
// the person approving the new registration is not them.
func TestASecondApprovedRegistrationMintsANewIDAndRetiresTheOldOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	addr := freeAddr(t)
	running := start(t, dir, decidedAdmissionConfig(addr))

	const serial = "SIM-IDENTITY-TRANSFER"

	// The first owner. The printer registers, an operator approves it, and it
	// dials in. Approving it is what makes the machine theirs: the approval
	// writes owner on the device once the printer arrives.
	first := registerApproveAndRun(t, running, serial)

	t.Logf("observed: the first owner's printer arrived as %s", first.ID)

	require.Contains(t, subjectsGranted(t, running, first.ID), "user:operator",
		"approving a registration did not make the machine the approver's, so nothing can be "+
			"transferred away from anybody")

	// A grant on the old device, which is what must not travel. It is written
	// through the published surface, because that is where an operator writes
	// one and it is the same table the listing filter reads.
	const watcher = "user:previous-owner"

	handed := running.call(t, http.MethodPost, "/api/v1/grants/device/"+first.ID,
		`{"relation":"operator","subject":"`+watcher+`"}`)
	require.Equal(t, http.StatusCreated, handed.status, handed.body)

	// The machine is the previous owner's, and the person deciding the next
	// registration is not them. That is the whole of what tells a transfer from
	// one owner re-approving their own printer, and it is written through the
	// same surface an operator hands a machine over with.
	given := running.call(t, http.MethodPost, "/api/v1/grants/device/"+first.ID,
		`{"relation":"owner","subject":"`+watcher+`"}`)
	require.Equal(t, http.StatusCreated, given.status, given.body)

	withdrawn := running.call(t, http.MethodDelete,
		"/api/v1/grants/device/"+first.ID+"?relation=owner&subject=user:operator", "")
	require.Equal(t, http.StatusNoContent, withdrawn.status, withdrawn.body)

	require.NotContains(t, subjectsGranted(t, running, first.ID), "user:operator",
		"the person about to approve still owns the machine, so this case is a re-bind")

	// The transfer. The old registration is rejected, which is what an operator
	// does to a machine that has left, and the printer registers again and is
	// approved for its new owner.
	rejected := running.call(t, http.MethodDelete, "/api/v1/registrations/"+first.Code, "")
	require.Equal(t, http.StatusNoContent, rejected.status, rejected.body)

	first.Stop()

	owner := registerApproveAndRun(t, running, serial)

	t.Logf("observed: the second owner's printer arrived as %s", owner.ID)

	require.NotEqual(t, first.ID, owner.ID,
		"the machine came back under the previous owner's id, so its history is now the new owner's")

	// The new owner's machine is the fleet, and the previous owner's row is one
	// the listing leaves out until somebody asks for it. Nothing was deleted,
	// which is what makes those records still readable.
	require.Equal(t, []string{owner.ID}, idsOf(printers(t, running)),
		"a retired device is on the fleet listing, which is what is here now")

	rows := map[string]httpapi.Device{}
	for _, one := range listedPrinters(t, running, "retired=include") {
		rows[one.ID] = one
	}

	require.Len(t, rows, 2, "the transfer left one row, so a record was merged or dropped")

	old, held := rows[first.ID]
	require.True(t, held, "the retired device is not in the listing")

	require.NotNil(t, old.Retired, "the old device reads as still naming its machine")
	require.Equal(t, "ownership_changed", old.Retired.Reason)
	require.False(t, old.Retired.At.IsZero(), "the retirement carries no moment")

	require.Nil(t, rows[owner.ID].Retired, "the new device was retired the moment it was minted")

	// The grant did not travel. Both objects are read back through the same
	// route, so what is compared is the tuple table and not a listing filter.
	require.Contains(t, subjectsGranted(t, running, first.ID), watcher,
		"the grant left the device it was written on")
	require.NotContains(t, subjectsGranted(t, running, owner.ID), watcher,
		"a grant written on the previous owner's device reaches the new owner's device")

	// And the old id still answers, which is what "not deleted" has to mean to
	// a client: the record is there to read, with its state and its history.
	single := running.get(t, "/api/v1/devices/"+first.ID)
	require.Equal(t, http.StatusOK, single.status,
		"the retired device's own route stopped answering, so its past is unreachable")

	jobs := running.get(t, "/api/v1/devices/"+first.ID+"/jobs")
	require.Equal(t, http.StatusOK, jobs.status,
		"the retired device's prints stopped being readable under the id they were filed under")

	// The retirement is a stored fact and not a flag this process holds. A
	// second core on the same directory has to read it back, because the whole
	// point of retiring rather than deleting is that the record outlives the
	// process that retired it.
	running.stopAndWait(t)

	second := start(t, dir, decidedAdmissionConfig(freeAddr(t)))

	restored := map[string]httpapi.Device{}
	for _, one := range listedPrinters(t, second, "retired=include") {
		restored[one.ID] = one
	}

	require.Len(t, restored, 2, "a restart forgot one of the two devices")
	require.NotNil(t, restored[first.ID].Retired,
		"a restart read the retired device back as one that still names its machine")
	require.Equal(t, "ownership_changed", restored[first.ID].Retired.Reason)
	require.Nil(t, restored[owner.ID].Retired,
		"a restart read the new owner's device back as retired")
}

// subjectsGranted is every subject holding a tuple on one device, read through
// the published grant surface.
func subjectsGranted(t *testing.T, running *process, device string) []string {
	t.Helper()

	read := running.get(t, "/api/v1/grants/device/"+device)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var listing struct {
		Items []struct {
			Subject string `json:"subject"`
		} `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &listing))

	subjects := make([]string, 0, len(listing.Items))
	for _, item := range listing.Items {
		subjects = append(subjects, item.Subject)
	}

	return subjects
}

// runPrinter starts one simulated printer against a running core and returns
// the function that stops it. Stopping is idempotent, so a case may stop it
// early and the cleanup still runs.
func runPrinter(t *testing.T, running *process, serial string) func() {
	t.Helper()

	printer, err := simulator.New(simulator.Config{
		Model:   "MK4",
		Serial:  serial,
		BaseURL: "http://" + running.addr,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(ctx) }()

	stopped := false
	stop := func() {
		if stopped {
			return
		}

		stopped = true

		cancel()
		<-done
	}

	t.Cleanup(stop)

	return stop
}

// listedBySerial is the row for one serial, or nil while the listing has none.
func listedBySerial(t *testing.T, running *process, serial string) *httpapi.Device {
	t.Helper()

	for _, one := range printers(t, running) {
		if one.Serial == serial {
			return &one
		}
	}

	return nil
}

// waitForListing waits until one serial is in the listing and returns its row.
func waitForListing(t *testing.T, running *process, serial string) httpapi.Device {
	t.Helper()

	var found httpapi.Device

	require.Eventually(t, func() bool {
		row := listedBySerial(t, running, serial)
		if row == nil {
			return false
		}

		found = *row

		return true
	}, settle, 25*time.Millisecond, "the printer never appeared in the listing")

	return found
}

// idInListing is waitForListing reduced to the id, for a case that only wants
// the string.
func idInListing(t *testing.T, running *process, serial string) string {
	t.Helper()

	return waitForListing(t, running, serial).ID
}

// askAuthRoute asks the frozen GET /p/auth what this server calls a printer.
//
// The driver runs in mode none here, which takes any fingerprint and any
// non-empty token, so the token is a placeholder: what this reads is the
// identity half of that route and not its credential half, which
// internal/connector/xbuddy already covers.
func askAuthRoute(t *testing.T, running *process, fingerprint string) string {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+running.addr+"/p/auth", nil)
	require.NoError(t, err)

	request.Header.Set("Fingerprint", fingerprint)
	request.Header.Set("Token", "any-token-mode-none-takes")

	answer, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = answer.Body.Close() }()

	var body map[string]string
	require.NoError(t, json.NewDecoder(answer.Body).Decode(&body))
	require.Equalf(t, http.StatusOK, answer.StatusCode, "GET /p/auth answered %v", body)

	return body["uuid"]
}

// approvedPrinter is one owner's printer: the registration an operator decided,
// and the device row it arrived as.
type approvedPrinter struct {
	httpapi.Device

	// Code is the registration the operator approved, which is what a transfer
	// rejects to end one ownership.
	Code string

	// Stop ends the printer's own process. It is idempotent and it also runs at
	// cleanup, so a case that has to have the machine off the wire before the
	// next thing it does calls it, and one that does not says nothing.
	Stop func()
}

// registerApproveAndRun walks one whole admission: the printer registers, the
// operator approves the code through the published API, and the printer dials
// in. It returns the registration and the row the machine landed in.
func registerApproveAndRun(t *testing.T, running *process, serial string) approvedPrinter {
	t.Helper()

	before := map[string]bool{}
	for _, one := range printers(t, running) {
		before[one.ID] = true
	}

	stop := runPrinter(t, running, serial)

	code := waitForPendingCode(t, running)

	approveCode(t, running, code)

	var row httpapi.Device

	require.Eventually(t, func() bool {
		for _, one := range printers(t, running) {
			if one.Serial == serial && !before[one.ID] {
				row = one

				return true
			}
		}

		return false
	}, settle, 25*time.Millisecond, "the approved printer never reached the listing")

	// The printer is left running by the caller of the first ownership and
	// stopped before the second registers, because one machine holds one
	// session and the case is about the identity rather than about the socket.
	t.Cleanup(stop)

	return approvedPrinter{Device: row, Code: code, Stop: stop}
}

// waitForPendingCode waits until one registration is waiting for a decision and
// returns the code an operator would read off the printer's screen.
func waitForPendingCode(t *testing.T, running *process) string {
	t.Helper()

	var code string

	require.Eventually(t, func() bool {
		read := running.get(t, "/api/v1/registrations")
		if read.status != http.StatusOK {
			return false
		}

		var listing struct {
			Items []httpapi.Registration `json:"items"`
		}

		require.NoError(t, json.Unmarshal([]byte(read.body), &listing))

		for _, row := range listing.Items {
			if !row.Approved {
				code = row.Code

				return true
			}
		}

		return false
	}, settle, 25*time.Millisecond, "no registration was waiting for a decision")

	return code
}

// approveCode is the operator's decision, through the published surface.
func approveCode(t *testing.T, running *process, code string) {
	t.Helper()

	approved := running.call(t, http.MethodPost, "/api/v1/registrations/"+code+"/approve", "")
	require.Equal(t, http.StatusOK, approved.status, approved.body)
}

// TestTheIdentityIndexBoundsWhatAnUnknownCallerCanMint is the limit read as the
// exposure it exists for.
//
// The shipping admission mode is none, which takes any fingerprint, and
// GET /p/auth mints an id for the fingerprint it is handed. So anything that can
// reach the printer port can make this process remember a binding, one request
// at a time, and without a bound that is a way to fill its memory.
//
// A binding this fleet has met is not part of that bound and is asserted here
// to survive the flood, because the whole limit would be worthless if a caller
// could evict a real printer's id with it.
func TestTheIdentityIndexBoundsWhatAnUnknownCallerCanMint(t *testing.T) {
	t.Parallel()

	index := newDeviceIdentities()

	held := index.identify(connectorapi.Identity{UnitUID: "FINGERPRINT-OF-A-REAL-PRINTER"})
	index.meet(held)

	// One more than the limit, so the eviction has run at least once.
	for at := range unclaimedIdentityLimit + 1 {
		index.identify(connectorapi.Identity{UnitUID: fmt.Sprintf("MADE-UP-FINGERPRINT-%06d", at)})
	}

	t.Logf("observed: after %d mints the index holds %d keys",
		unclaimedIdentityLimit+1, len(index.bound))

	require.LessOrEqual(t, len(index.minted), unclaimedIdentityLimit,
		"the index holds more unmet bindings than its limit, so nothing bounds it")

	// One key per bounded binding, plus the one this fleet has met.
	require.LessOrEqual(t, len(index.bound), unclaimedIdentityLimit+1,
		"the map grew past one key per bounded binding")

	resolved, bound := index.boundTo("FINGERPRINT-OF-A-REAL-PRINTER")
	require.True(t, bound, "a flood of unknown fingerprints evicted a printer this fleet had met")
	require.Equal(t, held, resolved, "the met printer's binding was replaced")

	// And the oldest of the made-up ones is gone, which is what made room.
	_, survived := index.boundTo("MADE-UP-FINGERPRINT-000000")
	require.False(t, survived, "nothing was evicted, so the limit is not enforced")
}

// TestATransferIsDecidedForADeviceTheDepartedTableHasDropped is the transfer
// test read against the fleet rather than against what this process happens to
// still hold.
//
// departedLimit bounds the state trees a stopped printer keeps at four hundred,
// and restore evicts while it loads. So on a site that has met more machines
// than that, the fingerprint of an early one is bound and its entry is gone. A
// transfer decided on the entry would call that a first admission and hand the
// new owner the previous owner's id, with its grants and its history.
//
// The fleet is built through restore, which is how a site larger than the limit
// exists at all.
func TestATransferIsDecidedForADeviceTheDepartedTableHasDropped(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: visibility{graph: graph}, Logger: discard()})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	plane := newPlaneOn(t, graph, hub)

	// One more device than the departed table holds, the first of them being
	// the one the transfer is about.
	const oldest = "device-0000"

	stored := make([]devicestore.DeviceRecord, 0, departedLimit+1)

	for at := range departedLimit + 1 {
		id := fmt.Sprintf("device-%04d", at)

		require.NoError(t, plane.register(t.Context(), id,
			connectorapi.Identity{Vendor: "Prusa", Model: "MK4", UnitUID: "FINGERPRINT-" + id}, ""))

		stored = append(stored, devicestore.DeviceRecord{
			DeviceID:  id,
			Driver:    "xbuddy",
			Vendor:    "Prusa",
			Model:     "MK4",
			UnitUID:   "FINGERPRINT-" + id,
			Departure: departureSessionEnded,
			LastSeen:  time.Now().UTC().Add(-time.Duration(departedLimit-at) * time.Minute),
		})
	}

	require.NoError(t, plane.records.SaveDeviceRecords(t.Context(), stored...))
	require.NoError(t, plane.restore(t.Context()))

	// The evidence that this is the case being tested: the fingerprint is bound
	// and the entry it was bound for is no longer held.
	_, live := plane.entry(oldest)
	require.False(t, live, "the oldest device is still held, so this case is not about a dropped entry")

	bound, held := plane.identities.boundTo("FINGERPRINT-" + oldest)
	require.True(t, held, "the oldest device's fingerprint lost its binding, so nothing can be transferred")
	require.Equal(t, oldest, bound)

	t.Logf("observed: %s is bound and its entry is gone; the departed table holds %d of %d rows",
		oldest, len(plane.departed), departedLimit+1)

	// Whose machine it is, because that is what a transfer is decided against.
	// It was approved onto this farm by one person and it is approved again by
	// another, which is the transfer (approvalKeepsTheIdentity).
	owner, err := authz.ParseSubject("user:alpha")
	require.NoError(t, err)

	_, err = graph.ClaimDevice(t.Context(), oldest, owner)
	require.NoError(t, err)

	require.NoError(t, plane.registrationApproved(t.Context(),
		admission.Registration{Fingerprint: "FINGERPRINT-" + oldest}, "user:beta"))

	minted, rebound := plane.identities.boundTo("FINGERPRINT-" + oldest)
	require.True(t, rebound, "the approval left the machine bound to nothing")

	t.Logf("observed: approving a new registration for it rebound the machine to %q", minted)

	require.NotEqual(t, oldest, minted,
		"the approval treated a machine this fleet has met as a first admission, "+
			"so the new owner arrives on the previous owner's id")

	_, err = uuid.Parse(minted)
	require.NoError(t, err, "the transfer minted something that is not a UUID")

	// The retirement is on the disk and not only in a table this process was
	// never holding. A device dropped from the departed table has no entry for
	// a flush to take, so the row is the only place it can be written.
	written := map[string]devicestore.DeviceRecord{}

	rows, err := plane.records.DeviceRecords(t.Context())
	require.NoError(t, err)

	for _, row := range rows {
		written[row.DeviceID] = row
	}

	require.Equal(t, retirementOwnerChanged, written[oldest].RetiredReason,
		"the stored row for the transferred-away device does not say it was retired")
	require.False(t, written[oldest].RetiredAt.IsZero(), "the stored retirement carries no moment")
}

// TestAFloodOfUnknownFingerprintsDoesNotRefuseAnAdmittedPrinter is the other
// half of the limit: what it may cost, and what it may not.
//
// Admit and Arrived are two calls, and the second looks the machine's
// fingerprint up again. If the binding could be evicted between them the
// printer would arrive under an id nothing admitted, and the host would refuse
// the session rather than merely rename it. So the flood has to be harmless to
// a printer whose Admit has landed, which is every printer in the window a
// handshake takes.
func TestAFloodOfUnknownFingerprintsDoesNotRefuseAnAdmittedPrinter(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	host := plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")

	identity := connectorapi.Identity{Vendor: "Prusa", Model: "MK4", UnitUID: "FINGERPRINT-ADMITTED"}

	_, err := host.Admit(t.Context(), identity)
	require.NoError(t, err)

	admitted := plane.idOf(identity)

	// More unknown fingerprints than the limit, which is what an unauthenticated
	// caller can drive through GET /p/auth one request at a time.
	for at := range unclaimedIdentityLimit + 1 {
		plane.identities.identify(connectorapi.Identity{
			UnitUID: fmt.Sprintf("FLOOD-FINGERPRINT-%06d", at),
		})
	}

	require.Equal(t, admitted, plane.idOf(identity),
		"the flood evicted an admitted printer's binding, so it arrives under an id nothing admitted")

	session := &countedSession{identity: identity, patches: make(chan connectorapi.StatePatch)}

	// The plane's pump ends when the driver closes the state channel, and this
	// session counts its closes rather than doing it, so the test is the driver.
	t.Cleanup(func() { close(session.patches) })

	require.NoError(t, host.Arrived(t.Context(), session),
		"the host refused an admitted printer's session after a flood of unknown fingerprints")

	_, live := plane.entry(admitted)
	require.True(t, live, "the printer arrived under a different id")
}
