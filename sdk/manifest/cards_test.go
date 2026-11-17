package manifest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// withCards wraps one cards list in the smallest manifest that carries it.
func withCards(cards string) string {
	return withUI(`{"entry":"ui/index.js","library":">=0.1.0 <1.0.0","contributes":{"cards":` + cards + `}}`)
}

// spoolCard is the card ADR-20261008-sww rule 16 describes, with one field of every
// kind, which is what a case that breaks one key starts from.
const spoolCard = `{
  "id": "spool",
  "title": "Spool",
  "description": "The spool on one printer, and how much is left.",
  "icon": "spool",
  "size": {"w": 4, "h": 3},
  "least": {"w": 2, "h": 2},
  "settings": [
    {"key": "device", "kind": "printer", "label": "Printer"},
    {"key": "fleet", "kind": "printers", "label": "Printers", "default": ["a"]},
    {"key": "tag", "kind": "tag", "label": "Tag"},
    {"key": "camera", "kind": "camera", "label": "Camera"},
    {"key": "unit", "kind": "choice", "label": "Unit",
     "options": [{"value": "g", "label": "Grams"}, {"value": "m", "label": "Metres"}]},
    {"key": "show", "kind": "choices", "label": "Show", "default": ["m"],
     "options": [{"value": "g", "label": "Grams"}, {"value": "m", "label": "Metres"}]},
    {"key": "warn", "kind": "number", "label": "Warn below", "min": 0, "max": 1000, "step": 10, "default": 100},
    {"key": "title_line", "kind": "text", "label": "Line", "max_bytes": 40},
    {"key": "note", "kind": "note", "label": "Note"},
    {"key": "alarm", "kind": "switch", "label": "Alarm", "default": true}
  ]
}`

func TestACardReadsBackWithEveryKindAndItsDefaults(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withCards("["+spoolCard+"]")), decode)
	if err != nil {
		t.Fatalf("the card ADR-20261008-sww prints was refused: %v", err)
	}

	cards := held.UI.Contributes.Cards
	if len(cards) != 1 {
		t.Fatalf("the cards read as %+v", cards)
	}

	card := cards[0]
	if card.ID != "spool" || card.Icon != "spool" || card.Size != (manifest.CardSize{W: 4, H: 3}) ||
		card.Least != (manifest.CardSize{W: 2, H: 2}) {
		t.Errorf("the card read as %+v", card)
	}

	defaults := map[string]any{}
	for _, setting := range card.Settings {
		defaults[setting.Key] = setting.Default
	}

	want := map[string]any{
		"device": "", "fleet": []string{"a"}, "tag": "", "camera": "", "unit": "g",
		"show": []string{"m"}, "warn": float64(100), "title_line": "", "note": "", "alarm": true,
	}
	if !reflect.DeepEqual(defaults, want) {
		t.Errorf("the defaults read as %#v, want %#v", defaults, want)
	}

	if card.Settings[7].MaxBytes != 40 || card.Settings[8].MaxBytes != manifest.DefaultNoteBytes {
		t.Errorf("the byte bounds read as %d and %d", card.Settings[7].MaxBytes, card.Settings[8].MaxBytes)
	}

	if step := card.Settings[6].Step; step == nil || *step != 10 {
		t.Errorf("the step read as %v", step)
	}
}

func TestANumberWithNoDefaultStartsAtItsLowerBound(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withCards(`[{"id":"a","title":"A","description":"A card.",
		"size":{"w":2,"h":2},"least":{"w":1,"h":1},
		"settings":[{"key":"n","kind":"number","label":"N","min":5},{"key":"m","kind":"number","label":"M"}]}]`)), decode)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}

	settings := held.UI.Contributes.Cards[0].Settings
	if settings[0].Default != float64(5) || settings[1].Default != float64(0) {
		t.Errorf("the defaults read as %v and %v", settings[0].Default, settings[1].Default)
	}
}

func TestEveryCardRuleRefusesWhatItGuards(t *testing.T) {
	t.Parallel()

	// card writes one card with the listed keys over a valid base.
	card := func(override string) string {
		base := `"id":"a","title":"A","description":"A card.","size":{"w":2,"h":2},"least":{"w":1,"h":1}`
		if override == "" {
			return withCards(`[{` + base + `}]`)
		}

		return withCards(`[{` + base + `,` + override + `}]`)
	}
	// field writes one card with one settings field.
	field := func(body string) string { return card(`"settings":[` + body + `]`) }

	for _, step := range []struct {
		name, document, wanted string
	}{
		{"an unknown card key", card(`"colour":"red"`), "colour"},
		{"an id that is not a label", withCards(`[{"id":"A b","title":"A","description":"A card.",` +
			`"size":{"w":2,"h":2},"least":{"w":1,"h":1}}]`), "label"},
		{"an id written twice", withCards(`[{"id":"a","title":"A","description":"A card.","size":{"w":2,"h":2},` +
			`"least":{"w":1,"h":1}},{"id":"a","title":"B","description":"B card.","size":{"w":2,"h":2},` +
			`"least":{"w":1,"h":1}}]`), "id"},
		{"no description", withCards(`[{"id":"a","title":"A","size":{"w":2,"h":2},"least":{"w":1,"h":1}}]`),
			"description"},
		{"a description over its bound", card(`"description":"` + strings.Repeat("d", 201) + `"`), "200"},
		{"an icon outside the list", card(`"icon":"rocket"`), "icons are"},
		{"no size", withCards(`[{"id":"a","title":"A","description":"A card.","least":{"w":1,"h":1}}]`), "size"},
		{"a width past the grid", card(`"size":{"w":13,"h":2}`), "12"},
		{"a height past the grid", card(`"size":{"w":2,"h":33}`), "32"},
		{"a size of nothing", card(`"size":{"w":0,"h":2}`), "columns"},
		{"an unknown size key", card(`"size":{"w":2,"h":2,"d":1}`), "d"},
		{"a least larger than the size", card(`"least":{"w":3,"h":1}`), "larger"},
		{"too many fields", card(`"settings":[` + strings.Repeat(`{"key":"k","kind":"switch","label":"K"},`, 16) +
			`{"key":"k","kind":"switch","label":"K"}]`), "16"},
		{"an unknown field key", field(`{"key":"k","kind":"switch","label":"K","colour":1}`), "colour"},
		{"a key that is not a name", field(`{"key":"Big","kind":"switch","label":"K"}`), "lower-case"},
		{"a key past its bound", field(`{"key":"` + strings.Repeat("k", 33) + `","kind":"switch","label":"K"}`),
			"lower-case"},
		{"a key starting with a digit", field(`{"key":"1k","kind":"switch","label":"K"}`), "lower-case"},
		{"a key written twice", field(`{"key":"k","kind":"switch","label":"K"},{"key":"k","kind":"tag","label":"T"}`),
			"key"},
		{"an unknown kind", field(`{"key":"k","kind":"colour","label":"K"}`), "kinds are"},
		{"no label", field(`{"key":"k","kind":"switch"}`), "label"},
		{"a choice with no options", field(`{"key":"k","kind":"choice","label":"K"}`), "options"},
		{"an option written twice", field(`{"key":"k","kind":"choice","label":"K",` +
			`"options":[{"value":"a","label":"A"},{"value":"a","label":"B"}]}`), "value"},
		{"options on a switch", field(`{"key":"k","kind":"switch","label":"K","options":[{"value":"a","label":"A"}]}`),
			"does not apply"},
		{"a bound on text", field(`{"key":"k","kind":"text","label":"K","min":1}`), "does not apply"},
		{"min over max", field(`{"key":"k","kind":"number","label":"K","min":5,"max":1}`), "larger"},
		{"a step of nothing", field(`{"key":"k","kind":"number","label":"K","step":0}`), "step"},
		{"text past its bound", field(`{"key":"k","kind":"text","label":"K","max_bytes":2001}`), "2000"},
		{"a note past its bound", field(`{"key":"k","kind":"note","label":"K","max_bytes":3001}`), "3000"},
		{"a default of the wrong kind", field(`{"key":"k","kind":"switch","label":"K","default":"on"}`), "true or false"},
		{"a choice default outside the options", field(`{"key":"k","kind":"choice","label":"K","default":"x",` +
			`"options":[{"value":"a","label":"A"}]}`), "not one of"},
		{"a choices default outside the options", field(`{"key":"k","kind":"choices","label":"K","default":["x"],` +
			`"options":[{"value":"a","label":"A"}]}`), "not one of"},
		{"a number default past its bound", field(`{"key":"k","kind":"number","label":"K","max":5,"default":6}`),
			"bounds"},
		{"a text default on two lines", field(`{"key":"k","kind":"text","label":"K","default":"a\nb"}`), "one line"},
		{"a text default past its bound", field(`{"key":"k","kind":"text","label":"K","max_bytes":2,"default":"abc"}`),
			"bytes"},
	} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()
			refuse(t, step.document, step.wanted)
		})
	}
}

// TestASettingKeyAtItsBoundIsRead holds the longest key the rule allows on the
// accepting side, so the bound is the documented 32 bytes and not one short.
func TestASettingKeyAtItsBoundIsRead(t *testing.T) {
	t.Parallel()

	key := strings.Repeat("k", manifest.MaxCardSettingKeyBytes)

	held, err := manifest.Parse("module.yaml", []byte(withCards(`[{"id":"a","title":"A","description":"A card.",`+
		`"size":{"w":2,"h":2},"least":{"w":1,"h":1},"settings":[{"key":"`+key+`","kind":"switch","label":"K"}]}]`)),
		decode)
	if err != nil {
		t.Fatalf("a key of %d bytes was refused: %v", len(key), err)
	}

	if got := held.UI.Contributes.Cards[0].Settings[0].Key; got != key {
		t.Errorf("the key read back as %q", got)
	}
}
