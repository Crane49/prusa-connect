//go:build !nodashboards

package dashboard_test

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/dashboard"
)

func card(id string, x, y, w, h int, config string) dashboard.Card {
	return dashboard.Card{ID: id, Type: "core/note", X: x, Y: y, W: w, H: h, Config: json.RawMessage(config)}
}

func cards(list ...dashboard.Card) *[]dashboard.Card { return &list }

func TestANewDashboardTakesTheDefaults(t *testing.T) {
	t.Parallel()

	board, err := dashboard.New(dashboard.Draft{Name: "  Farm  "})
	if err != nil {
		t.Fatal(err)
	}

	if board.Name != "Farm" || board.Columns != 12 || board.Density != dashboard.DensityComfortable ||
		board.Visibility != dashboard.VisibleToEveryone || board.Cards == nil || len(board.Cards) != 0 {
		t.Errorf("the defaults are %+v", board)
	}
}

func TestEveryRuleRefusesWhatItGuardsAndTakesItsLimit(t *testing.T) {
	t.Parallel()

	columns := func(n int) *int { return &n }
	visible := func(v dashboard.Visibility) *dashboard.Visibility { return &v }
	density := func(d dashboard.Density) *dashboard.Density { return &d }
	many := func(n int) *[]dashboard.Card {
		out := make([]dashboard.Card, n)
		for i := range out {
			out[i] = card("t"+strconv.Itoa(i), 0, i, 1, 1, "")
		}

		return &out
	}

	for _, step := range []struct {
		name  string
		draft dashboard.Draft
		at    string
	}{
		{"a name at its limit", dashboard.Draft{Name: strings.Repeat("n", dashboard.MaxNameBytes)}, ""},
		{"an empty name", dashboard.Draft{Name: "   "}, "body.name"},
		{"a name over its limit", dashboard.Draft{Name: strings.Repeat("n", dashboard.MaxNameBytes+1)}, "body.name"},
		{"a name on two lines", dashboard.Draft{Name: "a\nb"}, "body.name"},
		{"a name that turns its text around", dashboard.Draft{Name: "invoice\u202Efdp.exe"}, "body.name"},
		{"a name with a hidden space", dashboard.Draft{Name: "Farm\u200B"}, "body.name"},
		{"a name with a paragraph break", dashboard.Draft{Name: "Farm\u2029Wall"}, "body.name"},
		{"a name that is not UTF-8", dashboard.Draft{Name: "\xff"}, "body.name"},
		{"a name in another script", dashboard.Draft{Name: "Dílna ž 工房"}, ""},
		{"an unknown visibility", dashboard.Draft{Name: "x", Visibility: visible("friends")}, "body.visibility"},
		{"the admins visibility", dashboard.Draft{Name: "x", Visibility: visible(dashboard.VisibleToAdmins)}, ""},
		{"too few columns", dashboard.Draft{Name: "x", Columns: columns(dashboard.MinColumns - 1)}, "body.columns"},
		{"too many columns", dashboard.Draft{Name: "x", Columns: columns(dashboard.MaxColumns + 1)}, "body.columns"},
		{"the most columns", dashboard.Draft{Name: "x", Columns: columns(dashboard.MaxColumns)}, ""},
		{"an unknown density", dashboard.Draft{Name: "x", Density: density("cosy")}, "body.density"},
		{"the most cards", dashboard.Draft{Name: "x", Cards: many(dashboard.MaxCards)}, ""},
		{"one card too many", dashboard.Draft{Name: "x", Cards: many(dashboard.MaxCards + 1)}, "body.cards"},
		{"a card id with an upper-case letter",
			dashboard.Draft{Name: "x", Cards: cards(card("A1", 0, 0, 1, 1, ""))}, "body.cards[0].id"},
		{"two cards with one id",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, 0, 1, 1, ""), card("a", 1, 0, 1, 1, ""))},
			"body.cards[1].id"},
		{"two cards that share a cell", dashboard.Draft{Name: "x", Cards: cards(
			card("a", 0, 0, 4, 2, ""), card("b", 1, 1, 4, 2, ""))}, "body.cards[1]"},
		{"two cards side by side and one under them", dashboard.Draft{Name: "x", Cards: cards(
			card("a", 0, 0, 4, 2, ""), card("b", 4, 0, 4, 2, ""), card("c", 0, 2, 8, 1, ""))}, ""},
		{"settings with a NUL in a string", dashboard.Draft{Name: "x", Cards: cards(
			card("a", 0, 0, 1, 1, `{"k":"a\u0000b"}`))}, "body.cards[0].config"},
		{"settings with a NUL in a key", dashboard.Draft{Name: "x", Cards: cards(
			card("a", 0, 0, 1, 1, `{"a\u0000":{"n":[1]}}`))}, "body.cards[0].config"},
		{"settings with a backslash and the text u0000", dashboard.Draft{Name: "x", Cards: cards(
			card("a", 0, 0, 1, 1, `{"k":"a\\u0000"}`))}, ""},
		{"a card type with no namespace", dashboard.Draft{Name: "x", Cards: cards(dashboard.Card{
			ID: "a", Type: "printer", W: 1, H: 1})}, "body.cards[0].type"},
		{"a module's card type", dashboard.Draft{Name: "x", Cards: cards(dashboard.Card{
			ID: "a", Type: "com.github.aux.acme.spools/spool", W: 1, H: 1})}, ""},
		{"a card past the last column",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 10, 0, 3, 1, ""))}, "body.cards[0]"},
		{"a card that covers no cell",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, 0, 0, 1, ""))}, "body.cards[0]"},
		{"a card at a negative row",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, -1, 1, 1, ""))}, "body.cards[0]"},
		{"a card taller than its limit",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, 0, 1, dashboard.MaxCardRows+1, ""))},
			"body.cards[0].h"},
		{"a card past the last row",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, dashboard.MaxRows, 1, 1, ""))}, "body.cards[0]"},
		{"a card starting at the largest column",
			dashboard.Draft{Name: "x", Cards: cards(card("a", math.MaxInt, 0, 1, 1, ""))}, "body.cards[0]"},
		{"a card starting at the largest row",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, math.MaxInt, 1, 1, ""))}, "body.cards[0]"},
		{"a card wider than any grid",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, 0, math.MaxInt, 1, ""))}, "body.cards[0]"},
		{"a card at the last cell", dashboard.Draft{Name: "x", Cards: cards(
			card("a", dashboard.DefaultColumns-1, dashboard.MaxRows-1, 1, 1, ""))}, ""},
		{"settings that are a list",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, 0, 1, 1, "[1]"))}, "body.cards[0].config"},
		{"settings that are not JSON",
			dashboard.Draft{Name: "x", Cards: cards(card("a", 0, 0, 1, 1, "{nope"))}, "body.cards[0].config"},
		{"settings over their limit", dashboard.Draft{Name: "x", Cards: cards(card("a", 0, 0, 1, 1,
			`{"text":"`+strings.Repeat("a", dashboard.MaxConfigBytes)+`"}`))}, "body.cards[0].config"},
		{"a card title on two lines", dashboard.Draft{Name: "x", Cards: cards(dashboard.Card{
			ID: "a", Type: "core/note", W: 1, H: 1, Title: "a\nb"})}, "body.cards[0].title"},
	} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()

			_, err := dashboard.New(step.draft)
			if step.at == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}

				return
			}

			var invalid *dashboard.InvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("answered %v, not a refusal at %s", err, step.at)
			}

			if invalid.Location != step.at {
				t.Errorf("refused at %s, not at %s", invalid.Location, step.at)
			}
		})
	}
}

func TestSettingsAreKeptWrittenOutWithNoSpaces(t *testing.T) {
	t.Parallel()

	board, err := dashboard.New(dashboard.Draft{Name: "x", Cards: cards(
		card("a", 0, 0, 1, 1, ` { "device" : "mk4" } `),
		card("b", 1, 0, 1, 1, `null`),
		card("c", 2, 0, 1, 1, ``),
	)})
	if err != nil {
		t.Fatal(err)
	}

	for i, want := range []string{`{"device":"mk4"}`, `{}`, `{}`} {
		if got := string(board.Cards[i].Config); got != want {
			t.Errorf("card %d keeps %s, not %s", i, got, want)
		}
	}
}

func TestADraftKeepsWhatItLeavesOut(t *testing.T) {
	t.Parallel()

	base, err := dashboard.New(dashboard.Draft{
		Name: "Farm", Cards: cards(card("a", 0, 0, 2, 2, "")),
	})
	if err != nil {
		t.Fatal(err)
	}

	next, err := dashboard.Apply(base, dashboard.Draft{Name: "Wall"})
	if err != nil {
		t.Fatal(err)
	}

	if len(next.Cards) != 1 || next.Name != "Wall" {
		t.Errorf("the change kept %+v", next)
	}

	emptied, err := dashboard.Apply(base, dashboard.Draft{Name: "Wall", Cards: cards()})
	if err != nil {
		t.Fatal(err)
	}

	if len(emptied.Cards) != 0 {
		t.Errorf("an empty list of cards kept %d", len(emptied.Cards))
	}

	// A narrower grid holds the cards that are on it to the new width.
	four := 4
	if _, err = dashboard.Apply(base, dashboard.Draft{Name: "Wall", Columns: &four}); err != nil {
		t.Errorf("a card two columns wide was refused on four: %v", err)
	}
}

func TestTheVisibilityIsALevel(t *testing.T) {
	t.Parallel()

	for level, visible := range []dashboard.Visibility{
		dashboard.VisibleToEveryone, dashboard.VisibleToOperators, dashboard.VisibleToAdmins,
	} {
		if visible.Level() != level || dashboard.VisibilityOf(level) != visible {
			t.Errorf("%s is level %d", visible, visible.Level())
		}
	}

	if dashboard.Visibility("friends").Level() != -1 {
		t.Error("an unknown visibility has a level")
	}
}

// TestTheCardsAreMeasuredAsTheyAreWrittenOut holds the size limit to the JSON
// the store keeps, members and punctuation included, and not to the fields
// inside it: 64 cards whose fields fit can still be a record that does not.
func TestTheCardsAreMeasuredAsTheyAreWrittenOut(t *testing.T) {
	t.Parallel()

	settings := `{"t":"` + strings.Repeat("a", 978) + `"}`

	many := make([]dashboard.Card, dashboard.MaxCards)

	fields := 0

	for i := range many {
		many[i] = card("c"+strconv.Itoa(i), 0, i, 1, 1, settings)
		fields += len(many[i].ID) + len(many[i].Type) + len(settings)
	}

	require.Less(t, fields, dashboard.MaxCardsBytes, "the fields alone fit, which is the case")

	written, err := json.Marshal(many)
	require.NoError(t, err)
	require.Greater(t, len(written), dashboard.MaxCardsBytes, "the record does not, which is the case")

	_, err = dashboard.New(dashboard.Draft{Name: "x", Cards: &many})

	var invalid *dashboard.InvalidError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "body.cards", invalid.Location)
}
