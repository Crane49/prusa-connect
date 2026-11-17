package app

// The name an operator gives a device. What a person calls the machine is part
// of the device's identity.
//
// A tag is not a name. A device carries many tags, and nothing in a list of
// tags says which of them a screen puts at the top of a card. The name is the
// one value a screen shows instead of the model and the serial.
//
// Nothing here reaches the authorization graph with a name and nothing in the
// graph reads one. The name narrows a listing through the search filter and can
// never widen one, exactly as a tag cannot.

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// maxDeviceNameRunes bounds one name. It is counted in characters and not in
// bytes, because a person naming a printer in Czech or in Japanese would
// otherwise get a shorter name than a person naming one in English.
//
// The limit is the same 96 the derived display name is clipped to, so a name a
// person typed and a name this server composed are the same size on a screen.
const maxDeviceNameRunes = printerNameLimit

// SetName writes what an operator calls one device and returns what it now
// carries. An empty name takes the operator's name off, and the device reads
// under the model and the serial it reported again.
//
// It takes administer on the device, which is what SetTags takes and for the same
// reason: naming a machine is an administrative fact about it rather than
// something the machine did, so it sits above the operator who runs prints on
// it.
func (d *devices) SetName(ctx context.Context, principal string, id string, name string) (string, error) {
	trimmed, err := validDeviceName(name)
	if err != nil {
		return "", err
	}

	entry, _, err := d.reachHolding(ctx, principal, id, authz.ActionAdminister)
	if err != nil {
		return "", err
	}

	held := entry.setName(trimmed)

	// Immediately rather than on the next tick, for the reason a tag is
	// written immediately: a person typed it, and the frame that reports it
	// belongs to the same moment as an arrival or a departure.
	d.writeSoon(entry)

	if held != "" {
		return held, nil
	}

	// A device nobody names reads under what the machine said it is. The answer
	// is what a listing then shows, so a client that cleared the name renders
	// the same string the next read gives it.
	return entry.displayedName(""), nil
}

// setName writes the name onto the entry and marks the record behind it.
func (e *device) setName(name string) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.name = name
	e.changed = true

	return e.name
}

// displayedName is what a screen shows for this device: the operator's name
// where there is one, the directory's display name next, and what the machine
// said it is where the directory holds nothing.
//
// directory is the name the graph's own row carries, which register writes from
// the reported model and serial on every arrival.
func (e *device) displayedName(directory string) string {
	e.mu.Lock()
	named := e.name
	e.mu.Unlock()

	if named != "" {
		return named
	}

	if directory != "" {
		return directory
	}

	return displayName(e.id, e.identity)
}

// validDeviceName holds one name to its bounds before anything is written.
//
// There are three rules. The name is trimmed, because trailing space is
// invisible and a person cannot see why two names differ. It is bounded in
// characters. And it carries no control character, because a name is written
// into an event payload, a log line and a page, and a newline in one forges a
// frame in the first two.
func validDeviceName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)

	if utf8.RuneCountInString(trimmed) > maxDeviceNameRunes {
		return "", httpfault.RequestInvalid("body.name",
			"at most 96 characters",
			"the request carries a longer name")
	}

	if !utf8.ValidString(trimmed) {
		return "", httpfault.RequestInvalid("body.name",
			"valid UTF-8", "the request carries bytes that are not")
	}

	for _, char := range trimmed {
		if char < ' ' || char == 0x7f {
			return "", httpfault.RequestInvalid("body.name",
				"no control characters",
				"the request carries one")
		}
	}

	return trimmed, nil
}

// operatorFacts is what a person wrote about this machine on the entry a new
// activation replaces: the name and the tags.
//
// Both are carried across because both are a person's and not a machine's. A
// device coming back is the same device, and a name that lasted until the
// printer next reconnected would be a name nobody could rely on. Everything
// else on a new entry comes from the handshake, which is what the machine is
// saying about itself now.
//
// It reads the live table and the departed one, because a machine that comes
// back after its session ended is in the second.
func (d *devices) operatorFacts(id string) (name string, tags []string) {
	entry, seen := d.known(id)
	if !seen {
		return "", nil
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	return entry.name, slices.Clone(entry.tags)
}
