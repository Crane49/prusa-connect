package simulator

import (
	"maps"
	"testing"
)

// TestToolSetterKwargs: the firmware reads a tool setter as one
// tools.<k>.<name> key, with k counted the way the printer's own INFO keys its
// tools. The simulator reads it back into the names the profile declares.
func TestToolSetterKwargs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		kwargs map[string]any
		tree   bool
		want   map[string]any
		ok     bool
	}{
		{
			name: "numbered, first tool", kwargs: map[string]any{"tools.1.hardened": true},
			want: map[string]any{"tool": 0.0, "hardened": true}, ok: true,
		},
		{
			name: "tree, first tool", kwargs: map[string]any{"tools.0.high_flow": false}, tree: true,
			want: map[string]any{"tool": 0.0, "high_flow": false}, ok: true,
		},
		{
			name: "tree, fifth tool", kwargs: map[string]any{"tools.4.nozzle_diameter": 0.6}, tree: true,
			want: map[string]any{"tool": 4.0, "nozzle_diameter": 0.6}, ok: true,
		},
		{name: "numbered has no tool 0", kwargs: map[string]any{"tools.0.hardened": true}},
		{name: "not a number", kwargs: map[string]any{"tools.x.hardened": true}},
		{name: "a leading zero", kwargs: map[string]any{"tools.01.hardened": true}},
		{name: "no setter name", kwargs: map[string]any{"tools.1": true}},
		{name: "another setter", kwargs: map[string]any{"chamber.target_temp": 40.0}},
		{name: "the flat form", kwargs: map[string]any{"tool": 1.0, "hardened": true}},
		{name: "a key beside it", kwargs: map[string]any{"tools.1.hardened": true, "tool": 1.0}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := toolSetterKwargs(tc.kwargs, tc.tree)
			if ok != tc.ok {
				t.Fatalf("read = %v, want %v", ok, tc.ok)
			}

			want := tc.want
			if !tc.ok {
				want = tc.kwargs
			}

			if !maps.Equal(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// TestToolSetterKeyResolvesToItsVerb: the key read back names the verb the
// profile declares, on a printer of either shape.
func TestToolSetterKeyResolvesToItsVerb(t *testing.T) {
	t.Parallel()

	for _, tree := range []bool{false, true} {
		p, err := New(Config{Model: "XL", BaseURL: "http://127.0.0.1:1", ToolTree: tree})
		if err != nil {
			t.Fatalf("new XL: %v", err)
		}

		for key, want := range map[string]string{
			"tools.1.nozzle_diameter": "SET_TOOL_NOZZLE_DIAMETER",
			"tools.2.hardened":        "SET_TOOL_HARDENED",
			"tools.3.high_flow":       "SET_TOOL_HIGH_FLOW",
		} {
			kwargs, ok := toolSetterKwargs(map[string]any{key: true}, tree)
			if !ok {
				t.Fatalf("%s was not read on tree=%v", key, tree)
			}

			definition, found := p.resolve(jsonCommand{Command: setValueCommand, Kwargs: kwargs})
			if !found || definition.Name != want {
				t.Errorf("%s on tree=%v resolves to %q (%v), want %s", key, tree, definition.Name, found, want)
			}
		}
	}
}
