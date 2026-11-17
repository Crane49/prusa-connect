package wire_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/wire"
)

func TestMemoryBudget(t *testing.T) {
	cases := []struct {
		value string
		bytes int64
		ok    bool
	}{
		{"268435456", 268435456, true},
		{"", 0, false},
		{"0", 0, false},
		{"-5", 0, false},
		{"abc", 0, false},
	}

	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			t.Setenv(wire.MemoryBudgetEnv, c.value)

			bytes, ok := wire.MemoryBudget()
			if bytes != c.bytes || ok != c.ok {
				t.Fatalf("MemoryBudget() = (%d, %v), want (%d, %v)", bytes, ok, c.bytes, c.ok)
			}
		})
	}
}
