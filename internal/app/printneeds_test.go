package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printfile"
)

// TestThePerFilamentNeedsReachTheAPIAndTheModuleWire reads a real five-tool XL
// file and follows its needs to the stat route's body and to the module wire.
// Both are JSON a client reads, so the case compares the members by name.
func TestThePerFilamentNeedsReachTheAPIAndTheModuleWire(t *testing.T) {
	t.Parallel()

	file, err := os.Open(filepath.Join("..", "printfile", "testdata", "cube10-xl5t.bgcode"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = file.Close() })

	info, err := file.Stat()
	require.NoError(t, err)

	details := detailsOf(printfile.Read(t.Context(), file, info.Size()))
	require.NotNil(t, details)

	module := printInfoOf(details)

	for name, encoded := range map[string]any{"api": details, "module": module} {
		raw, marshalErr := json.Marshal(encoded)
		require.NoError(t, marshalErr)

		var read struct {
			Filaments []struct {
				Index    int     `json:"index"`
				Used     *bool   `json:"used"`
				Material string  `json:"material"`
				Nozzle   float64 `json:"nozzle_mm"`
				Colour   string  `json:"colour"`
			} `json:"filaments"`
			Bed    map[string]float64 `json:"bed"`
			Bounds map[string]float64 `json:"bounds"`
			Checks struct {
				PrinterModel string   `json:"printer_model"`
				Features     []string `json:"features"`
				Nozzles      []struct {
					Index int `json:"index"`
				} `json:"nozzles"`
			} `json:"checks"`
			FilamentG float64 `json:"filament_g"`
		}

		require.NoError(t, json.Unmarshal(raw, &read), name)
		require.Len(t, read.Filaments, 5, name)
		require.Equal(t, "ASA", read.Filaments[2].Material, name)
		require.True(t, *read.Filaments[2].Used, name)
		require.False(t, *read.Filaments[3].Used, name)
		require.Equal(t, "#3EC0FF", read.Filaments[2].Colour, name)
		require.InDelta(t, 360, read.Bed["width_mm"], 1e-9, name)
		require.InDelta(t, 175, read.Bounds["min_x_mm"], 1e-9, name)
		require.Equal(t, "XL", read.Checks.PrinterModel, name)
		require.Equal(t, []string{"Input shaper"}, read.Checks.Features, name)
		require.Len(t, read.Checks.Nozzles, 3, name)
		require.InDelta(t, 0.84, read.FilamentG, 1e-9, name)
	}
}

// The filaments of a file count from 0 on the wire, as the G-code's T0 does.
func TestTheFileFilamentsCountFromZero(t *testing.T) {
	t.Parallel()

	details := detailsOf(printfile.Preview{Details: printfile.Details{
		Filaments: []printfile.Filament{{Index: 0}, {Index: 1}},
		Checks:    &printfile.Checks{Nozzles: []printfile.NozzleCheck{{Index: 1, DiameterMM: 0.6}}},
	}})

	raw, err := json.Marshal(printInfoOf(details))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"filaments":[{"index":0},{"index":1}]`)
	require.Contains(t, string(raw), `"nozzles":[{"index":1,"diameter_mm":0.6}]`)
}
