package settings_test

import (
	"errors"
	"testing"
	"time"

	"github.com/prusa3d/server-slicing/internal/settings"
)

func TestAnEmptyBlockTakesTheDefaults(t *testing.T) {
	t.Parallel()

	values, err := settings.Settings{}.Checked()
	if err != nil {
		t.Fatal(err)
	}

	if values.MaxJobs != 1 || values.Timeout != 30*time.Minute ||
		!values.GitHubReleases || values.MaxInputBytes != 512<<20 {
		t.Fatalf("the defaults are %+v", values)
	}
}

func TestASettingOutsideItsBoundsIsRefused(t *testing.T) {
	t.Parallel()

	zero, huge := 0, 100_000

	cases := map[string]settings.Settings{
		"no worker":            {MaxJobs: &zero},
		"a day and more":       {TimeoutMinutes: &huge},
		"a manifest over http": {ManifestURL: "http://builds.example.com/m.json"},
		"a relative manifest":  {ManifestURL: "m.json"},
		"a relative binary":    {LocalBinaries: []string{"prusa-slicer"}},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := held.Checked(); !errors.Is(err, settings.ErrInvalid) {
				t.Fatalf("answered %v", err)
			}
		})
	}
}

func TestRunMemoryBytes(t *testing.T) {
	t.Parallel()

	const mib = 1 << 20

	for _, tc := range []struct {
		name    string
		budget  int64
		ok      bool
		maxJobs int
		want    int64
	}{
		{"one job", 2560 * mib, true, 1, 2496 * mib},
		{"two jobs share it", 2560 * mib, true, 2, 1248 * mib},
		{"no budget", 0, false, 1, 0},
		{"a budget below the reserve", 32 * mib, true, 1, mib},
		{"no jobs counts as one", 2560 * mib, true, 0, 2496 * mib},
	} {
		if got := settings.RunMemoryBytes(tc.budget, tc.ok, tc.maxJobs); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
