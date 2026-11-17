package manifest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The keys of ADR-20261029-dgv: a tab of a module's own on a file's page, and
// a file action drawn as a button there.

func TestAFilePageTakesATabAndAPrimaryAction(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withUI(`{"entry":"ui/index.js","library":">=0.0.1",
	  "contributes":{
	    "file_tab":[{"id":"history","title":"History","file_types":[".3mf"]}],
	    "file_action":[
	      {"id":"slice","title":"Slice","primary":true,"file_types":[".3mf"]},
	      {"id":"share","title":"Share"}]}}`)), decode)
	if err != nil {
		t.Fatalf("a file tab and a primary action were refused: %v", err)
	}

	tabs := held.UI.Contributes.Sections[manifest.SlotFileTab]
	if len(tabs) != 1 || tabs[0].ID != "history" || !slices.Equal(tabs[0].FileTypes, []string{".3mf"}) {
		t.Errorf("the file tab read as %+v", tabs)
	}

	actions := held.UI.Contributes.Actions[manifest.SlotFileAction]
	if len(actions) != 2 || !actions[0].Primary || actions[1].Primary {
		t.Errorf("primary read as %+v", actions)
	}
}

func TestOnlyAFileActionIsPrimary(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ contributes, refuses string }{
		"a device action": {
			`{"device_action":[{"id":"x","title":"X","primary":true}]}`, "primary",
		},
		"a file tab": {
			`{"file_tab":[{"id":"x","title":"X","primary":true}]}`, "primary",
		},
		"not a truth": {
			`{"file_action":[{"id":"x","title":"X","primary":"yes"}]}`, "expected true or false",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(withUI(`{"entry":"ui/index.js","library":">=0.0.1",
			  "contributes":`+tc.contributes+`}`)), decode)
			if err == nil || !strings.Contains(err.Error(), tc.refuses) {
				t.Fatalf("the refusal is %v and does not name %q", err, tc.refuses)
			}
		})
	}
}
