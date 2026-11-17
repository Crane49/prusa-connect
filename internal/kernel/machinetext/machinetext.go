// Package machinetext bounds one piece of text a device chose: a refusal
// reason, a dialog title, a filename, a job name.
//
// The bound belongs to the kernel because the same value reaches an HTTP
// answer, a log line, an audit record and the state tree. One bound keeps them
// in step.
package machinetext

import (
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// MaxBytes is how much of one free-text value from a device this server keeps.
// It is the bound Kubernetes puts on Condition.message, a field of the same
// kind: text a machine writes and a person reads. internal/kernel/condition
// uses the same value for a condition message.
//
// It caps what is stored. It does not limit what a device may send; that limit
// belongs to the device's dialect.
const MaxBytes = 32768

// Marker is added to the end of a value this server cut, so a reader can tell a
// cut value from a complete one.
const Marker = "…"

// Clip returns text cut to at most limit bytes, on a rune boundary, with Marker
// added. A cut through a multi-byte character would reach a JSON encoder as the
// replacement character, and much of what a firmware writes is not in English.
//
// To learn whether anything was cut, compare the length of the result with the
// length of the input.
func Clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}

	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return text[:cut] + Marker
}

// IdentityMaxBytes is how much of one field of a device identity this server
// keeps. A vendor name, a model name, a serial number and a firmware version
// are all short, and a device that sends more of one is not describing itself.
//
// It is far below MaxBytes because an identity is not free text. Each field
// reaches a device record, a log line, a metric label and an event, and a metric
// label of any length is a series an operator's storage has to hold.
const IdentityMaxBytes = 256

// ClipIdentity returns the identity with every text a device chose cut to
// IdentityMaxBytes, on a rune boundary and marked, the way Clip cuts any other
// text a device chose.
//
// UnitUID and UnitUIDAlias are not cut. They name the machine, so a cut one
// addresses a different device, and a host that shortened them would file two
// machines under one id. A dialect bounds its own identifier.
func ClipIdentity(identity connectorapi.Identity) connectorapi.Identity {
	identity.Vendor = Clip(identity.Vendor, IdentityMaxBytes)
	identity.Model = Clip(identity.Model, IdentityMaxBytes)
	identity.Serial = Clip(identity.Serial, IdentityMaxBytes)

	// MarshalText and not String: the forbidigo rule on String keeps a version
	// comparison out of every package but the two resolvers, and this is a cut.
	text, err := identity.Firmware.MarshalText()
	if err != nil {
		return identity
	}

	identity.Firmware = connectorapi.NewFirmwareLabel(Clip(string(text), IdentityMaxBytes))

	return identity
}
