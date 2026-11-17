package authz

import (
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// The verb table has two halves.
//
// The generated half is the printer rows: a static Go table, reviewed in pull
// requests, keyed to the device kind `printer`.
//
// The per-build half is every other kind's verbs, gathered once at start from
// the declarations compiled into this binary. It is never read from
// configuration and never from a running process. What is in it is what
// somebody chose to build in. go.sum pins what was built and the module path
// names who wrote it.
//
// The table is total over the union of the halves. Every verb either half
// names has exactly one class per kind. There is no default arm, and a (kind,
// verb) pair nothing declared yields the zero Action, whose Valid() is false.
// A default arm would decide for every verb written after it, and nobody has
// weighed those verbs.

// DeviceKind is the dimension the verb table is keyed by. It is ref's, spelled
// here so a caller of this package does not have to import the kernel to name
// one.
type DeviceKind = ref.DeviceKind

// The device kinds this build carries in its own right.
const (
	DeviceKindPrinter = ref.DeviceKindPrinter
	DeviceKindCamera  = ref.DeviceKindCamera
)

// Marking is a reason a verb is in the dangerous tier: conferred by no class,
// no role and no level, and reached only by a row naming subject, device and
// verb.
type Marking string

// The markings.
const (
	// MarkingIrreversible is an act this server cannot undo.
	MarkingIrreversible Marking = "irreversible"
	// MarkingOpaquePayload is a payload nothing inspected. The host computes it
	// from the capability descriptor, so a declaration cannot avoid it.
	MarkingOpaquePayload Marking = "opaque_payload"
)

// String returns the marking as it is written in the inventory.
func (m Marking) String() string { return string(m) }

// Valid reports whether m is a marking this build knows.
func (m Marking) Valid() bool { return m == MarkingIrreversible || m == MarkingOpaquePayload }

// Markings returns the closed set, in declaration order.
func Markings() []Marking { return []Marking{MarkingIrreversible, MarkingOpaquePayload} }

// Marker is who put a marking on a verb. An adapter may raise its own bar and
// may never clear a marking it did not make, and the host's own marking is
// cleared by nobody.
type Marker string

// The markers, strongest first.
const (
	// MarkerHost is the host's own computation. Nothing clears it.
	MarkerHost Marker = "host"
	// MarkerAdapter is the declaration's own marking.
	MarkerAdapter Marker = "adapter"
)

// String returns the marker as the inventory renders it.
func (m Marker) String() string { return string(m) }

// verbEntry is one verb of one kind: the class it carries and who marked it
// how. It is the value both halves of the table hold, so the generated rows and
// a declared one cannot be told apart by anything downstream.
type verbEntry struct {
	class    Action
	markings map[Marking]Marker
}

// dangerous reports whether this verb carries a marking.
func (e verbEntry) dangerous() bool { return len(e.markings) > 0 }

// markingNames renders the markings in the closed set's order, so the inventory
// and a pin record read the same for the same verb.
func (e verbEntry) markingNames() []Marking {
	out := make([]Marking, 0, len(e.markings))

	for _, marking := range Markings() {
		if _, marked := e.markings[marking]; marked {
			out = append(out, marking)
		}
	}

	return out
}

// KindVerbs is one kind's verbs as a caller reads them: the module that
// declared the kind, the display name, and every verb with its class and
// markings. It is what the served inventory renders and what the per-build half
// is installed from.
type KindVerbs struct {
	// Module is the Go module path that declared this kind, empty for the
	// generated half. It is part of the first-seen key, because renaming the
	// kind does not change it.
	Module string
	// Kind is the device kind, qualified for a declared one.
	Kind DeviceKind
	// DisplayName is what an operator reads in a listing.
	DisplayName string
	// Verbs is every verb of this kind with its class and markings.
	Verbs []VerbRow
}

// VerbRow is one verb of one kind, rendered.
type VerbRow struct {
	Verb     string
	Class    Action
	Markings []MarkingRow
}

// MarkingRow is one marking and who put it there.
type MarkingRow struct {
	Marking Marking
	Marker  Marker
}

// verbTable is the two halves indexed together. It is replaced whole when the
// per-build half is installed, never mutated in place, so a reader holding one
// reads a consistent table for as long as it holds it.
type verbTable struct {
	byKind map[DeviceKind]map[string]verbEntry
	// union is every verb either half names, whatever kind names it. Stage one
	// of the two-stage refusal reads this and nothing else, so the refusal for
	// a verb nothing declares carries no fact about any device.
	union map[string]bool
	// kinds is the declared kinds in a stable order, for the inventory.
	kinds []KindVerbs
}

// installed is the table this binary answers from. It starts as the generated
// half alone and is replaced once, at start, when the declarations compiled
// into this binary are installed. The mutex covers the swap: a test installs a
// table and the API serves the inventory from another goroutine.
//
//nolint:gochecknoglobals // the vocabulary's own table; one value, replaced once at start.
var installed = struct {
	mu    sync.RWMutex
	table *verbTable
}{table: generatedTable()}

// table returns the table in force.
func table() *verbTable {
	installed.mu.RLock()
	defer installed.mu.RUnlock()

	return installed.table
}

// setTable replaces the table in force. It is called once, at start, by the
// store that validated and pinned the declarations.
func setTable(t *verbTable) {
	installed.mu.Lock()
	defer installed.mu.Unlock()

	installed.table = t
}

// ResetVerbTable puts the generated half back, for a test that installed a
// declaration and must not leak it into the next one.
func ResetVerbTable() { setTable(generatedTable()) }

// ActionForVerb returns the class a verb carries on a device of this kind, and
// the zero Action for a (kind, verb) pair the table does not name.
//
// It is one function and not a check at each call site, because this server
// sends commands from more than one place, and a site that names an action
// directly is right only for the verbs that existed when it was written.
//
// The zero Action carries no mask, so every check of it is refused with
// core.auth/UNKNOWN_ACTION. A verb nobody classified reaches no device. A
// default arm would hand it to whoever holds the weakest write action. A verb
// another kind declares does not exist for kind printer.
func ActionForVerb(kind DeviceKind, verb string) Action {
	entry, named := table().lookup(kind, verb)
	if !named {
		return ""
	}

	return entry.class
}

// Declared reports whether this kind names this verb at all.
func Declared(kind DeviceKind, verb string) bool {
	_, named := table().lookup(kind, verb)

	return named
}

// Dangerous reports whether this verb carries a marking on this kind. An
// unmarked verb is conferred by its class. A marked one is conferred by no
// class, role or level and takes a row of its own.
func Dangerous(kind DeviceKind, verb string) bool {
	entry, named := table().lookup(kind, verb)

	return named && entry.dangerous()
}

// MarkingsOf returns the markings on one verb of one kind, in the closed set's
// order, and nil for a verb the kind does not name.
func MarkingsOf(kind DeviceKind, verb string) []Marking {
	entry, named := table().lookup(kind, verb)
	if !named {
		return nil
	}

	return entry.markingNames()
}

// KnownVerb reports whether any compiled-in kind names this verb. It is the
// first stage of the refusal, and the only check that runs before a device is
// resolved. The answer is the same whatever the caller holds and whether or
// not the id names a machine, so it carries no fact about any device and
// does not reveal whether a device exists. The union is part of the
// published vocabulary, served to a caller holding no credential, so the
// refusal discloses nothing the vocabulary route does not.
func KnownVerb(verb string) bool { return table().union[verb] }

// DangerousVerbsOf returns every dangerous verb of one kind, sorted. It is what
// ownership expands into: the relation device#owner implies one per-verb row per
// dangerous verb of that device's kind, computed and not stored.
func DangerousVerbsOf(kind DeviceKind) []string {
	verbs := table().byKind[kind]

	out := make([]string, 0, len(verbs))

	for verb, entry := range verbs {
		if entry.dangerous() {
			out = append(out, verb)
		}
	}

	slices.Sort(out)

	return out
}

// KnownKind reports whether this build carries this device kind at all: the
// generated half's kinds and every kind a compiled-in declaration named. The
// directory holds a registration to it, so a device cannot be filed under a
// kind whose verbs nothing can classify.
func KnownKind(kind DeviceKind) bool {
	_, known := table().byKind[kind.Or(DeviceKindPrinter)]

	return known
}

// Kinds returns every device kind this build carries, sorted, with printer
// first so a listing does not reorder when a module is added.
func Kinds() []DeviceKind {
	out := make([]DeviceKind, 0, len(table().kinds))
	for _, kind := range table().kinds {
		out = append(out, kind.Kind)
	}

	return out
}

// Inventory returns every kind and every verb of it, with the class, the
// markings and who marked each. The person who added a module to a build has
// very likely not read its verb table, and this is where they read it.
func Inventory() []KindVerbs { return table().kinds }

// lookup is the one read both halves answer through.
func (t *verbTable) lookup(kind DeviceKind, verb string) (verbEntry, bool) {
	verbs, known := t.byKind[kind.Or(DeviceKindPrinter)]
	if !known {
		return verbEntry{}, false
	}

	entry, named := verbs[verb]

	return entry, named
}

// CanonicalVerb returns the verb this build names for a spelling a caller sent,
// and false when no verb answers to that spelling.
//
// It folds case and supplies the core prefix, so SET_NOZZLE_TEMPERATURE
// answers as core.command.set_nozzle_temperature.
//
// It suggests from the same union KnownVerb reads, so every caller gets the
// same suggestion for the same spelling. A verb of another kind is suggested
// to somebody who misspells it against a printer id. The union is published,
// so the suggestion tells a caller nothing the vocabulary route would not. It
// never invents a verb.
//
// Cost is one case fold and one map lookup over a string the request already
// decoded. Matching by edit distance instead would walk the table once per
// request, over a string whose length the caller chooses.
func CanonicalVerb(spelling string) (string, bool) {
	union := table().union

	verb := strings.ToLower(spelling)
	if union[verb] {
		return verb, true
	}

	if prefixed := verbPrefix + verb; union[prefixed] {
		return prefixed, true
	}

	return "", false
}

// VerbActions returns every verb of one kind with the class it carries. A
// caller deciding what to publish or what to refuse reads this rather than
// keeping a list of its own, and the map is freshly built so no caller can edit
// the table the command path reads.
func VerbActions(kind DeviceKind) map[string]Action {
	verbs := table().byKind[kind.Or(DeviceKindPrinter)]

	out := make(map[string]Action, len(verbs))
	for verb, entry := range verbs {
		out[verb] = entry.class
	}

	return out
}

// RawGCodeVerbs returns the raw gcode verbs, in declaration order. A transport
// that has to refuse them before the device plane is reached reads this rather
// than keeping its own list.
func RawGCodeVerbs() []string { return []string{verbSendGCode, verbForceGCode} }

// ValidateVerb holds the spelling of a verb a grant may name: a capability key,
// bounded, and never empty.
func ValidateVerb(verb string) error {
	if verb == "" {
		return declaredGrantRefused.New("the verb is empty").With(paramName(verb))
	}

	if len(verb) > maxVerbLen {
		return declaredGrantRefused.New("the verb is longer than a stored grant carries").
			With(paramName(verb))
	}

	if !isCapabilityKey(verb) {
		return declaredGrantRefused.New(
			"a verb is dotted lowercase segments, as core.command.send_gcode is").
			With(paramName(verb))
	}

	return nil
}

// maxVerbLen bounds a stored verb. It is the width a qualified action name
// already carries in this vocabulary.
const maxVerbLen = 160

// isCapabilityKey holds the capability grammar: dotted segments of lowercase
// alphanumerics with '-' and '_' inside a segment. It is spelled here rather
// than imported because this package is the authorization graph and imports
// nothing of the printer plane; TestTheVerbGrammarMatchesTheCapabilityKeyOne
// holds the two copies together.
func isCapabilityKey(key string) bool {
	if key == "" {
		return false
	}

	for segment := range strings.SplitSeq(key, ".") {
		if segment == "" {
			return false
		}

		for i := range len(segment) {
			c := segment[i]

			alphanumeric := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
			if alphanumeric || ((c == '-' || c == '_') && i > 0 && i < len(segment)-1) {
				continue
			}

			return false
		}
	}

	return true
}

// verbPrefix is the namespace the generated half is spelled under. A key under
// another prefix belongs to whoever declared it.
const verbPrefix = "core.command."

// generatedTable builds the table the binary starts with: the generated half
// alone. It is called at start and again by ResetVerbTable, so it never shares
// a map with a table anybody is reading.
func generatedTable() *verbTable {
	printer := make(map[string]verbEntry, len(generatedVerbs))

	rows := make([]VerbRow, 0, len(generatedVerbs))

	for _, row := range generatedVerbs {
		entry := verbEntry{class: row.class, markings: map[Marking]Marker{}}
		for _, marking := range row.markings {
			entry.markings[marking] = MarkerHost
		}

		printer[row.verb] = entry
		rows = append(rows, VerbRow{
			Verb: row.verb, Class: entry.class, Markings: markingRows(entry),
		})
	}

	slices.SortFunc(rows, func(a, b VerbRow) int { return strings.Compare(a.Verb, b.Verb) })

	out := &verbTable{
		byKind: map[DeviceKind]map[string]verbEntry{
			DeviceKindPrinter: printer,
			// A camera is a device the directory holds and no verb reaches. It
			// is in the table with an empty verb set rather than absent, so a
			// verb sent to one is a kind mismatch and not an unknown kind.
			DeviceKindCamera: {},
		},
		union: make(map[string]bool, len(printer)),
		kinds: []KindVerbs{
			{Kind: DeviceKindPrinter, DisplayName: "Printer", Verbs: rows},
			{Kind: DeviceKindCamera, DisplayName: "Camera", Verbs: nil},
		},
	}

	for verb := range printer {
		out.union[verb] = true
	}

	return out
}

// markingRows renders one entry's markings for the inventory.
func markingRows(entry verbEntry) []MarkingRow {
	names := entry.markingNames()

	out := make([]MarkingRow, 0, len(names))
	for _, marking := range names {
		out = append(out, MarkingRow{Marking: marking, Marker: entry.markings[marking]})
	}

	return out
}
