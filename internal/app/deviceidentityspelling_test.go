package app

// The two spellings of one xBuddy fingerprint, driven through the whole server,
// and who may take a printer off the device that holds it.
//
// Buddy computes fifty base32 characters and sends all of them in the body of
// POST /p/register, then puts the first sixteen in the Fingerprint header of
// every request including the websocket upgrade. A machine met by one spelling
// and registered under the other was two devices on the bench on 2026-09-03,
// and the old row was neither the machine nor retired (BUG-011).

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The fingerprint of the machine these cases are about, in the two spellings
// its firmware puts on the wire. Fifty characters is what printerHash emits,
// and sixteen is FINGERPRINT_HDR_SIZE.
const (
	fullSpelling   = "C2UV69LUJ3KIRTJFNK6FFU1AON1SGF4R2TFMBFEPUVVMOF4TDO"
	headerSpelling = "C2UV69LUJ3KIRTJF"
)

// openAdmissionConfig is the shipping core: mode none, which admits any printer
// that dials and has no registration for anybody to approve.
func openAdmissionConfig(principal string) string {
	return lines(
		"core:",
		"  log:",
		"    level: error",
		`  principal: "`+principal+`"`,
	)
}

// TestAPrinterMetInModeNoneKeepsItsIDWhenItRegisters is BUG-011.
//
// The site ran in mode none, so the printer was admitted with no registration
// and under the only spelling it had put on the wire: the sixteen characters of
// the Fingerprint header. The site then moved to mode token and the owner
// pressed "Add printer to Connect", which posts all fifty characters.
//
// One machine, so one device. What puts the two spellings together is the
// printer's next handshake and not the approval: it dials with the sixteen
// characters and the token that registration issued, so the machine holding the
// credential is what says the two strings are one, and the id it arrives under
// is the id it already had. Nothing is retired, because nothing changed hands,
// and a second row would be a printer that never connects again beside a device
// with no history.
//
// The approval deliberately merges nothing. Sixteen characters of a fingerprint
// are in the clear in every request a printer makes, so a registration that
// shares them has proved nothing by sharing them; that is the review round
// finding this case was rewritten for, and
// TestAStrangerSharingSixteenCharactersOfAFingerprintIsItsOwnPrinter is the
// other direction of it.
func TestAPrinterMetInModeNoneKeepsItsIDWhenItRegisters(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, openAdmissionConfig("operator"))

	// First sight, over the frozen wire, spelled the way the header spells it.
	// The simulator sends one spelling in both places, which is the one thing
	// this case must not have.
	socket := dialWithFingerprint(t, running, headerSpelling, "any-token-mode-none-takes")

	met := waitForUnitUID(t, running, headerSpelling)

	t.Logf("observed: mode none admitted the printer as %s under unit_uid %q", met.ID, met.UnitUID)

	require.NoError(t, socket.Close())

	// The site moves to mode token. The binding comes back from the record
	// store, still under the header spelling, because that is all this fleet
	// has ever been told.
	running.stopAndWait(t)

	running = start(t, dir, decidedAdmissionConfigAs(freeAddr(t), "operator"))

	restored := printers(t, running)
	require.Len(t, restored, 1, "the restart did not read the printer back, so there is nothing to re-bind")
	require.Equal(t, met.ID, restored[0].ID, "the restart gave the printer a different id")

	// "Add printer to Connect": the whole fingerprint, in the body, and an
	// operator's approval.
	code := beginRegistration(t, running, fullSpelling)

	approved := running.call(t, http.MethodPost, "/api/v1/registrations/"+code+"/approve", "")
	require.Equal(t, http.StatusOK, approved.status, approved.body)

	rows := printers(t, running)

	for _, row := range rows {
		t.Logf("observed: after the approval the listing holds %s unit_uid=%q retired=%v",
			row.ID, row.UnitUID, row.Retired)
	}

	// The approval by itself touches nothing the machine has. It minted an id
	// for the fifty character spelling, which no machine has arrived under, so
	// the fleet is still the one device it was.
	require.Len(t, rows, 1,
		"the approval minted a second device for a machine this fleet already had, "+
			"so one printer is two rows and one of them will never connect again")
	require.Equal(t, met.ID, rows[0].ID,
		"the machine came back under a new id although nothing changed hands, so its history, "+
			"its grants and its address are somebody else's")
	require.Nil(t, rows[0].Retired,
		"the device the machine still is reads as retired")

	// The handshake is what merges the two spellings. The printer dials with
	// its sixteen and the token the registration issued, which is the frozen
	// wire's own asymmetry, and it must arrive on the id it already had rather
	// than on the one the approval minted.
	token := collectToken(t, running, code)

	back := dialWithFingerprint(t, running, headerSpelling, token)
	t.Cleanup(func() { _ = back.Close() })

	arrived := waitForUnitUID(t, running, fullSpelling)

	t.Logf("observed: the registered printer dialled with its header spelling and arrived as %s", arrived.ID)

	require.Equal(t, met.ID, arrived.ID,
		"the registered printer arrived under a different id than the one it was admitted as")
	require.Len(t, printers(t, running), 1, "the printer dialling produced a second row")
}

// TestAStrangerSharingSixteenCharactersOfAFingerprintIsItsOwnPrinter is the
// review round of BUG-011, and it is a security case rather than an identity
// one.
//
// A printer puts the leading sixteen characters of its fingerprint in the clear
// in the Fingerprint header of every request it makes. So anything that has
// seen one request can post POST /p/register with a fifty character string that
// starts with those sixteen, and nothing about that string is evidence of
// anything: the other thirty-four are the caller's own invention.
//
// The first fix bound a device id from that. The registration reported the
// header spelling as an alias whenever it was the entry that owned the prefix
// index, which a printer met in admission mode none never is because it has no
// registration at all, and the approval resolved the machine through the alias.
// One operator approving a row whose serial and model the caller chose was
// therefore enough to bind the caller's fingerprint and its issued token to the
// working printer's device id and to make the approver that printer's owner.
//
// Nothing an approval reads is a prefix any more. The stranger is approved onto
// a device of its own, and the printer keeps its id, its ownership and its
// token binding.
func TestAStrangerSharingSixteenCharactersOfAFingerprintIsItsOwnPrinter(t *testing.T) {
	t.Parallel()

	// The same first sixteen characters, which is all a listener on one request
	// ever sees, and a tail the stranger made up.
	stranger := headerSpelling + strings.Repeat("Z", len(fullSpelling)-len(headerSpelling))

	dir := t.TempDir()
	running := start(t, dir, openAdmissionConfig("operator"))

	// The victim: a printer this fleet met in mode none, so it is known by the
	// sixteen characters of its header and nothing owns its prefix.
	socket := dialWithFingerprint(t, running, headerSpelling, "any-token-mode-none-takes")

	victim := waitForUnitUID(t, running, headerSpelling)

	t.Logf("observed: mode none admitted the printer as %s under unit_uid %q", victim.ID, victim.UnitUID)

	require.NoError(t, socket.Close())

	running.stopAndWait(t)

	running = start(t, dir, decidedAdmissionConfigAs(freeAddr(t), "operator"))

	restored := printers(t, running)
	require.Len(t, restored, 1, "the restart did not read the printer back, so there is nothing to take")

	// The open server made the printer its principal's when it arrived, and
	// that principal is also who approves below. The ownership is withdrawn,
	// so an approval that hands the printer to its approver still shows.
	require.Contains(t, subjectsGranted(t, running, victim.ID), "user:operator",
		"the open server did not make the printer nobody approved its principal's")

	withdrawn := running.call(t, http.MethodDelete,
		"/api/v1/grants/device/"+victim.ID+"?relation=owner&subject=user:operator", "")
	require.Equal(t, http.StatusNoContent, withdrawn.status, withdrawn.body)

	// The stranger registers. It never held the printer's fingerprint and never
	// held a credential of the printer's; it read sixteen characters off the
	// wire and wrote the rest.
	code := beginRegistration(t, running, stranger)

	// And an operator approves the row, which is the whole of the gesture the
	// attack needed.
	approved := running.call(t, http.MethodPost, "/api/v1/registrations/"+code+"/approve", "")
	require.Equal(t, http.StatusOK, approved.status, approved.body)

	rows := printers(t, running)

	for _, row := range rows {
		t.Logf("observed: after the stranger's approval the listing holds %s unit_uid=%q retired=%v",
			row.ID, row.UnitUID, row.Retired)
	}

	require.Len(t, rows, 1,
		"the stranger's approval added or removed a device, and it has no machine to add one for")
	require.Equal(t, victim.ID, rows[0].ID, "the printer's device id changed hands on a shared prefix")
	require.Equal(t, headerSpelling, rows[0].UnitUID,
		"the printer's row now names the stranger's spelling, so the stranger's token resolves to it")
	require.Nil(t, rows[0].Retired,
		"the printer was retired by a registration that shares sixteen characters with it")

	require.NotContains(t, subjectsGranted(t, running, victim.ID), "user:operator",
		"approving the stranger's registration made the approver the owner of somebody else's printer")

	// The stranger's own credential admits the stranger, under an id of its
	// own. It is a machine this server has never met, so it is a first
	// admission and not a transfer, and the printer's device is untouched by it.
	token := collectToken(t, running, code)

	dialled := dialWithFingerprint(t, running, stranger, token)
	t.Cleanup(func() { _ = dialled.Close() })

	arrived := waitForUnitUID(t, running, stranger)

	t.Logf("observed: the stranger arrived as %s, the printer is %s", arrived.ID, victim.ID)

	require.NotEqual(t, victim.ID, arrived.ID,
		"the stranger arrived on the printer's device id, so its token is bound to that machine")

	victimRow := deviceByID(t, running, victim.ID)
	require.NotNil(t, victimRow, "the printer's device is gone from the listing")
	require.Nil(t, victimRow.Retired, "the printer's device was retired by a stranger dialling")
	require.Equal(t, headerSpelling, victimRow.UnitUID,
		"the printer's device changed the spelling it answers to")
}

// deviceByID is one row of the listing, or nil when the fleet holds no device
// under that id.
func deviceByID(t *testing.T, running *process, id string) *httpapi.Device {
	t.Helper()

	for _, row := range printers(t, running) {
		if row.ID == id {
			return &row
		}
	}

	return nil
}

// TestASecondApprovalByThePersonWhoOwnsThePrinterKeepsItsID is the other half
// of the transfer rule.
//
// The same person approving their own machine a second time is not an ownership
// transfer. It is a printer that was factory reset, or one whose registration
// its owner rejected and made again, and retiring the device would throw that
// printer's history away for an operator repeating themselves.
func TestASecondApprovalByThePersonWhoOwnsThePrinterKeepsItsID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, decidedAdmissionConfigAs(freeAddr(t), "alpha"))

	const serial = "SIM-IDENTITY-REAPPROVED"

	first := registerApproveAndRun(t, running, serial)

	t.Logf("observed: the printer was approved and arrived as %s", first.ID)

	require.Contains(t, subjectsGranted(t, running, first.ID), "user:alpha",
		"the approval did not make the machine the approver's, so this case compares nothing")

	// The machine goes off the wire, because one printer holds one session and
	// this case is about the identity rather than about the socket.
	first.Stop()

	// The same operator rejects the registration and approves the machine
	// again, which is the same two requests a transfer makes.
	rejected := running.call(t, http.MethodDelete, "/api/v1/registrations/"+first.Code, "")
	require.Equal(t, http.StatusNoContent, rejected.status, rejected.body)

	runPrinter(t, running, serial)

	approveCode(t, running, waitForPendingCode(t, running))

	require.Eventually(t, func() bool {
		row := listedBySerial(t, running, serial)

		return row != nil && row.Connected
	}, settle, 25*time.Millisecond, "the re-approved printer never came back")

	rows := printers(t, running)

	for _, row := range rows {
		t.Logf("observed: after the second approval the listing holds %s connected=%v retired=%v",
			row.ID, row.Connected, row.Retired)
	}

	require.Len(t, rows, 1, "re-approving one machine left two rows")
	require.Equal(t, first.ID, rows[0].ID,
		"the owner re-approving their own printer minted a second id for it")
	require.Nil(t, rows[0].Retired,
		"the owner re-approving their own printer retired it, so its history is nobody's")
}

// dialWithFingerprint opens the frozen device wire by hand, as a printer that
// spells its fingerprint the way the header does.
//
// It is written here rather than taken from the simulator because the simulator
// speaks the dialect correctly and sends one spelling in both places. The
// asymmetry is the whole of these cases.
func dialWithFingerprint(t *testing.T, running *process, fingerprint, token string) *websocket.Conn {
	t.Helper()

	header := http.Header{}
	header.Set("Fingerprint", fingerprint)
	header.Set("Token", token)
	header.Set("User-Agent-Printer", "MK4")
	header.Set("User-Agent-Version", firmwareInsideTheProfile)

	dialer := websocket.Dialer{
		Subprotocols:     []string{"prusa-connect"},
		HandshakeTimeout: 10 * time.Second,
	}

	conn, response, err := dialer.DialContext(t.Context(), "ws://"+running.addr+"/p/ws", header)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	require.NoError(t, err, "the printer could not get onto the wire")

	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

// beginRegistration is POST /p/register with the whole fingerprint, which is
// what the firmware sends when somebody presses "Add printer to Connect". It
// returns the code an operator reads off the screen.
func beginRegistration(t *testing.T, running *process, fingerprint string) string {
	t.Helper()

	body := `{"sn":"18070-5042441661804658","fingerprint":"` + fingerprint +
		`","printer_type":"7.1.0","firmware":"6.8.1"}`

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+running.addr+"/p/register", strings.NewReader(body))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/json")

	answered, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = answered.Body.Close() }()

	require.Equal(t, http.StatusOK, answered.StatusCode, "POST /p/register was refused")

	code := answered.Header.Get("Code")
	require.NotEmpty(t, code, "POST /p/register answered no code")

	return code
}

// collectToken is GET /p/register, which is the poll a printer makes until
// somebody has decided. It returns the credential the printer then dials with.
func collectToken(t *testing.T, running *process, code string) string {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+running.addr+"/p/register", nil)
	require.NoError(t, err)

	request.Header.Set("Code", code)

	answered, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = answered.Body.Close() }()

	require.Equal(t, http.StatusOK, answered.StatusCode, "the approved printer's poll was refused")

	token := answered.Header.Get("Token")
	require.NotEmpty(t, token, "an approved registration answered no token")

	return token
}

// waitForUnitUID waits until the listing holds a device the connector reported
// under one unit id, and returns its row.
func waitForUnitUID(t *testing.T, running *process, unitUID string) httpapi.Device {
	t.Helper()

	var found httpapi.Device

	require.Eventually(t, func() bool {
		for _, row := range printers(t, running) {
			if row.UnitUID == unitUID {
				found = row

				return true
			}
		}

		return false
	}, settle, 25*time.Millisecond, "no device was listed under unit_uid "+unitUID)

	return found
}
