package main

import (
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// TestAncestorShadowSelection: a server that runs the file extension keeps
// ContentStore1 of every node above, and one that does not keeps nothing.
func TestAncestorShadowSelection(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name  string
		files string
		want  shadow.Selection
	}{
		{name: "files off", want: shadow.Selection{}},
		{
			name:  "files on",
			files: `,"files":{"base_url":"https://brno.example:8443/fractal"}`,
			want:  shadow.Selection{Patterns: []fractal.Pattern{"ContentStore1"}},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			held, err := ParseConfig(fmt.Appendf(nil,
				`{"node_id":"brno","data_dir":%q,"dedup_window":"1m"%s}`, t.TempDir(), row.files))
			if err != nil {
				t.Fatalf("read the settings: %v", err)
			}

			held.Grants.Data = t.TempDir()

			built, err := build(t.Context(), held, newLink(), &tree{}, session{fleet: noMachines{}},
				slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatalf("build the node: %v", err)
			}
			t.Cleanup(func() { _ = built.binding.Close() })

			if got := built.node.Ancestors().Selection(); !reflect.DeepEqual(got, row.want) {
				t.Errorf("the ancestor shadow selects %+v, want %+v", got, row.want)
			}
		})
	}
}
