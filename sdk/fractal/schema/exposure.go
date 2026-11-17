package schema

// Exposure is the set of directions in which a node reports every instance of
// an interface. It governs reporting only. A node calls a method only on a
// node below it, whatever the exposure.
type Exposure uint8

// The exposures a definition file may state.
const (
	// ExposureUpward reports an instance to the nodes above, by REP.
	ExposureUpward Exposure = 1
	// ExposureDownward reports an instance to the nodes below, by AREP.
	ExposureDownward Exposure = 2
	// ExposureBoth reports an instance in both directions.
	ExposureBoth Exposure = ExposureUpward | ExposureDownward
)

// Up reports whether a node reports the instance to the nodes above.
func (e Exposure) Up() bool { return e&ExposureUpward != 0 }

// Down reports whether a node reports the instance to the nodes below.
func (e Exposure) Down() bool { return e&ExposureDownward != 0 }

// Valid reports whether e is ExposureUpward, ExposureDownward or ExposureBoth.
func (e Exposure) Valid() bool { return e >= ExposureUpward && e <= ExposureBoth }

// String returns the name a definition file uses for the exposure: "upward",
// "downward" or "both". It returns "unknown" for any other value.
func (e Exposure) String() string {
	switch e {
	case ExposureUpward:
		return "upward"
	case ExposureDownward:
		return "downward"
	case ExposureBoth:
		return "both"
	}
	return "unknown"
}

// parseExposure returns the exposure named "upward", "downward" or "both". ok
// is false for any other string.
func parseExposure(s string) (e Exposure, ok bool) {
	switch s {
	case "upward":
		return ExposureUpward, true
	case "downward":
		return ExposureDownward, true
	case "both":
		return ExposureBoth, true
	}
	return 0, false
}
