package admission

import (
	"context"
	"time"
)

// A registration table kept only in a dialect's memory is lost at every
// restart. An approved machine then comes back with a token the table does not
// hold and is refused, so a whole fleet has to be approved again by hand.
//
// The port is here for the same reason as the rest of the package: the dialect
// holds the table, the host holds the database, and neither may import the
// other. The host satisfies it. A dialect handed a store writes through it and
// reads it back at startup; a dialect handed none keeps its table in memory,
// which keeps a driver testable with no database.

// Record is one registration as it is kept between processes.
//
// It carries no token, only the digest of one. A row an attacker reads must not
// be a credential they can present, and a driver only ever needs to answer
// whether a token presented on the wire is the one it issued.
type Record struct {
	// Connector is the instance that minted the code. A code is minted per
	// connector, and two connectors may mint the same one, so nothing here is
	// addressable without it.
	Connector string

	// Code is what the machine polls with and what an operator reads off its
	// screen.
	Code string

	// Fingerprint is the identity the machine keeps across reconnections.
	Fingerprint string

	// TokenHash is the hex-encoded SHA-256 of the token the driver issued, and
	// is empty for a registration whose token was never minted.
	TokenHash string

	// Serial, Model and Firmware are what the machine said about itself, each
	// in its own spelling and each empty for a handshake that does not carry
	// it.
	Serial   string
	Model    string
	Firmware string

	// Kind is the device kind this connector registers its machines under, so a
	// listing read before anything dials still says what is waiting.
	Kind string

	// Approved is whether anything has let this device through. Endorsed is
	// whether a person did: auto-approval sets Approved and leaves Endorsed
	// false, and a full table never drops an entry a person endorsed. Completed
	// is whether the machine collected its token, the only sign a row holds
	// that a machine is behind it and not some other caller.
	Approved  bool
	Endorsed  bool
	Completed bool

	// AdmittedBy and AdmittedAt are who let this device through and when, empty
	// and zero for a registration nobody has decided. An operator reads them to
	// find out who approved a device.
	AdmittedBy string
	AdmittedAt time.Time

	// IssuedAt is when the code was minted. ExpiresAt is when a registration
	// nobody has approved stops being usable, and is zero for one that does not
	// expire. An approved registration carries no expiry: it is the credential
	// the machine dials with.
	IssuedAt  time.Time
	ExpiresAt time.Time

	// Seq is the order the machines registered in, which is what decides who is
	// dropped when the table is full. It is a counter and not a clock, so two
	// registrations in one millisecond are still ordered.
	Seq uint64
}

// Expired reports whether an unapproved registration has run out of time. An
// approved one never has: the operator's decision is what the row is for.
func (r Record) Expired(now time.Time) bool {
	return !r.Approved && !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt)
}

// Store keeps a connector's registrations between processes. It is a port: the
// host satisfies it over its own database, and a driver either has one or has
// none.
//
// Every method takes the connector instance, because one store serves every
// connector on the server and a code alone does not say whose it is.
type Store interface {
	// Load returns every registration this connector holds, in the order they
	// were registered. An expired one is the caller's to drop: a store that
	// dropped rows on a read would decide policy.
	Load(ctx context.Context, connector string) ([]Record, error)

	// Save writes one registration, replacing the row already held for that
	// code.
	Save(ctx context.Context, record Record) error

	// Forget removes one registration and every credential it held. Removing
	// one that is not there is not an error: the caller wanted it gone and it
	// is gone.
	Forget(ctx context.Context, connector, code string) error
}
