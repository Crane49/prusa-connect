package event

// The payload types of the registration events.
//
// A registration is one machine asking to join this server through a
// connector. The machine shows a code and an operator decides. The origin is
// the connector instance, not a device. No payload contains a credential.

// The payload bounds, in bytes.
const (
	// RegistrationAnnouncedMaxBytes bounds the announced payload.
	RegistrationAnnouncedMaxBytes = 1024
	// RegistrationApprovedMaxBytes bounds the approved payload.
	RegistrationApprovedMaxBytes = 1024
	// RegistrationRejectedMaxBytes bounds the rejected payload.
	RegistrationRejectedMaxBytes = 512
	// RegistrationRefusedMaxBytes bounds the refused payload.
	RegistrationRefusedMaxBytes = 512
)

// RegistrationAnnounced is the data member of
// com.prusa.connect.device.registration.announced. It is what an operator
// matches against the machine in front of them.
type RegistrationAnnounced struct {
	// Code is what the machine polls with and what an operator reads off its
	// screen. It is the id every operation on the registration surface takes.
	Code string `json:"code"`
	// Connector is the connector instance of this registration, without the
	// resource kind: the origin reads driver:<name> and this reads <name>.
	Connector string `json:"connector"`
	// Fingerprint is the identity the machine keeps across reconnections.
	// After approval it is the key of the connector's allowlist.
	Fingerprint string `json:"fingerprint"`
	// Serial, Model and Firmware are what the machine said about itself. Each
	// is empty for a dialect whose handshake does not send it.
	Serial   string `json:"serial,omitempty"`
	Model    string `json:"model,omitempty"`
	Firmware string `json:"firmware,omitempty"`
}

// RegistrationApproved is the data member of
// com.prusa.connect.device.registration.approved. It is published for an
// operator's approval and for a connector that approves on arrival.
type RegistrationApproved struct {
	// Code is the registration that was approved.
	Code string `json:"code"`
	// Connector is the connector instance of the registration.
	Connector string `json:"connector"`
	// Fingerprint is the key of the connector's allowlist.
	Fingerprint string `json:"fingerprint"`
	// DeviceID is the device this approval bound the machine to. It is empty
	// for a registration with no fingerprint. Such a machine gets an id at its
	// first handshake.
	DeviceID string `json:"device_id,omitempty"`
	// Serial, Model and Firmware are what the machine said about itself.
	Serial   string `json:"serial,omitempty"`
	Model    string `json:"model,omitempty"`
	Firmware string `json:"firmware,omitempty"`
}

// RegistrationRejected is the data member of
// com.prusa.connect.device.registration.rejected. It is not permanent: the
// machine registers again at its next boot and gets a new code.
type RegistrationRejected struct {
	// Code is the registration that was rejected. After this frame the
	// surface answers for it as for a code that was never issued.
	Code string `json:"code"`
	// Connector is the connector instance of the registration.
	Connector string `json:"connector"`
	// Fingerprint is the identity the machine had claimed.
	Fingerprint string `json:"fingerprint"`
}

// The values of RegistrationRefused.Reason.
const (
	// RefusedUnknownCredentials is a machine whose credentials this server did
	// not issue, or that presented none.
	RefusedUnknownCredentials = "unknown_credentials"
	// RefusedNotAdmitted is a machine whose credentials were accepted and that
	// the host still would not take.
	RefusedNotAdmitted = "not_admitted"
)

// The values of RegistrationRefused.Admission.
const (
	// AdmissionApproval is a connector that waits for a person to approve each
	// registration.
	AdmissionApproval = "approval"
	// AdmissionAutomatic is a connector that approves every registration the
	// moment it arrives.
	AdmissionAutomatic = "automatic"
)

// RegistrationRefused is the data member of
// com.prusa.connect.device.registration.refused. It says why a machine that
// called is not joining. The caller is untrusted, so the payload has no
// credential, no network address and only a digest of the identity.
type RegistrationRefused struct {
	// Connector is the connector instance that refused the machine.
	Connector string `json:"connector"`
	// Reason is RefusedUnknownCredentials or RefusedNotAdmitted.
	Reason string `json:"reason"`
	// Admission is AdmissionApproval or AdmissionAutomatic: how that connector
	// lets a machine in.
	Admission string `json:"admission"`
	// FingerprintDigest is the hex-encoded SHA-256 of the identity the machine
	// presented, empty when it presented none. Two frames with one digest are
	// one machine calling again.
	FingerprintDigest string `json:"fingerprint_digest,omitempty"`
	// Model and Firmware are what the machine said it is.
	Model    string `json:"model,omitempty"`
	Firmware string `json:"firmware,omitempty"`
}
