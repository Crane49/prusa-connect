package fixture_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/prusa3d/server-slicing/internal/fixture"
)

// gateProject is the project scripts/check.sh slices on a demo server, with
// the stand-in slicer: two cubes on bed 0 and one on bed 1.
var gateProject = fixture.Project{
	At:       [][2]float64{{100, 100}, {fixture.Bed1X + 100, 100}, {130, 100}},
	Settings: fixture.Bed200,
	Mode:     "binary",
}

// The committed file is the one the fixture writes, so a change to the
// fixture is a change to the file the gate slices. SLICER_WRITE_FIXTURE=1
// writes it again.
func TestTheGateProjectIsTheFixture(t *testing.T) {
	t.Parallel()

	want, err := gateProject.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	const path = "../../testdata/three-cubes.3mf"

	if os.Getenv("SLICER_WRITE_FIXTURE") == "1" {
		if err = os.WriteFile(path, want, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	held, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(held, want) {
		t.Fatal("testdata/three-cubes.3mf is not what the fixture writes. Run with SLICER_WRITE_FIXTURE=1")
	}
}
