package xbuddy

import (
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The bounds a raw gcode payload is held to. The length is the descriptor's,
// declared in the wire table; the rest cannot be said in a descriptor and lives
// here under ADR-20260825-dee rule 8.
const (
	// maxRawGCode is the payload length in bytes. It is generous enough for a
	// long M117 message and short enough that nothing here allocates in
	// proportion to a body.
	maxRawGCode = 256

	// The printable ASCII range. Every other byte is refused, which is what
	// stops a second command being smuggled behind a carriage return: the
	// firmware reads a multi-line payload, and the templates in gcode.go send
	// one on purpose.
	firstPrintable = 0x20
	lastPrintable  = 0x7E

	// letterAndDigit is the shortest word this parser accepts, such as "G0".
	letterAndDigit = 2
)

// refusedWords are the gcode words this server never puts on the wire, keyed by
// the letter and the whole-number part so that a decimal spelling of one cannot
// walk past it.
//
// It is a denylist and not an allowlist on purpose (ADR-20260825-z65 rule 7). An
// allowlist compiled into the baseline is a ceiling on what a customer may
// send that only a release can raise, and its marginal safety is small because
// gcode's danger lives in parameters that no word list reaches. A deployment
// that wants an allowlist narrows the argument's Supported set in a profile.
//
//nolint:gochecknoglobals // a closed set read on every command, never written.
var refusedWords = map[string]string{
	"M997": "firmware is not flashed through this API",
	"M28":  "a file is written through the file capabilities, which check the path",
	"M29":  "a file is written through the file capabilities, which check the path",
	"M30":  "a file is deleted through the file capabilities, which check the path",
}

// refusedLetter is the whole letter this server never sends. Buddy's D codes
// read and write memory, flash and OTP by address, which is the one thing here
// that damages a machine past recovery.
const refusedLetter = 'D'

// commandLetters are the letters a payload may start with.
const commandLetters = "GMT"

// gcodeRaw renders a raw gcode payload, refusing everything the descriptor
// cannot state.
//
// Three refusals, and the order matters. Printable ASCII first, because a
// payload carrying a newline is two commands and the word check would see only
// the first. Then the parse, which is what makes the word extraction total: a
// spelling this cannot read is refused rather than read as far as it goes, so a
// refused word has no spelling that reaches the wire. Then the word itself.
func gcodeRaw(args arguments) (string, error) {
	payload, supplied := args.text("gcode")
	if !supplied {
		return "", malformed("", "no payload was supplied")
	}

	if err := refuseUnprintable(payload); err != nil {
		return "", err
	}

	trimmed := strings.Trim(payload, " ")
	if trimmed == "" {
		return "", malformed(payload, "the payload is blank")
	}

	// The whole letter goes before the parse, so that D-1 and D3 are refused as
	// what they are rather than as something unreadable.
	if upper(trimmed[0]) == refusedLetter {
		return "", notAllowed(trimmed, string(refusedLetter),
			"a D code reads and writes memory, flash and OTP by address")
	}

	word, err := commandWord(trimmed)
	if err != nil {
		return "", err
	}

	if reason, refused := refusedWords[word]; refused {
		return "", notAllowed(trimmed, word, reason)
	}

	return trimmed, nil
}

// refuseUnprintable rejects any byte outside printable ASCII.
//
// It is a byte loop and not a rune loop because the rule is about bytes: a
// multi-byte rune has no byte in the printable range, so refusing byte by byte
// refuses it, and a lone continuation byte that is not valid UTF-8 is refused
// by the same pass rather than becoming a replacement character.
func refuseUnprintable(payload string) error {
	for i := range len(payload) {
		if payload[i] < firstPrintable || payload[i] > lastPrintable {
			return malformed(payload,
				"the payload carries a byte outside printable ASCII, and a payload is one line")
		}
	}

	return nil
}

// commandWord returns the letter and whole number a payload starts with,
// uppercased, which is the form refusedWords is keyed by. A payload that does
// not start with one is refused.
//
// The digit run is greedy and the decimal part is dropped. Greedy is what makes
// M2830 a different word from M28 rather than a prefix of it, and dropping the
// decimal is what makes M28.1 the same word as M28: keying on the whole text
// would let a suffix nothing implements walk past the refusal.
//
// Nothing is required after the number. Marlin reads M104S215 and M997;now as
// the command their first word names, so a parser that insisted on a space
// would refuse the first for no reason and, worse, would refuse the second as
// unreadable rather than as the flash command it is.
func commandWord(payload string) (string, error) {
	if len(payload) < letterAndDigit {
		return "", malformed(payload, "the payload is shorter than one gcode word")
	}

	letter := upper(payload[0])
	if !strings.ContainsRune(commandLetters, rune(letter)) {
		return "", malformed(payload, "a command starts with G, M or T followed by its number")
	}

	digits := countDigits(payload[1:])
	if digits == 0 {
		return "", malformed(payload, "the letter is not followed by a number")
	}

	return string(letter) + payload[1:1+digits], nil
}

// countDigits returns how many ASCII digits s starts with.
func countDigits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}

	return n
}

// upper folds one ASCII letter. strings.ToUpper would allocate on every
// command, and the payload has already been held to ASCII.
func upper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - ('a' - 'A')
	}

	return b
}

func malformed(payload, reason string) error {
	return connectorapi.GCodeMalformed.New(reason).
		With(fault.StringParam("gcode", payload))
}

func notAllowed(payload, word, reason string) error {
	return connectorapi.GCodeNotAllowed.New("this server never sends that gcode word").
		With(fault.StringParam("gcode", payload),
			fault.StringParam("word", word),
			fault.StringParam("reason", reason))
}
