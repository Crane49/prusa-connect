package connectorapi

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/netip"
	"time"
)

// The optional halves of a driver: interfaces the host looks for and calls if
// a driver implements them, and does without if it does not.
//
// They are published here rather than declared where they are consumed because
// an out-of-tree adapter cannot read the consumer. A driver compiled against
// sdk/ alone (ADR-20260825-n55 rule 2) cannot import the host package that type-asserts
// for these. Without this file an author learns of them by reading core source
// they may not import, copies the method set by hand, and finds out about a
// rename by noticing that the feature has silently stopped. Everything here is
// declared so that a driver can write
//
//	var _ connectorapi.TakesLogger = (*Driver)(nil)
//
// and have the compiler hold the shape.
//
// Each is optional, in the strict sense: a driver that does not implement one
// is not refused and is not degraded beyond losing what that interface buys.
// The host asserts, and moves on.

// TakesLogger is the half of a driver that accepts the host's logger.
//
// A driver built from configuration is handed a spec and nothing else, so
// unless it says it takes a logger, every line it writes goes nowhere. The host
// calls UseLogger once, before the driver is reachable and before any device
// can arrive.
type TakesLogger interface {
	// UseLogger takes the logger this process writes through.
	UseLogger(*slog.Logger)
}

// ReportsProbe is an optional interface of a session. A session implements it
// to tell the host which capabilities this one printer supports. A probe is
// the question a driver asks a printer when it connects: which components and
// commands do you have?
//
// The host works out a printer's capabilities in three steps. It starts from
// the driver's Baseline. It removes what the host's profile for that model and
// firmware does not support. It then removes what the probe does not list. A
// step can only remove. A capability the probe lists and the Baseline does not
// have is ignored.
//
// Implement it when the printer answers that question during the handshake.
// A Klipper printer, for example, lists the components in its configuration.
// A driver that cannot ask, or learns the answer only after the host has the
// session, does not implement it. The host then uses the profile alone.
//
// The host calls Probe once, when it receives the session. The API lists the
// resulting commands for that device. The host refuses any other command with
// CAPABILITY_NOT_SUPPORTED, and the driver never receives it.
type ReportsProbe interface {
	// Probe returns the capabilities this printer reported, or nil when it
	// reported none. The host calls it once, so it may be slow.
	Probe() Set
}

// CountsLink is the half of a session that says what its connection has cost:
// telemetry the host was too slow to read, and frames the dialect could not
// read at all.
//
// Session.State requires every driver to bound what waits for the host and to
// count what it drops. This is how the host reads that count. It is a separate
// interface and not a method on Session because a method added to a published
// interface stops every driver already compiled against it from building, and
// because a dialect that reads whatever arrives has no unreadable frame to
// report.
//
// A session that does not implement it is reported as counting nothing, which a
// reader can tell apart from counting zero. Both totals only ever grow, for the
// life of the session.
type CountsLink interface {
	// Dropped is how many state patches this session discarded because the host
	// was not reading them.
	Dropped() uint64

	// Refused is how many frames the device sent that this dialect could not
	// read and dropped.
	Refused() uint64
}

// ReportsPeer is the half of a session that says which network address its
// device reaches the host from.
//
// The host compares that address with the printers it hears announce
// themselves on the local network. A printer that is already connected is then
// not offered to a person as one still to add. A session whose device is not on
// an IP network, such as one on a serial port, does not implement it.
//
// The address is the far end of the connection the driver holds. A device
// behind a proxy or a NAT router reports the proxy or the router, which no
// announcement names, so the host matches nothing for it.
type ReportsPeer interface {
	// Peer is the address the device's connection comes from right now. It is
	// the zero netip.Addr while no connection is open.
	Peer() netip.Addr
}

// DialsAbsolutePaths is the half of an inbound driver whose devices dial paths
// the host may not choose.
//
// Inbound lets the host pick a prefix and strip it, which is right for anything
// configurable. Firmware that is already in the field has its paths compiled in
// and is not updated on this project's schedule, so a driver serving those
// machines says which paths it needs and is mounted on them verbatim.
type DialsAbsolutePaths interface {
	// Routes are the absolute paths this driver's devices dial.
	Routes() []string
}

// DeviceListener is a socket of a dialect's own, for the devices it serves.
//
// A dialect whose devices reach the host over a network asks for one when those
// devices cannot be satisfied on the same socket as everything else. The
// xBuddy firmware is the case this exists for: it verifies against one
// certificate loaded from a USB stick and speaks TLS 1.2 with a single cipher
// suite, and a browser reaching the same socket would be held to that same
// floor. The host binds this address, serves the device routes on it and drains
// it at shutdown; the dialect decides the address, the certificate and the
// handshake budget, because each of the three is a property of the firmware.
type DeviceListener struct {
	// Addr is a host:port that net.Listen accepts. Empty asks for no socket of
	// its own, and the devices then dial the host's own listener, which is the
	// shipping default.
	Addr string

	// TLS is the profile that socket serves, or nil for plain HTTP. The
	// dialect builds it, so the version floor, the version ceiling and the
	// cipher suites are the firmware's and not the host's.
	TLS *tls.Config

	// HandshakeTimeout bounds one TLS handshake on that socket. Go takes the
	// handshake budget from the read and write timeouts of the server, so a
	// device whose processor is busy with its own work needs a value of its
	// own here. Zero leaves the host's own budget in place.
	HandshakeTimeout time.Duration
}

// ServesItsOwnSocket is the half of an inbound driver that asks the host for a
// listener of its own.
//
// The host calls it once, when it wires the driver, and binds what it is given
// before any device can arrive. A driver that does not implement it, and one
// that answers with an empty address, has its routes served on the host's own
// listener.
type ServesItsOwnSocket interface {
	// DeviceListener is the socket this dialect asks for. An empty Addr asks
	// for none.
	DeviceListener() DeviceListener
}

// ErrUnknownCode reports a registration code no table answers to. A driver
// implementing Admits returns it, and a caller inspects it with errors.Is.
//
// A code that has been rejected and a code that was never issued are the same
// answer. Telling them apart would let anything that can reach the operator
// surface learn which codes this server has seen.
var ErrUnknownCode = errors.New("connectorapi: unknown registration code")

// DeviceRegistration is one device waiting for a decision, as an operator sees
// it. It is spelled DeviceRegistration and not Registration because a driver
// registration already holds that name in this package: this one is about a
// machine asking to be let in, and that one is about a dialect being compiled
// in.
//
// It carries no credential. The token a driver issues is what the machine
// presents on its own wire afterwards, and an operator surface must not be a
// second way to obtain one.
type DeviceRegistration struct {
	// Code is what the machine polls with and what an operator reads off its
	// screen.
	Code string

	// Fingerprint is the identity the machine keeps across reconnections. It is
	// what a driver's allowlist is keyed by once the registration is approved.
	Fingerprint string

	// A registration names one spelling of one identity and no other. A
	// dialect whose machines spell their identity two ways reports the second
	// spelling on the admission instead ([Identity.UnitUIDAlias]), because
	// that is the exchange where a credential was checked: the leading
	// characters of an xBuddy fingerprint travel in the clear in every header,
	// so a registration that merely shares them with a working printer has
	// proved nothing, and a host that bound identities from a registration's
	// aliases let one operator approval hand a stranger the device id, the
	// token binding and the ownership of somebody else's machine.

	// Serial, Model and Firmware are what the machine said about itself in the
	// handshake. Each is the machine's own spelling, and each is empty for a
	// dialect whose handshake does not carry it.
	Serial   string
	Model    string
	Firmware string

	// Approved is whether anything has let this device through: an operator, or
	// a driver configured to approve on arrival.
	Approved bool
}

// Admits is the half of a driver that holds a registration table an operator
// decides on. A dialect whose devices pair by displaying a code implements it,
// and the host publishes that table at /api/v1/registrations.
//
// It is here, and its types are here, because that is what makes it reachable.
// An interface declared in the host with methods returning a type in the
// core's internal tree is one no module outside the core can ever satisfy: a
// third-party dialect whose devices pair by code would be shut out of the
// operator surface by the type system rather than by a decision. A
// driver that does not implement it admits a device without asking anybody and
// contributes no row to that surface, which is still a choice an author may
// make.
type Admits interface {
	// Pending are the devices waiting for a decision, in a stable order.
	Pending() []DeviceRegistration

	// Approve lets one through, so its next poll carries the credential it
	// dials with. A code the table does not hold is ErrUnknownCode.
	//
	// by is who decided, as the host names them. It is recorded against the
	// registration: an operator asking after the fact which of us let this
	// printer onto the farm is asking a question only the decision can answer.
	Approve(ctx context.Context, code, by string) (DeviceRegistration, error)

	// Reject forgets one registration and every credential it held. A code the
	// table does not hold is ErrUnknownCode.
	Reject(ctx context.Context, code string) error
}

// ApprovalNotice is how a driver tells the host about an approval the host did
// not make. by is who decided, as the host named them, and is empty for an
// approval a driver made on its own: nobody decided that one.
//
// The host binds the device id for the machine behind the registration, so an
// approval it never hears about is a machine that gets its id at its first
// handshake instead. A driver that reports one is reporting the same fact
// Admits.Approve returns, over the same type.
type ApprovalNotice func(ctx context.Context, registration DeviceRegistration, by string) error

// NotifiesApprovals is the half of a driver that approves a registration
// without being asked.
//
// A dialect configured to let a machine through the moment it registers decides
// inside its own table, so the host's approval path never runs and the identity
// that path binds is never bound. Such a driver says so here and is handed the
// notice to call. A driver that only ever approves through Admits.Approve
// implements nothing: the host is already the caller there.
type NotifiesApprovals interface {
	// UseApprovals takes the notice this driver calls for an approval it made
	// itself. The host calls it once, before the driver is reachable and before
	// any device can register.
	UseApprovals(notify ApprovalNotice)
}

// ReportsAdmission is the half of a driver holding a registration table that
// says whether a person decides on each registration.
//
// A page that helps a person add a device reads the answer before the machine
// calls. Where a person decides, the page asks them to compare the code and
// approve it; where nobody does, the page waits for the machine to join. A
// driver that holds a table and does not implement this is taken to ask a
// person, because that answer never skips a decision somebody has to make.
type ReportsAdmission interface {
	// AdmitsOnArrival reports whether every registration is approved the
	// moment it arrives. The answer is fixed when the driver is built.
	AdmitsOnArrival() bool
}

// RefusalReason is why a driver turned a device away at its own wire.
type RefusalReason string

// The reasons a device is turned away.
const (
	// RefusalUnknownCredentials is a device that presented credentials this
	// server did not issue, or none at all. The usual cause is a printer that
	// was never added, or one whose registration was rejected or forgotten.
	RefusalUnknownCredentials RefusalReason = "unknown_credentials"
	// RefusalNotAdmitted is a device whose credentials were accepted and that
	// the host still would not take, such as a device somebody retired.
	RefusalNotAdmitted RefusalReason = "not_admitted"
)

// DeviceRefusal is one device a driver turned away at its own wire.
//
// It carries no credential and no address. The values are what the device said
// about itself, which is untrusted text from a caller nobody has admitted.
type DeviceRefusal struct {
	// Reason is why the device was turned away.
	Reason RefusalReason
	// FingerprintDigest is the hex-encoded SHA-256 of the identity the device
	// presented, empty when it presented none. It tells two refused devices
	// apart without publishing the identity itself.
	FingerprintDigest string
	// Model and Firmware are what the device said it is, each empty for a
	// handshake that does not carry it.
	Model    string
	Firmware string
}

// RefusalNotice is how a driver tells the host about a device it turned away.
// The driver decides how often: a refused device usually calls again every
// few seconds, and one notice per attempt would bury the person reading them.
type RefusalNotice func(ctx context.Context, refusal DeviceRefusal)

// ReportsRefusals is the half of a driver that tells the host about the
// devices it turns away at its own wire.
//
// Without it a refused device is a line in the log, and a person waiting for a
// printer to join waits for something that will never happen.
type ReportsRefusals interface {
	// UseRefusals takes the notice this driver calls for a device it turned
	// away. The host calls it once, before the driver is reachable.
	UseRefusals(notify RefusalNotice)
}

// ForgetsDevices is the half of a driver that can take back the credential it
// issued for one device.
//
// An operator who removes a device from this server expects it to stay out.
// The host retires the record, but a driver that still holds the device's
// token or approved fingerprint lets the same machine dial in again the next
// minute, and the host then admits it as a new device without anybody
// deciding. The host calls ForgetDevice when a device is retired, so the
// machine has to register again, and be approved again where a person
// approves, before it is let back in.
//
// A driver that issues no credential has nothing to take back and does not
// implement it. One configured to check no credential may keep what it holds,
// and take it back when it is called again under a setting that checks one.
type ForgetsDevices interface {
	// ForgetDevice withdraws every credential this driver issued for the
	// machine identity names, when it was issued before the moment given. The
	// moment keeps a registration the machine made after its record was
	// retired: that one is a new request, and a person may already have
	// approved it.
	//
	// A machine the driver holds no credential for is not an error, so the
	// host may call it twice for one device. The withdrawal is durable when
	// it returns nil: a driver whose table survives a restart has written it
	// there.
	ForgetDevice(ctx context.Context, identity Identity, issuedBefore time.Time) error
}
