// Package admission is the vocabulary of a device working through a dialect's
// pairing handshake: the code an operator reads off the machine, what the
// machine said it is, and whether anything has let it through.
//
// Two packages need one spelling of it. A dialect holds the registration table
// and the host publishes it, and neither may import the other. The handshake
// itself belongs to the dialect and is not described here.
//
// Registration and ErrUnknownCode are aliases of the published names in
// sdk/connectorapi, not copies. A dialect outside the core module cannot import
// this package, so a copy here would leave such a dialect unable to satisfy the
// interface the host asserts for. In-tree code writes admission.Registration,
// an out-of-tree module writes connectorapi.Registration, and the compiler
// treats them as one type. Store stays here: it is the host's own persistence
// port, and no dialect implements it.
package admission

import "github.com/prusa3d/prusa-connect/sdk/connectorapi"

// ErrUnknownCode reports a code no registration answers to. A driver holding a
// registration table returns it, and a caller inspects it with errors.Is.
//
// A rejected code and a code that was never issued give the same answer, so a
// caller cannot learn which codes this server has seen.
var ErrUnknownCode = connectorapi.ErrUnknownCode

// Registration is one device waiting for a decision, as an operator sees it.
// See [connectorapi.DeviceRegistration] for the field-by-field account.
type Registration = connectorapi.DeviceRegistration
