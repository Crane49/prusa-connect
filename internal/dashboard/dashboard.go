//go:build !nodashboards

// Package dashboard holds what a dashboard is and the rules it is held to.
//
// A dashboard is a page a person lays out from cards. This package holds the
// shape and the rules. The composition root decides who may see a dashboard,
// against the authorization graph. The store decides where one is kept, in
// the host's database. Both use the types here, so every surface applies the
// same rules.
//
// A card's settings belong to its card type, which is code in a browser bundle
// the server never runs. So the server holds the settings to being an object
// of a bounded size and never reads a field. A new kind of card is then a
// change to a bundle alone.
package dashboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The limits on a dashboard, on its cards and on the text a person writes.
const (
	// MaxDashboards is how many dashboards one server keeps.
	MaxDashboards = 200
	// MaxCards is how many cards one dashboard holds.
	MaxCards = 64
	// MaxNameBytes bounds the name of a dashboard.
	MaxNameBytes = 80
	// MaxTitleBytes bounds the heading over a card.
	MaxTitleBytes = 120
	// MaxConfigBytes bounds a card's settings, written out with no spaces.
	MaxConfigBytes = 4096
	// MaxCardsBytes bounds every card of a dashboard written out, which is the
	// size of one record in the record store.
	MaxCardsBytes = 64 << 10
	// MinColumns and MaxColumns bound the width of the grid.
	MinColumns = 4
	MaxColumns = 24
	// DefaultColumns is the grid a dashboard gets when the draft names none.
	DefaultColumns = 12
	// MaxCardRows is the tallest a card may be. A module's card declares its
	// size against the same bound, so it is the manifest's number.
	MaxCardRows = manifest.MaxCardRows
	// MaxRows is the last row a card may reach.
	MaxRows = 512
)

// Visibility says who sees a dashboard.
type Visibility string

// The three visibilities, in the order of the actions they take on the kind.
const (
	VisibleToEveryone  Visibility = "everyone"
	VisibleToOperators Visibility = "operators"
	VisibleToAdmins    Visibility = "admins"
)

// Level is the visibility as a number the store compares in its query: 0 for
// everyone, 1 for operators, 2 for administrators. An unknown visibility
// answers -1.
func (v Visibility) Level() int {
	return slices.Index(visibilities, v)
}

// VisibilityOf is the visibility a level spells, everyone for a level below
// the first.
func VisibilityOf(level int) Visibility {
	return visibilities[min(max(level, 0), len(visibilities)-1)]
}

// visibilities is the three in the order of their levels.
var visibilities = []Visibility{VisibleToEveryone, VisibleToOperators, VisibleToAdmins}

// Density is how tall one row of the grid is.
type Density string

// The three densities.
const (
	DensityCompact     Density = "compact"
	DensityComfortable Density = "comfortable"
	DensitySpacious    Density = "spacious"
)

// Card is one tile of a dashboard: what it shows, where it sits, and its own
// settings.
type Card struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	X      int             `json:"x"`
	Y      int             `json:"y"`
	W      int             `json:"w"`
	H      int             `json:"h"`
	Title  string          `json:"title"`
	Config json.RawMessage `json:"config"`
}

// Dashboard is one page of cards, as the store keeps it.
type Dashboard struct {
	ID         int64
	Name       string
	Visibility Visibility
	IsDefault  bool
	Columns    int
	Density    Density
	Cards      []Card
	Revision   int64
	Created    time.Time
	CreatedBy  string
	Changed    time.Time
	ChangedBy  string
}

// Row is one dashboard as a listing shows it: no cards, only how many.
type Row struct {
	ID         int64
	Name       string
	Visibility Visibility
	IsDefault  bool
	Cards      int
	Changed    time.Time
	ChangedBy  string
}

// Draft is what a person sends to make or to change a dashboard. A member left
// out keeps what the dashboard had, or takes the default for a new one.
type Draft struct {
	Name       string
	Visibility *Visibility
	Columns    *int
	Density    *Density
	Cards      *[]Card
}

// InvalidError is a member that breaks a rule. Location names the member, such as
// body.cards[2].w, and Reason says which rule and its limit, in words a person
// can act on.
type InvalidError struct {
	Location string
	Reason   string
}

func (e *InvalidError) Error() string { return e.Location + ": " + e.Reason }

// The conditions a store or a check reports.
var (
	// ErrNotFound reports a dashboard that is not there, or that the caller may
	// not see. The two are one answer, so a caller learns nothing about a
	// dashboard it may not see.
	ErrNotFound = errors.New("dashboard: no such dashboard")
	// ErrFull reports a new dashboard past MaxDashboards.
	ErrFull = errors.New("dashboard: the server keeps no more dashboards")
)

// ChangedError reports a change made against an older revision. Revision is the
// revision the dashboard is at now.
type ChangedError struct {
	Revision int64
}

func (e *ChangedError) Error() string {
	return fmt.Sprintf("dashboard: saved at revision %d after the change was read", e.Revision)
}

// The grammar of a card id and of a card type. A type is a namespace, a slash
// and a name: core/printer, or com.github.aux.acme.spools/spool for a module's
// card.
var (
	cardIDPattern   = regexp.MustCompile(`^[a-z0-9]{1,16}$`)
	cardTypePattern = regexp.MustCompile(
		`^[a-z0-9]([a-z0-9.-]{0,126}[a-z0-9])?/[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
)

// New is a dashboard made from a draft, with the default for every member the
// draft leaves out. It refuses a draft that breaks a rule.
func New(draft Draft) (Dashboard, error) {
	base := Dashboard{
		Visibility: VisibleToEveryone,
		Columns:    DefaultColumns,
		Density:    DensityComfortable,
		Cards:      []Card{},
	}

	return Apply(base, draft)
}

// Apply is the dashboard with the draft written over it. A member the draft
// leaves out keeps what the dashboard had. It refuses a draft that breaks a
// rule, and leaves the dashboard it was given as it was.
func Apply(base Dashboard, draft Draft) (Dashboard, error) {
	next := base

	name, err := line("body.name", draft.Name, MaxNameBytes, true)
	if err != nil {
		return Dashboard{}, err
	}

	next.Name = name

	if draft.Visibility != nil {
		if draft.Visibility.Level() < 0 {
			return Dashboard{}, &InvalidError{"body.visibility", "is everyone, operators or admins"}
		}

		next.Visibility = *draft.Visibility
	}

	if draft.Columns != nil {
		if *draft.Columns < MinColumns || *draft.Columns > MaxColumns {
			return Dashboard{}, &InvalidError{"body.columns",
				fmt.Sprintf("is between %d and %d", MinColumns, MaxColumns)}
		}

		next.Columns = *draft.Columns
	}

	if draft.Density != nil {
		switch *draft.Density {
		case DensityCompact, DensityComfortable, DensitySpacious:
			next.Density = *draft.Density
		default:
			return Dashboard{}, &InvalidError{"body.density", "is compact, comfortable or spacious"}
		}
	}

	cards := next.Cards
	if draft.Cards != nil {
		cards = *draft.Cards
	}

	if next.Cards, err = checkCards(cards, next.Columns); err != nil {
		return Dashboard{}, err
	}

	return next, nil
}

// checkCards holds every card to its rules and answers them with their words
// trimmed and their settings written out with no spaces.
func checkCards(cards []Card, columns int) ([]Card, error) {
	if len(cards) > MaxCards {
		return nil, &InvalidError{"body.cards", fmt.Sprintf("holds at most %d cards", MaxCards)}
	}

	out := make([]Card, 0, len(cards))
	seen := make(map[string]bool, len(cards))

	for index, card := range cards {
		at := fmt.Sprintf("body.cards[%d]", index)

		checked, err := checkCard(at, card, columns)
		if err != nil {
			return nil, err
		}

		if seen[checked.ID] {
			return nil, &InvalidError{at + ".id", "is given to two cards of one dashboard"}
		}

		if slices.ContainsFunc(out, func(other Card) bool { return overlaps(other, checked) }) {
			return nil, &InvalidError{at, "covers cells another card covers"}
		}

		seen[checked.ID] = true
		out = append(out, checked)
	}

	// The cards as the store keeps them: the size is the record's, measured
	// on the JSON itself and not on the fields inside it.
	written, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("write the cards: %w", err)
	}

	if len(written) > MaxCardsBytes {
		return nil, &InvalidError{"body.cards", fmt.Sprintf("are at most %d bytes written out", MaxCardsBytes)}
	}

	return out, nil
}

func checkCard(at string, card Card, columns int) (Card, error) {
	if !cardIDPattern.MatchString(card.ID) {
		return Card{}, &InvalidError{at + ".id", "is 1 to 16 lower-case letters and digits"}
	}

	if !cardTypePattern.MatchString(card.Type) {
		return Card{}, &InvalidError{at + ".type", "is a namespace, a slash and a name, such as core/printer"}
	}

	if err := checkPlace(at, card, columns); err != nil {
		return Card{}, err
	}

	title, err := line(at+".title", card.Title, MaxTitleBytes, false)
	if err != nil {
		return Card{}, err
	}

	config, err := checkConfig(at+".config", card.Config)
	if err != nil {
		return Card{}, err
	}

	card.Title = title
	card.Config = config

	return card, nil
}

func checkPlace(at string, card Card, columns int) error {
	switch {
	case card.X < 0 || card.Y < 0:
		return &InvalidError{at, "starts at column 0 and row 0 or later"}
	case card.W < 1 || card.H < 1:
		return &InvalidError{at, "covers at least one column and one row"}
	// Neither end is added to its start: X and Y come off the wire, and a
	// start near the largest int would wrap past the check.
	case card.X > columns-card.W:
		return &InvalidError{at, fmt.Sprintf("ends at or before the last of %d columns", columns)}
	case card.H > MaxCardRows:
		return &InvalidError{at + ".h", fmt.Sprintf("is at most %d rows", MaxCardRows)}
	case card.Y > MaxRows-card.H:
		return &InvalidError{at, fmt.Sprintf("ends at or before row %d", MaxRows)}
	}

	return nil
}

// overlaps reports whether two cards, already held to the grid, share a cell.
func overlaps(a, b Card) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// checkConfig holds a card's settings to being one JSON object of a bounded
// size, and writes it out with no spaces. Absent or null is an empty object.
// A NUL character in a key or a string is refused: Postgres cannot store one
// in JSONB.
func checkConfig(at string, config json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(config)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage("{}"), nil
	}

	if trimmed[0] != '{' {
		return nil, &InvalidError{at, "is a JSON object"}
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, &InvalidError{at, "is a JSON object"}
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(compact.Bytes(), &object); err != nil {
		return nil, &InvalidError{at, "is a JSON object"}
	}

	if compact.Len() > MaxConfigBytes {
		return nil, &InvalidError{at, fmt.Sprintf("is at most %d bytes written out", MaxConfigBytes)}
	}

	if holdsNUL(compact.Bytes()) {
		return nil, &InvalidError{at, "holds no NUL character"}
	}

	return json.RawMessage(compact.Bytes()), nil
}

// holdsNUL reports whether a key or a string of valid JSON decodes to a text
// with a NUL character in it, however the JSON escapes it.
func holdsNUL(document []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(document))

	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}

		if text, ok := token.(string); ok && strings.ContainsRune(text, 0) {
			return true
		}
	}
}

// line holds one line of words a person wrote: trimmed, at most limit bytes,
// and free of a control character and of an invisible formatting character,
// which would let one name read as another in the list everybody shares.
func line(at, value string, limit int, required bool) (string, error) {
	held := strings.TrimSpace(value)

	switch {
	case !utf8.ValidString(held):
		return "", &InvalidError{at, "is valid UTF-8"}
	case required && held == "":
		return "", &InvalidError{at, "is not empty"}
	case len(held) > limit:
		return "", &InvalidError{at, fmt.Sprintf("is at most %d bytes", limit)}
	case strings.ContainsFunc(held, unseen):
		return "", &InvalidError{at, "holds no line break, control character or invisible formatting character"}
	}

	return held, nil
}

// unseen reports a character a person cannot see: a control character, a
// format character such as the right-to-left override, and the line and
// paragraph separators.
func unseen(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029'
}
