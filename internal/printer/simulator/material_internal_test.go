package simulator

import "testing"

func TestAPrinterWithNoMaterialLeavesItOutOfTelemetry(t *testing.T) {
	t.Parallel()

	for _, one := range []struct {
		name     string
		none     bool
		reported bool
	}{
		{"a printer that reports its filament", false, true},
		{"a printer with no filament sensor", true, false},
	} {
		printer, err := New(Config{Model: "MK4S", Serial: "SIM-1", BaseURL: "http://127.0.0.1:1", NoMaterial: one.none})
		if err != nil {
			t.Fatal(err)
		}

		printer.mu.Lock()
		_, reported := printer.telemetryFields()["material"]
		printer.mu.Unlock()

		if reported != one.reported {
			t.Errorf("%s: material reported = %v; want %v", one.name, reported, one.reported)
		}
	}
}
