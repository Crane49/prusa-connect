package xbuddy

import (
	"encoding/json"
	"net/http"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The frozen shape of an answer at authPath. A caller reads uuid on 200 and
// error on a refusal, and both names are what the retired server wrote
// (earlier research, the /p/auth row). The value under uuid is this host's device
// id, so the same string addresses the machine at
// GET /api/v1/devices/{device_id}.
const (
	authIDMember    = "uuid"
	authErrorMember = "error"
)

// Identities is the host half this route needs: the id the host addresses a
// device by, derived from the identity the handshake carried.
//
// The driver decides whether a credential is one it issued and the host decides
// what that device is called, so neither half can answer this path alone. It is
// an optional interface for the reason admits is one: a host that mints no id
// of its own is still a host, and it answers here with the refusal that says
// so. It is declared here because this is where it is consumed.
type Identities interface {
	// DeviceID is the id this host addresses the device by, minting one the
	// first time it meets a machine. A device that has never held a session
	// therefore answers with the id it will hold when it dials, and asking
	// twice answers the same string twice.
	DeviceID(identity connectorapi.Identity) string
}

// handleAuth answers GET /p/auth: the printer presents the credential it dials
// the websocket with, and learns the id this server knows it by.
//
// The two refusals are the retired server's own. A request missing either
// header is 401 before any table is read, so a caller probing for fingerprints
// cannot tell an unknown one from a malformed request; a credential this driver
// did not issue is 403.
func (d *Driver) handleAuth(w http.ResponseWriter, r *http.Request, arrivals connectorapi.Arrivals) {
	fingerprint := r.Header.Get(fingerprintHeader)
	token := r.Header.Get(tokenHeader)

	if fingerprint == "" || token == "" {
		writeAuthJSON(w, http.StatusUnauthorized,
			map[string]string{authErrorMember: "the request carries no fingerprint or no token"})

		return
	}

	host, mints := arrivals.(Identities)
	if !mints {
		writeAuthJSON(w, http.StatusNotImplemented,
			map[string]string{authErrorMember: "this host mints no device id, so there is none to answer with"})

		return
	}

	known, err := d.registrar.authenticate(fingerprint, token)
	if err != nil {
		writeAuthJSON(w, http.StatusForbidden,
			map[string]string{authErrorMember: "the printer's credentials were refused"})

		return
	}

	writeAuthJSON(w, http.StatusOK,
		map[string]string{authIDMember: host.DeviceID(d.identityOf(known, r))})
}

// writeAuthJSON writes one answer at authPath. The body is a flat string map,
// which is the shape the retired server published and a printer parses.
func writeAuthJSON(w http.ResponseWriter, status int, body map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// The response is already committed by WriteHeader, so a write that fails
	// has nothing left to tell the caller and the connection is gone anyway.
	_ = json.NewEncoder(w).Encode(body)
}
