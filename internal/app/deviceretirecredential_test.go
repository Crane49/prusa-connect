package app

// A removed printer loses the credential its connector issued it, so it cannot
// dial in again as a new device without anybody deciding.

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// forgetCall is one ForgetDevice the host made.
type forgetCall struct {
	identity connectorapi.Identity
	before   time.Time
}

// recordingForgetter is a connector that remembers what it was asked to forget,
// and answers with fail while that is set.
type recordingForgetter struct {
	mu    sync.Mutex
	calls []forgetCall
	fail  error
}

func (r *recordingForgetter) ForgetDevice(_ context.Context, identity connectorapi.Identity, before time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fail != nil {
		return r.fail
	}

	r.calls = append(r.calls, forgetCall{identity: identity, before: before})

	return nil
}

func (r *recordingForgetter) taken() []forgetCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]forgetCall(nil), r.calls...)
}

// arriveThrough admits one printer through a named connector with a unit id,
// which is what a connector that issues credentials reports.
func arriveThrough(t *testing.T, plane *devices, connector string, identity connectorapi.Identity) string {
	t.Helper()

	host := plane.Arrivals(connectorapi.Ref(connector), nil,
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := &countedSession{identity: identity, patches: make(chan connectorapi.StatePatch)}

	_, err := host.Admit(context.WithoutCancel(t.Context()), identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(identity)
}

// managing gives the manager role on one device, which is what Retire takes.
func managing(t *testing.T, graph *authz.Store, id string) {
	t.Helper()

	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
}

// Retiring a device asks the connector that admitted it, and no other, to
// forget it. A second retirement asks again as of the first one, so a removal
// that failed halfway can be finished.
func TestRetiringADeviceAsksItsConnectorToForgetIt(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	holder, other := &recordingForgetter{}, &recordingForgetter{}
	plane.useForgetter("xbuddy", holder)
	plane.useForgetter("other", other)

	identity := connectorapi.Identity{Vendor: "Prusa", Model: "XL", Serial: "SN-XL", UnitUID: "FP-XL"}
	id := arriveThrough(t, plane, "xbuddy", identity)
	managing(t, graph, id)

	require.NoError(t, plane.Retire(t.Context(), manager, id))

	calls := holder.taken()
	require.Len(t, calls, 1, "the connector holding the printer was not asked to forget it")
	require.Equal(t, "FP-XL", calls[0].identity.UnitUID)
	require.Empty(t, other.taken(), "a connector that never held the printer was asked to forget it")

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.NotNil(t, device.Retired)
	require.True(t, calls[0].before.Equal(device.Retired.At),
		"the credential was withdrawn as of %s and the record retired at %s", calls[0].before, device.Retired.At)

	require.NoError(t, plane.Retire(t.Context(), manager, id))

	calls = holder.taken()
	require.Len(t, calls, 2, "retiring again did not ask the connector again")
	require.True(t, calls[1].before.Equal(calls[0].before),
		"the second retirement withdrew as of a later moment than the first")
}

// A connector that cannot forget the printer fails the removal, and the device
// stays on the listing. A device retired with a live credential is the defect
// this exists for, and the operator learns of the failure and repeats it.
func TestARemovalWhoseConnectorCannotForgetLeavesTheDeviceListed(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	holder := &recordingForgetter{fail: errors.New("the store is gone")}
	plane.useForgetter("xbuddy", holder)

	id := arriveThrough(t, plane, "xbuddy", connectorapi.Identity{Vendor: "Prusa", Model: "XL", UnitUID: "FP-FAIL"})
	managing(t, graph, id)

	require.Error(t, plane.Retire(t.Context(), manager, id))

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.Nil(t, device.Retired, "the device was retired although its credential is still live")

	holder.mu.Lock()
	holder.fail = nil
	holder.mu.Unlock()

	require.NoError(t, plane.Retire(t.Context(), manager, id))
	require.Len(t, holder.taken(), 1)
}

// At start, a connector is asked to forget every printer an operator removed,
// as of the moment it was removed. That covers a removal made by a build that
// did not take the credential back. A device that changed hands is not
// forgotten, because its registration is the new owner's, and neither is a
// machine the fleet holds again under a newer record.
func TestAStartAsksTheConnectorToForgetEveryRemovedPrinter(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	printer := func(unitUID string) connectorapi.Identity {
		return connectorapi.Identity{Vendor: "Prusa", Model: "XL", UnitUID: unitUID}
	}

	removed := arriveThrough(t, plane, "xbuddy", printer("FP-REMOVED"))
	transferred := arriveThrough(t, plane, "xbuddy", printer("FP-SOLD"))
	elsewhere := arriveThrough(t, plane, "other", printer("FP-ELSEWHERE"))
	kept := arriveThrough(t, plane, "xbuddy", printer("FP-KEPT"))
	returned := arriveThrough(t, plane, "xbuddy", printer("FP-BACK"))

	for _, id := range []string{removed, elsewhere, kept, returned} {
		managing(t, graph, id)
	}

	// The arrivals reach the store first. A record taken at an arrival and
	// flushed after a retirement writes the row back without it.
	plane.flush()

	// No connector is listening yet, which is a removal made by a build that
	// did not take credentials back.
	require.NoError(t, plane.Retire(t.Context(), manager, removed))
	require.NoError(t, plane.Retire(t.Context(), manager, elsewhere))
	require.NoError(t, plane.retireIdentity(t.Context(), transferred, retirementOwnerChanged, time.Now()))

	// A machine removed and then held again under a new record, as the defect
	// left the printer it was found on. Its registration is the new record's.
	require.NoError(t, plane.Retire(t.Context(), manager, returned))
	require.NotEqual(t, returned, arriveThrough(t, plane, "xbuddy", printer("FP-BACK")))

	// The start reads the stored fleet, which the last process flushed.
	plane.flush()

	holder := &recordingForgetter{}
	plane.withdrawRetiredCredentials(t.Context(), "xbuddy", holder)

	calls := holder.taken()
	require.Len(t, calls, 1, "the start asked about %+v", calls)
	require.Equal(t, "FP-REMOVED", calls[0].identity.UnitUID)

	device, err := plane.Get(t.Context(), manager, removed)
	require.NoError(t, err)
	require.False(t, calls[0].before.After(device.Retired.At),
		"the start withdrew as of %s, after the removal at %s", calls[0].before, device.Retired.At)
}

// The defect, through the whole server: a printer approved in mode token is
// removed, and dials /p/ws again with the token it was issued. It is refused
// with 401, before and after a restart, and no new device appears. It gets back
// in by registering again and being approved again.
func TestARemovedPrinterIsRefusedOnItsOldToken(t *testing.T) {
	t.Parallel()

	const fingerprint = "RMVD69LUJ3KIRTJFNK6FFU1AON1SGF4R2TFMBFEPUVVMOF4TDO"

	dir := t.TempDir()
	running := start(t, dir, decidedAdmissionConfigAs(freeAddr(t), "operator"))

	code := beginRegistration(t, running, fingerprint)

	approved := running.call(t, http.MethodPost, "/api/v1/registrations/"+code+"/approve", "")
	require.Equal(t, http.StatusOK, approved.status, approved.body)

	token := collectToken(t, running, code)

	socket := dialWithFingerprint(t, running, fingerprint, token)
	joined := waitForUnitUID(t, running, fingerprint)

	removed := running.call(t, http.MethodDelete, "/api/v1/devices/"+joined.ID, "")
	require.Equal(t, http.StatusNoContent, removed.status, removed.body)

	_ = socket.Close()

	require.Equal(t, http.StatusUnauthorized, dialStatus(t, running, fingerprint, token),
		"the removed printer was let back in on the token it was issued")
	require.Empty(t, printers(t, running), "the removed printer is listed again")

	running.stopAndWait(t)

	running = start(t, dir, decidedAdmissionConfigAs(freeAddr(t), "operator"))

	require.Equal(t, http.StatusUnauthorized, dialStatus(t, running, fingerprint, token),
		"the next process let the removed printer back in on its old token")
	require.Empty(t, printers(t, running), "the removed printer is listed again after a restart")

	// Add Printer to Connect, and a person's decision.
	again := beginRegistration(t, running, fingerprint)
	require.NotEqual(t, code, again, "the removed printer was handed the code it had before")

	approved = running.call(t, http.MethodPost, "/api/v1/registrations/"+again+"/approve", "")
	require.Equal(t, http.StatusOK, approved.status, approved.body)

	back := dialWithFingerprint(t, running, fingerprint, collectToken(t, running, again))
	t.Cleanup(func() { _ = back.Close() })

	rejoined := waitForUnitUID(t, running, fingerprint)
	require.NotEqual(t, joined.ID, rejoined.ID, "the printer came back on the record that was removed")
}

// dialStatus opens /p/ws as a printer would and returns the status the server
// answered, closing a connection it did let through.
func dialStatus(t *testing.T, running *process, fingerprint, token string) int {
	t.Helper()

	header := http.Header{}
	header.Set("Fingerprint", fingerprint)
	header.Set("Token", token)
	header.Set("User-Agent-Printer", "XL")
	header.Set("User-Agent-Version", firmwareInsideTheProfile)

	dialer := websocket.Dialer{Subprotocols: []string{"prusa-connect"}, HandshakeTimeout: 10 * time.Second}

	conn, response, err := dialer.DialContext(t.Context(), "ws://"+running.addr+"/p/ws", header)
	if conn != nil {
		_ = conn.Close()
	}

	require.NotNil(t, response, "the dial got no answer: %v", err)

	if response.Body != nil {
		_ = response.Body.Close()
	}

	return response.StatusCode
}

// In mode none the server checks no credential, so a removed printer joins
// again by itself as a new device. It keeps what it said when it registered:
// its firmware holds a token and does not register a second time, so a
// registration forgotten here would bring it back without its serial.
func TestARemovedPrinterInModeNoneJoinsAgainWithItsSerial(t *testing.T) {
	t.Parallel()

	const fingerprint = "OPEN69LUJ3KIRTJFNK6FFU1AON1SGF4R2TFMBFEPUVVMOF4TDO"

	running := start(t, t.TempDir(), openAdmissionConfig("operator"))

	token := collectToken(t, running, beginRegistration(t, running, fingerprint))

	socket := dialWithFingerprint(t, running, fingerprint, token)
	joined := waitForUnitUID(t, running, fingerprint)
	require.NotEmpty(t, joined.Serial)

	removed := running.call(t, http.MethodDelete, "/api/v1/devices/"+joined.ID, "")
	require.Equal(t, http.StatusNoContent, removed.status, removed.body)

	_ = socket.Close()

	back := dialWithFingerprint(t, running, fingerprint, token)
	t.Cleanup(func() { _ = back.Close() })

	rejoined := waitForUnitUID(t, running, fingerprint)
	require.NotEqual(t, joined.ID, rejoined.ID, "the printer came back on the record that was removed")
	require.Equal(t, joined.Serial, rejoined.Serial, "the printer came back without the serial it registered")
}
