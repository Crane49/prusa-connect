package analysis_test

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fileapi"

	"github.com/prusa3d/threemf/internal/analysis"
	"github.com/prusa3d/threemf/threemf"
)

// Every text in a project is the file's own, so a file with long names and
// many filaments is answered cut to the bounds, not refused.
func TestALongProjectIsCutToTheBounds(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", fileapi.MaxTextBytes)
	filaments := make([]threemf.Filament, fileapi.MaxFilaments+5)

	for i := range filaments {
		filaments[i] = threemf.Filament{Extruder: i + 1, Type: long}
	}

	project := &threemf.Project{
		Producer: threemf.Producer{Name: "PrusaSlicer", Dialect: threemf.DialectPrusaSlicer2},
		Setups: []threemf.Setup{{
			PrinterModel:     long,
			PrinterProfile:   long,
			PrintProfile:     long,
			MaterialProfiles: []string{long},
			Filaments:        filaments,
		}},
		Objects: []threemf.Object{{Name: long}},
	}

	answer := analysis.Of(project)

	if err := answer.Validate(); err != nil {
		t.Fatalf("the answer is out of bounds: %v", err)
	}

	if answer.Print == nil || len(answer.Print.Filaments) != fileapi.MaxFilaments {
		t.Fatalf("print %+v", answer.Print)
	}
}
