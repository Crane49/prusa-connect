package manifest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// withUI wraps one ui block in the smallest manifest that carries it, so a
// case says what it is testing and nothing else.
func withUI(block string) string {
	return `{"name":"spools","kind":"module","license":"MIT","ui":` + block + `}`
}

// wholeUI is the block ADR-20260911-4p6 rule 13 prints, which is what a case that
// changes one key starts from.
const wholeUI = `{
  "entry": "ui/index.js",
  "library": ">=0.3.0 <1.0.0",
  "contributes": {
    "pages": [{"path": "spools", "title": "Spools", "icon": "layers", "hidden": false}],
    "device_section": [{"id": "spool-status", "title": "Loaded spool"}],
    "settings": [{"id": "spools", "title": "Spools"}]
  }
}`

func TestTheUIBlockOfTheRecordReadsBackWholly(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withUI(wholeUI)), decode)
	if err != nil {
		t.Fatalf("the block the record prints was refused: %v", err)
	}

	ui := held.UI
	if ui == nil {
		t.Fatal("the block was dropped")
	}

	if ui.Entry != "ui/index.js" || ui.Library != ">=0.3.0 <1.0.0" {
		t.Errorf("the entry and the library read as %q and %q", ui.Entry, ui.Library)
	}

	if len(ui.Contributes.Pages) != 1 {
		t.Fatalf("the pages read as %+v", ui.Contributes.Pages)
	}

	page := ui.Contributes.Pages[0]
	if page.Path != "spools" || page.Title != "Spools" || page.Icon != "layers" || page.Hidden {
		t.Errorf("the page read as %+v", page)
	}

	deviceSection := ui.Contributes.Sections[manifest.SlotDeviceSection]
	if len(deviceSection) != 1 || deviceSection[0].ID != "spool-status" {
		t.Errorf("the device section sections read as %+v", deviceSection)
	}

	settings := ui.Contributes.Sections[manifest.SlotSettings]
	if len(settings) != 1 || settings[0].Title != "Spools" {
		t.Errorf("the settings panes read as %+v", settings)
	}
}

// TestASectionListLandsUnderItsSlot proves the parser walks the table: a
// section list is held under the slot whose key it is.
func TestASectionListLandsUnderItsSlot(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withUI(wholeUI)), decode)
	if err != nil {
		t.Fatalf("the block the record prints was refused: %v", err)
	}

	got := held.UI.Contributes.Sections[manifest.SlotDeviceSection]
	want := []manifest.Section{{ID: "spool-status", Title: "Loaded spool"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the device section landed as %+v, wanted %+v", got, want)
	}
}

// TestAnIconOutsideTheListIsRefused keeps the manifest to the icons the app
// ships, and names them so the author can pick one.
func TestAnIconOutsideTheListIsRefused(t *testing.T) {
	t.Parallel()

	refuse(t, withUI(`{"entry":"ui/index.js","library":">=0.1.0 <1.0.0",
		"contributes":{"pages":[{"path":"shelf","title":"Shelf","icon":"rocket"}]}}`),
		`ui.contributes.pages[0].icon is "rocket"`)
}

// TestTheSlotRefusalNamesEveryList keeps the refusal text in step with the
// table.
func TestTheSlotRefusalNamesEveryList(t *testing.T) {
	t.Parallel()

	_, err := manifest.Parse("module.yaml", []byte(withUI(`{"entry":"ui/index.js",
		"library":">=0.1.0 <1.0.0","contributes":{"pages":[{"path":"a","title":"A","slot":"x"}]}}`)), decode)
	if err == nil {
		t.Fatal("a slot named by hand was accepted")
	}

	for _, slot := range manifest.Slots() {
		if !strings.Contains(err.Error(), slot.Name.Key()) {
			t.Errorf("the refusal %q does not name the %s list", err.Error(), slot.Name.Key())
		}
	}
}

func TestAHiddenPageIsRead(t *testing.T) {
	t.Parallel()

	block := `{"entry":"index.js","library":">=0.3.0 <1.0.0",
		"contributes":{"pages":[{"path":"spools","title":"Spools","hidden":true}]}}`

	held, err := manifest.Parse("module.yaml", []byte(withUI(block)), decode)
	if err != nil {
		t.Fatalf("a hidden page was refused: %v", err)
	}

	if !held.UI.Contributes.Pages[0].Hidden {
		t.Error("hidden read as false")
	}
}

func TestAUIBlockWithNoContributionsIsRead(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml",
		[]byte(withUI(`{"entry":"index.js","library":">=0.3.0 <1.0.0"}`)), decode)
	if err != nil {
		t.Fatalf("a block that fills no slot was refused: %v", err)
	}

	if len(held.UI.Contributes.Pages) != 0 {
		t.Errorf("pages read as %+v", held.UI.Contributes.Pages)
	}
}

// TestTheEntryKeyIsValidated holds ADR-20260911-4p6 rule 14's first sentence: the path
// is relative and cannot climb out of the bundle root, and the app imports it.
func TestTheEntryKeyIsValidated(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		entry   string
		refuses string
	}{
		"absent":         {"", "entry is empty"},
		"blank":          {"   ", "entry is empty"},
		"leading slash":  {"/ui/index.js", "starts with no slash"},
		"climbs once":    {"../index.js", "climb out of it"},
		"climbs inside":  {"ui/../../index.js", "climb out of it"},
		"climbs at end":  {"ui/..", "climb out of it"},
		"backslash":      {`ui\index.js`, "forward slashes"},
		"a dot element":  {"./index.js", "does not name a file"},
		"a trailing sep": {"ui/", "does not name a file"},
		"not javascript": {"ui/index.html", "ends in .js"},
		"no suffix":      {"ui/index", "ends in .js"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			block := `{"entry":"` + strings.ReplaceAll(testCase.entry, `\`, `\\`) +
				`","library":">=0.3.0 <1.0.0"}`
			if name == "absent" {
				block = `{"library":">=0.3.0 <1.0.0"}`
			}

			refuse(t, withUI(block), testCase.refuses)
		})
	}
}

// TestTheLibraryKeyIsValidated holds the range to the grammar ParseRange
// reads, through the manifest rather than through the parser. The forms npm
// names are accepted; what is refused here is what no version satisfies and
// what the grammar does not name at all.
func TestTheLibraryKeyIsValidated(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		library string
		refuses string
	}{
		"absent":                     {"", "library is empty"},
		"an empty range":             {">=1.0.0 <1.0.0", "holds no version"},
		"bounds the wrong way round": {">=2.0.0 <1.0.0", "holds no version"},
		"a version with four fields": {"1.2.3.4", "at most 3"},
		"an operator on a wildcard":  {">=*", "wildcard"},
		"a wildcard before a number": {"1.x.3", "after a wildcard"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			block := `{"entry":"index.js","library":"` + testCase.library + `"}`
			if name == "absent" {
				block = `{"entry":"index.js"}`
			}

			refuse(t, withUI(block), testCase.refuses)
		})
	}
}

// TestEveryPathAndIDIsOneLabel holds ADR-20260911-4p6 rule 14's second sentence: a
// path and an id are one label with no slash.
func TestEveryPathAndIDIsOneLabel(t *testing.T) {
	t.Parallel()

	names := map[string]string{
		"a slash":         "spools/loaded",
		"an upper case":   "Spools",
		"a leading dash":  "-spools",
		"a trailing dash": "spools-",
		"an underscore":   "spool_status",
		"a dot":           "spools.v2",
		"a space":         "loaded spool",
		"too long":        strings.Repeat("s", 64),
	}

	for name, value := range names {
		t.Run("page path with "+name, func(t *testing.T) {
			t.Parallel()

			refuse(t, withUI(`{"entry":"index.js","library":">=0.3.0 <1.0.0","contributes":{"pages":[`+
				`{"path":"`+value+`","title":"Spools"}]}}`), "pages[0].path")
		})

		t.Run("device card id with "+name, func(t *testing.T) {
			t.Parallel()

			refuse(t, withUI(`{"entry":"index.js","library":">=0.3.0 <1.0.0","contributes":{"device_card":[`+
				`{"id":"`+value+`","title":"Loaded spool"}]}}`), "device_card[0].id")
		})

		t.Run("settings id with "+name, func(t *testing.T) {
			t.Parallel()

			refuse(t, withUI(`{"entry":"index.js","library":">=0.3.0 <1.0.0","contributes":{"settings":[`+
				`{"id":"`+value+`","title":"Spools"}]}}`), "settings[0].id")
		})
	}
}

// TestAnActionListLandsUnderItsSlot proves the parser reads a FormAction
// slot's list the same way a section list is read: an action entry lands
// under the slot whose key it is, with the fields a run button carries.
func TestAnActionListLandsUnderItsSlot(t *testing.T) {
	t.Parallel()

	block := `{"entry":"index.js","library":">=0.3.0 <1.0.0",
		"contributes":{"device_action":[{"id":"flush","title":"Flush","icon":"wrench","destructive":true}]}}`

	held, err := manifest.Parse("module.yaml", []byte(withUI(block)), decode)
	if err != nil {
		t.Fatalf("the block was refused: %v", err)
	}

	got := held.UI.Contributes.Actions[manifest.SlotDeviceAction]
	want := []manifest.Action{{ID: "flush", Title: "Flush", Icon: "wrench", Destructive: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the device action landed as %+v, wanted %+v", got, want)
	}
}

// TestEveryContributionIsValidated is the rest of the block: a title that says
// nothing, an id written twice, a list of the wrong shape and a key this
// version does not read.
func TestEveryContributionIsValidated(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		contributes string
		refuses     string
	}{
		"a page with no title":      {`{"pages":[{"path":"spools"}]}`, "pages[0].title is empty"},
		"a page with a blank title": {`{"pages":[{"path":"spools","title":"  "}]}`, "title is empty"},
		"a page with no path":       {`{"pages":[{"title":"Spools"}]}`, "pages[0].path is empty"},
		"a page with an empty icon": {
			`{"pages":[{"path":"spools","title":"Spools","icon":""}]}`,
			"icon is empty",
		},
		"a page with a hidden word": {
			`{"pages":[{"path":"spools","title":"Spools","hidden":"yes"}]}`,
			"hidden is text",
		},
		"two pages on one path": {
			`{"pages":[{"path":"spools","title":"Spools"},{"path":"spools","title":"Again"}]}`,
			`path names "spools" twice`,
		},
		"a section with no id":    {`{"device_card":[{"title":"Loaded spool"}]}`, "device_card[0].id is empty"},
		"a section with no title": {`{"settings":[{"id":"spools"}]}`, "settings[0].title is empty"},
		"two sections on one id": {
			`{"settings":[{"id":"spools","title":"Spools"},{"id":"spools","title":"Again"}]}`,
			`id names "spools" twice`,
		},
		"an action with no title": {`{"device_action":[{"id":"flush"}]}`, "device_action[0].title is empty"},
		"two actions on one id": {
			`{"device_action":[{"id":"flush","title":"Flush"},{"id":"flush","title":"Again"}]}`,
			`id names "flush" twice`,
		},
		"an action with an icon outside the list": {
			`{"device_action":[{"id":"flush","title":"Flush","icon":"rocket"}]}`,
			`device_action[0].icon is "rocket"`,
		},
		"an unknown key on an action": {
			`{"device_action":[{"id":"flush","title":"Flush","confirm":true}]}`,
			"does not read",
		},
		"a page that is not a mapping": {`{"pages":["spools"]}`, "pages[0] is text"},
		"a pages key that is a mapping": {
			`{"pages":{"path":"spools"}}`,
			"pages is a mapping, expected a list",
		},
		"an unknown key on a page":    {`{"pages":[{"path":"s","title":"S","route":"x"}]}`, "does not read"},
		"an unknown key on a section": {`{"settings":[{"id":"s","title":"S","pane":"x"}]}`, "does not read"},
		"an unknown list":             {`{"dashboard":[{"id":"s","title":"S"}]}`, "does not read"},
		"a slot named by hand":        {`{"pages":[{"path":"s","title":"S","slot":"page"}]}`, "the slot follows"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			refuse(t, withUI(`{"entry":"index.js","library":">=0.3.0 <1.0.0","contributes":`+
				testCase.contributes+`}`), testCase.refuses)
		})
	}
}

// TestTheUIBlockRefusesAKeyItDoesNotRead covers the block itself rather than
// its lists, including the namespace the host derives (ADR-20260911-4p6 rule 15).
func TestTheUIBlockRefusesAKeyItDoesNotRead(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		block   string
		refuses string
	}{
		"a namespace": {
			`{"entry":"index.js","library":">=0.3.0 <1.0.0","namespace":"com.github.aux.acme.spools"}`,
			"derives the namespace",
		},
		"a href": {
			`{"entry":"index.js","library":">=0.3.0 <1.0.0","href":"/m/spools"}`,
			"does not read",
		},
		"widgets, the older name": {
			`{"entry":"index.js","library":">=0.3.0 <1.0.0","widgets":[]}`,
			"does not read",
		},
		"the block as a list": {`[]`, "ui is a list, expected a mapping"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			refuse(t, withUI(testCase.block), testCase.refuses)
		})
	}
}

// refuse parses one document and holds the refusal to naming what is wrong.
func refuse(t *testing.T, document, wanted string) {
	t.Helper()

	_, err := manifest.Parse("module.yaml", []byte(document), decode)
	if err == nil {
		t.Fatalf("%s was accepted", document)
	}

	if !strings.Contains(err.Error(), wanted) {
		t.Errorf("the refusal is %q and does not name %q", err.Error(), wanted)
	}
}
