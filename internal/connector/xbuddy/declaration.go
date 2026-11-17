// The two things this driver publishes about itself: the descriptor the
// extension reference reads, and the declaration a host and a page read.
//
// They are in a file of their own because they are documentation rather than
// wire code, and because xbuddy.go is at the length limit the linter holds this
// package to.

package xbuddy

import (
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// dialect is what this driver says about the machines it talks to, for the host
// and for a page that asks a person which machine they are adding.
//
// It is here and not in the host for one reason: the host would have to name
// this dialect to know any of it, and a table of dialect names in the core is a
// table an out-of-tree driver cannot add a row to.
//
// The admission words are this driver's own spelling of the two choices the
// first run offers. Mode none admits every printer that dials. Mode token makes
// the registration the credential, and a registration in that mode is never
// approved without an operator. That is what "ask me first" means: a printer
// is never let in without a person deciding.
func dialect() connectorapi.Dialect {
	return connectorapi.Dialect{
		Brands: []string{Vendor},
		Needs:  connectorapi.NeedsSettingsFile,
		Admission: connectorapi.Admission{
			Key:  admissionKey,
			Open: string(ModeNone),
			Ask:  string(ModeToken),
		},
		States: states(),
	}
}

// admissionKey is the settings key this driver reads its admission mode under.
// The published option below and the declaration above both use it, so the two
// cannot name different keys.
const admissionKey = "mode"

// doc is what the published plugin reference says about this driver.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "The dialect Prusa xBuddy and Buddy boards speak, over the frozen registration and websocket wire.",
		Description: "The only dialect with machines in the field, so its wire is frozen rather than chosen: " +
			"the registration handshake, the nine-byte framing and the verb rewrite are what firmware that " +
			"cannot be updated on our schedule already speaks. It is inbound, so nothing in configuration " +
			"names a printer: the printer registers, waits to be approved, and then dials.",
		Version:   "1.0.0",
		Contract:  connectorapi.Contract,
		Stability: extension.StabilityStable,
		Options: []extension.Option{
			{
				Key:     admissionKey,
				Type:    extension.OptionString,
				Default: string(ModeNone),
				Summary: "The credential a printer presents on the websocket: none, token, fingerprint or both.",
			},
			{
				Key:  "auto_register",
				Type: extension.OptionBool,
				// No default, because the key is no longer a setting: the mode
				// decides, and this is accepted only where it says what the
				// mode already says. Writing `true` here published a default
				// that is false in three of the four modes and that nothing
				// reads in any of them.
				Summary: "Retired, and it has no default because the mode decides. Whether a " +
					"registration is approved the moment it is made is the admission mode and " +
					"nothing else, so this key is accepted only where it says what the mode " +
					"already says: true in mode none, which admits every printer that dials, " +
					"and false in mode token, fingerprint and both, where the registration is " +
					"the credential and an operator decides. Any other combination is refused " +
					"at startup, with a sentence naming the mode that does what was asked for.",
			},
			{
				Key:     "max_registrations",
				Type:    extension.OptionInt,
				Default: "400",
				Summary: "How many printers this driver keeps a registration for. The registration " +
					"path carries no credential, so this is what bounds a table anything that can " +
					"open a socket may write to. Over it, the oldest registration no printer " +
					"completed is dropped.",
			},
			{
				Key:     "queue_depth",
				Type:    extension.OptionInt,
				Default: "32",
				Summary: "How many commands may wait for one printer, whether it is connected or away.",
			},
			{
				Key:  "chunk_write_ceiling",
				Type: extension.OptionInt,
				// The number comes from the constant so that the published
				// default and the code cannot drift apart.
				Default: strconv.Itoa(defaultChunkWriteCeiling),
				Summary: "How many bytes of the file one transfer chunk may carry, whatever chunk " +
					"size the printer asks for. The default puts exactly 1024 bytes on the TLS " +
					"connection per write - payload, the nine-byte transfer header and the " +
					"four-byte WebSocket header - because Buddy firmware compiles mbedTLS with a " +
					"1024-byte record buffer and drops the connection on anything longer. It " +
					"never advertises that limit, so raise this only for a board known to read " +
					"larger records, and only where nothing between host and printer re-frames " +
					"the stream.",
			},
			{
				Key:     "offline_grace",
				Type:    extension.OptionDuration,
				Default: "0s",
				Summary: "Close a session whose printer has been away that long. Zero lets a session outlive any absence.",
			},
			{
				Key:     "default_storage",
				Type:    extension.OptionString,
				Default: "/usb",
				Summary: "The firmware-side mountpoint a path with no storage segment resolves against.",
			},
			{
				Key:     "read_deadline",
				Type:    extension.OptionDuration,
				Default: readDeadline.String(),
				Summary: "How long a printer's connection may say nothing before it is reported " +
					"unreachable. It is what notices a link that drops packets rather than hanging up, " +
					"and shortening it is the only way to notice one sooner.",
			},
			{
				Key:     "ping_interval",
				Type:    extension.OptionDuration,
				Default: pingInterval.String(),
				Summary: "How often the host asks a printer to prove it is there. Every frame the " +
					"printer sends re-arms the read deadline, and a pong is what a quiet printer sends, " +
					"so this sits below it and never above the shipping 54s.",
			},
			{
				Key:     "ping_timeout",
				Type:    extension.OptionDuration,
				Default: pingTimeout.String(),
				Summary: "How long writing one ping may take before the connection is treated as stuck.",
			},
			{
				Key:     "listener",
				Type:    extension.OptionObject,
				Default: "no socket",
				Summary: "A socket of this dialect's own for printers to dial, with addr, tls and " +
					"handshake_timeout under it. No block is the default, and printers then dial " +
					"the server's own listener. The certificate must be ECDSA, because the firmware " +
					"offers one cipher suite and that suite needs an ECDSA key.",
			},
			{
				Key:     "command_deadline",
				Type:    extension.OptionDuration,
				Default: defaultCommandDeadline.String(),
				Summary: "How long one command waits for the printer to answer it, counted from the " +
					"moment its frame is written. A printer that reports telemetry and answers nothing " +
					"keeps its connection healthy, so this is the only thing that ends that wait. A " +
					"G-code command is answered when the printer accepts it, and the printer refuses " +
					"every other command until it finishes, so this also bounds how long the next " +
					"command is held while a G-code command runs.",
			},
		},
	}
}
