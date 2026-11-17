package printfile

import (
	"strconv"
	"strings"
)

// The firmware reads the first G-code lines of a file before it prints and
// checks the printer against what they ask for. This file reads the same
// lines the same way, so a caller can tell what the firmware will check. The
// rules are the firmware's: GCodeInfo::load and GCodeInfo::parse_gcode in
// src/common/gcode/gcode_info.cpp of Prusa-Firmware-Buddy.

// maxScannedGCodes is how many G-code lines the firmware reads for its checks
// (search_first_x_gcodes in src/common/gcode/gcode_info.hpp). A comment line
// is not counted, and neither is M486, which a slicer writes once per object.
const maxScannedGCodes = 200

// The bounds on what one file's checks may hold. PrusaSlicer writes one M862.1
// line per used filament and two or three M862.6 lines. The nozzle checks are
// bounded only by the lines the scan reads, and the others leave room and
// still stop a file from filling the details with lines.
const (
	maxNozzleChecks = maxScannedGCodes
	maxFeatures     = 16
	maxCheckText    = 64
)

// headScanner gathers the checks out of G-code lines, one line at a time.
type headScanner struct {
	checks  Checks
	counted int
	// read is whether any G-code line reached the scanner at all. A file whose
	// G-code could not be read has no checks, which is not the same as a file
	// that asks for none.
	read bool
}

// done reports whether the scanner has read as many G-code lines as the
// firmware does.
func (h *headScanner) done() bool { return h.counted >= maxScannedGCodes }

// result is the checks gathered, and nil when no G-code line was read.
func (h *headScanner) result() *Checks {
	if !h.read {
		return nil
	}

	out := h.checks

	return &out
}

// line reads one G-code line.
func (h *headScanner) line(raw string) {
	if h.done() {
		return
	}

	line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
	if line == "" || line[0] == ';' {
		return
	}

	h.counted++

	// A line number goes before the command on a line sent over a serial
	// port, and a file may carry one too.
	if line[0] == 'N' {
		line = strings.TrimLeft(line[1:], "0123456789 \t")
	}

	command, rest := commandOf(line)

	// A file of plain text has lines too. Only a line that starts with a
	// G-code command says the G-code was read.
	if len(command) > 1 && isDigit(command[1]) {
		h.read = true
	}

	switch {
	case strings.HasPrefix(command, "M862."):
		h.m862(strings.TrimPrefix(command, "M862."), rest)
	case command == "M115":
		h.m115(rest)
	case command == "M486":
		// Written once per object, and the firmware does not count it, so a
		// plate of two hundred objects still has its checks read.
		h.counted--
	}
}

// commandOf splits a line into its command, such as M862.3, and the rest.
// A comment after the command is dropped, unless the semicolon is inside a
// quoted value.
func commandOf(line string) (string, string) {
	line = withoutComment(line)

	end := 0
	for end < len(line) && (isDigit(line[end]) || line[end] == '.' || (end == 0 && isLetter(line[end]))) {
		end++
	}

	return strings.ToUpper(line[:end]), line[end:]
}

// withoutComment cuts a line at the first semicolon outside quotes.
func withoutComment(line string) string {
	quoted := false

	for i := range len(line) {
		switch line[i] {
		case '"':
			quoted = !quoted
		case ';':
			if !quoted {
				return strings.TrimSpace(line[:i])
			}
		}
	}

	return strings.TrimSpace(line)
}

// param is one letter of a command and the value after it.
type param struct {
	letter byte
	value  string
}

// paramsOf reads the parameters of one command: a letter, then its value. A
// value in quotes may hold spaces, and the space between the letter and an
// opening quote is allowed, as in M862.3 P "MK4S".
func paramsOf(rest string) []param {
	var out []param

	for i := 0; i < len(rest); {
		if !isLetter(rest[i]) {
			i++

			continue
		}

		letter := upper(rest[i])
		i++

		for i < len(rest) && rest[i] == ' ' && i+1 < len(rest) && rest[i+1] == '"' {
			i++
		}

		start := i

		if i < len(rest) && rest[i] == '"' {
			closing := strings.IndexByte(rest[i+1:], '"')
			if closing < 0 {
				out = append(out, param{letter: letter, value: rest[i+1:]})

				break
			}

			out = append(out, param{letter: letter, value: rest[i+1 : i+1+closing]})
			i += closing + len(`""`)

			continue
		}

		for i < len(rest) && rest[i] != ' ' && rest[i] != '\t' {
			i++
		}

		out = append(out, param{letter: letter, value: rest[start:i]})
	}

	return out
}

// m862 reads one M862 line. The sub-code says which check it asks for.
func (h *headScanner) m862(sub, rest string) {
	params := paramsOf(rest)
	if sub == "1" {
		h.nozzle(params)

		return
	}

	value, ok := valueOf(params, 'P')
	if !ok {
		return
	}

	switch sub {
	case "2":
		h.checks.PrinterCode = positive(value)
	case "3":
		h.checks.PrinterModel = checkText(value)
	case "4":
		h.checks.MinFirmware = checkText(value)
	case "5":
		h.checks.GCodeLevel = positive(value)
	case "6":
		if feature := checkText(value); feature != "" && len(h.checks.Features) < maxFeatures {
			h.checks.Features = append(h.checks.Features, feature)
		}
	}
}

// positive reads a whole number above zero, and answers zero for anything
// else.
func positive(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0
	}

	return n
}

// nozzle reads one M862.1 line: T names the tool, counted from 0, P the
// diameter, A whether the nozzle has to be hardened and F whether it has to be
// high-flow.
func (h *headScanner) nozzle(params []param) {
	if len(h.checks.Nozzles) >= maxNozzleChecks {
		return
	}

	var check NozzleCheck

	for _, p := range params {
		switch p.letter {
		case 'T':
			if n, err := strconv.Atoi(p.value); err == nil && n >= 0 {
				check.Index = n
			}
		case 'P':
			check.DiameterMM = number(p.value)
		case 'A':
			check.Hardened = flag(p.value)
		case 'F':
			check.HighFlow = flag(p.value)
		}
	}

	h.checks.Nozzles = append(h.checks.Nozzles, check)
}

// m115 reads M115 U, the newest firmware the slicer's profile knows.
func (h *headScanner) m115(rest string) {
	if value, ok := valueOf(paramsOf(rest), 'U'); ok {
		h.checks.Firmware = checkText(value)
	}
}

// valueOf is the value of the first parameter with this letter.
func valueOf(params []param, letter byte) (string, bool) {
	for _, p := range params {
		if p.letter == letter {
			return strings.TrimSpace(p.value), true
		}
	}

	return "", false
}

// checkText bounds a value a check line names.
func checkText(value string) string {
	value = cleanField(value)
	if len(value) > maxCheckText {
		return ""
	}

	return value
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}

	return c
}

// scanGCodeText reads the G-code lines of a section of text, until the scanner
// has read as many as the firmware does.
func scanGCodeText(text string, scanner *headScanner) {
	for line := range strings.SplitSeq(text, "\n") {
		if scanner.done() {
			return
		}

		scanner.line(line)
	}
}
