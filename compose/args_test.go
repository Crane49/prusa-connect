package compose

import (
	"slices"
	"testing"
)

func TestRestartArgumentsDropTheSafeFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"single dash", []string{"bin", "-safe", "-config", "c.yaml"}, []string{"bin", "-config", "c.yaml"}},
		{"double dash", []string{"bin", "--safe"}, []string{"bin"}},
		{"true value", []string{"bin", "-safe=true", "-addr", ":1"}, []string{"bin", "-addr", ":1"}},
		{"double dash value", []string{"bin", "--safe=false"}, []string{"bin"}},
		{"false value", []string{"bin", "-safe=false"}, []string{"bin"}},
		{"absent", []string{"bin", "-config", "c.yaml"}, []string{"bin", "-config", "c.yaml"}},
		{"after the command", []string{"bin", "core", "-safe"}, []string{"bin", "core"}},
		{"a value that looks like it", []string{"bin", "-config", "-safer"}, []string{"bin", "-config", "-safer"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := withoutSafe(test.in); !slices.Equal(got, test.want) {
				t.Fatalf("withoutSafe(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
