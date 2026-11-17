package files_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

func TestCheckName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in string
		ok bool
	}{
		{in: "a.txt", ok: true},
		{in: "Ä.txt", ok: true},
		{in: "...", ok: true},
		{in: ".hidden", ok: true},
		{in: "Benchy & Co + 3DBenchy #2 (Ä).bgcode", ok: true},
		{in: "Ä.txt", ok: true},
		{in: ""},
		{in: "."},
		{in: ".."},
		{in: "a/b"},
		{in: "/"},
		{in: "a\x00b"},
		{in: "\xff.txt"},
		{in: "\xc3"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			err := files.CheckName(tc.in)
			if tc.ok != (err == nil) || err != nil && !errors.Is(err, files.ErrBadName) {
				t.Fatalf("CheckName(%q) = %v, want ok %v or ErrBadName", tc.in, err, tc.ok)
			}
		})
	}
}
