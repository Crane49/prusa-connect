package printcompat_test

import (
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/printcompat"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// onFirmware is a printer that reports a firmware version.
func onFirmware(p printcompat.Printer, version string) printcompat.Printer {
	p.Firmware = version

	return p
}

// withFeatures is a file whose M862.6 lines name the features given.
func withFeatures(f *storageapi.PrintInfo, features ...string) *storageapi.PrintInfo {
	f.Checks.Features = features

	return f
}

// levelOf is the level of the first finding of a rule, and "" when no
// finding has the rule.
func levelOf(found []printcompat.Finding, rule printcompat.Rule) printcompat.Level {
	for _, f := range found {
		if f.Rule == rule {
			return f.Level
		}
	}

	return ""
}

func unverified(got printcompat.Verdict, rule printcompat.Rule) bool {
	return slices.ContainsFunc(got.Unverified, func(f printcompat.Finding) bool { return f.Rule == rule })
}

// TestEvaluateINDXLock is the indx_lock check of
// CompatibilityReport::generate_without_toolmapping and the INDX lock arm of
// the M862.6 parse in GCodeInfo, both from Prusa-Firmware-Buddy v6.6.2.
func TestEvaluateINDXLock(t *testing.T) {
	t.Parallel()

	const lock = "INDX lock"

	cases := []struct {
		name        string
		file        *storageapi.PrintInfo
		printer     printcompat.Printer
		lock        printcompat.Level
		unsupported printcompat.Level
		unverified  bool
		// unknownFeature is unsupported_feature listed as unverified.
		unknownFeature bool
	}{
		{
			name:    "an INDX file without the lock on current firmware warns",
			file:    file("COREONEINDX", "PLA"),
			printer: onFirmware(printer("CoreOne_INDX", "PLA"), "6.9.0"),
			lock:    printcompat.LevelWarning,
		},
		{
			name:    "a Core One L INDX on the first firmware with the check warns",
			file:    file("COREONEL-INDX", "PLA"),
			printer: onFirmware(printer("CoreOneL_INDX", "PLA"), "6.6.2+1234"),
			lock:    printcompat.LevelWarning,
		},
		{
			name:       "a printer that did not report its firmware leaves the lock unverified",
			file:       file("COREONEINDX", "PLA"),
			printer:    onFirmware(printer("CoreOne_INDX", "PLA"), ""),
			unverified: true,
		},
		{
			name:           "a printer that did not report its firmware may not know the feature",
			file:           withFeatures(file("COREONEINDX", "PLA"), "Input shaper", lock),
			printer:        onFirmware(printer("CoreOne_INDX", "PLA"), ""),
			unknownFeature: true,
		},
		{
			name:    "an INDX file with the lock passes",
			file:    withFeatures(file("COREONEINDX", "PLA"), "Input shaper", lock),
			printer: onFirmware(printer("CoreOne_INDX", "PLA"), "6.9.0"),
		},
		{
			name:    "the firmware compares the feature without case",
			file:    withFeatures(file("COREONEINDX", "PLA"), "Input shaper", "indx LOCK"),
			printer: onFirmware(printer("CoreOne_INDX", "PLA"), "6.9.0"),
		},
		{
			name:    "an INDX firmware older than the check does not ask for the lock",
			file:    file("COREONEINDX", "PLA"),
			printer: onFirmware(printer("CoreOne_INDX", "PLA"), "6.6.1"),
		},
		{
			name:        "an INDX firmware older than the check does not know the feature",
			file:        withFeatures(file("COREONEINDX", "PLA"), "Input shaper", lock),
			printer:     onFirmware(printer("CoreOne_INDX", "PLA"), "6.6.1"),
			unsupported: printcompat.LevelWarning,
		},
		{
			name:    "a Core One without INDX does not ask for the lock",
			file:    file("COREONE", "PLA"),
			printer: onFirmware(printer("CoreOne", "PLA"), "6.9.0"),
		},
		{
			name:        "a Core One without INDX does not know the feature",
			file:        withFeatures(file("COREONE", "PLA"), "Input shaper", lock),
			printer:     onFirmware(printer("CoreOne", "PLA"), "6.9.0"),
			unsupported: printcompat.LevelWarning,
		},
		{
			name:    "a model this package does not know is not asked for the lock",
			file:    file("COREONEINDX", "PLA"),
			printer: onFirmware(printer("Voron 2.4", "PLA"), "6.9.0"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := printcompat.Evaluate(tc.file, tc.printer)

			if level := levelOf(got.Findings, printcompat.RuleINDXLock); level != tc.lock {
				t.Errorf("indx_lock: got %q, want %q (findings %+v)", level, tc.lock, got.Findings)
			}

			if level := levelOf(got.Findings, printcompat.RuleUnsupportedFeature); level != tc.unsupported {
				t.Errorf("unsupported_feature: got %q, want %q (findings %+v)", level, tc.unsupported, got.Findings)
			}

			if listed := unverified(got, printcompat.RuleUnsupportedFeature); listed != tc.unknownFeature {
				t.Errorf("unverified unsupported_feature: got %v, want %v (unverified %+v)",
					listed, tc.unknownFeature, got.Unverified)
			}

			if listed := unverified(got, printcompat.RuleINDXLock); listed != tc.unverified {
				t.Errorf("unverified indx_lock: got %v, want %v (unverified %+v)",
					listed, tc.unverified, got.Unverified)
			}
		})
	}

	t.Run("a file whose G-code was not read leaves the lock unverified", func(t *testing.T) {
		t.Parallel()

		f := file("COREONEINDX", "PLA")
		f.Checks = nil

		got := printcompat.Evaluate(f, onFirmware(printer("CoreOne_INDX", "PLA"), "6.9.0"))
		if !unverified(got, printcompat.RuleINDXLock) || levelOf(got.Findings, printcompat.RuleINDXLock) != "" {
			t.Errorf("got %+v", got)
		}
	})
}

// TestEvaluateNozzleCleaner is the nozzle_cleaner_not_calibrated check of
// CompatibilityReport::generate_without_toolmapping, in every public INDX
// firmware from Prusa-Firmware-Buddy v6.6.0. It refuses the print, and its
// input never reaches the host, so it is unverified on every INDX printer.
func TestEvaluateNozzleCleaner(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		file    *storageapi.PrintInfo
		printer printcompat.Printer
		listed  bool
	}{
		{
			name:    "a Core One with INDX",
			file:    withFeatures(file("COREONEINDX", "PLA"), "Input shaper", "INDX lock"),
			printer: onFirmware(printer("CoreOne_INDX", "PLA"), "6.9.0"),
			listed:  true,
		},
		{
			name:    "a Core One L with INDX on the first INDX firmware",
			file:    file("COREONEL-INDX", "PLA"),
			printer: onFirmware(printer("CoreOneL_INDX", "PLA"), "6.6.0"),
			listed:  true,
		},
		{
			name: "an INDX printer with a file whose G-code was not read",
			file: func() *storageapi.PrintInfo {
				f := file("COREONEINDX", "PLA")
				f.Checks = nil

				return f
			}(),
			printer: printer("CoreOne_INDX", "PLA"),
			listed:  true,
		},
		{
			name:    "a Core One without INDX has no nozzle cleaner check",
			file:    file("COREONE", "PLA"),
			printer: onFirmware(printer("CoreOne", "PLA"), "6.9.0"),
		},
		{
			name:    "an XL has no nozzle cleaner check",
			file:    file("XL", "PLA", "PETG"),
			printer: onFirmware(printer("XL", "PLA", "PETG"), "6.9.0"),
		},
		{
			name:    "a model this package does not know",
			file:    file("COREONEINDX", "PLA"),
			printer: printer("Voron 2.4", "PLA"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := printcompat.Evaluate(tc.file, tc.printer)

			if listed := unverified(got, printcompat.RuleNozzleCleaner); listed != tc.listed {
				t.Errorf("unverified nozzle_cleaner: got %v, want %v (unverified %+v)",
					listed, tc.listed, got.Unverified)
			}

			// Never a pass, and never a finding: the package cannot see the
			// calibration, so it does not guess either way.
			if level := levelOf(got.Findings, printcompat.RuleNozzleCleaner); level != "" {
				t.Errorf("nozzle_cleaner is a finding at %q", level)
			}
		})
	}

	t.Run("a clean INDX print is OK with the cleaner unverified", func(t *testing.T) {
		t.Parallel()

		got := printcompat.Evaluate(
			withFeatures(file("COREONEINDX", "PLA"), "Input shaper", "INDX lock"),
			onFirmware(printer("CoreOne_INDX", "PLA"), "6.9.0"),
		)

		if got.Level != printcompat.LevelOK || len(got.Findings) != 0 || len(got.Unverified) != 1 {
			t.Errorf("got %+v", got)
		}
	})
}

// hardenedHead is a one-head printer with a material loaded and a nozzle that
// is hardened, not hardened, or of unknown hardness for "?".
func hardenedHead(model, material, hardened string) printcompat.Printer {
	s := state{connectorapi.ToolCountPath: "1"}.head("0", material)
	if hardened == "?" {
		delete(s, at("0", connectorapi.FeatureNozzleHardened))
	} else {
		s[at("0", connectorapi.FeatureNozzleHardened)] = hardened
	}

	return onFirmware(s.printer(model), "6.9.0")
}

// TestEvaluateFilamentAbrasive is the abrasive check of filament_compatibility
// (StandardFFFPhysicalToolBase::filament_compatibility_report), which
// generate_toolmapping_only_noclear runs from Prusa-Firmware-Buddy v6.8.0.
// No preset of a release firmware is abrasive, and the abrasive flag of a
// filament a person defined is not reported, so the check is never a
// finding: it is unverified or it does not apply.
func TestEvaluateFilamentAbrasive(t *testing.T) {
	t.Parallel()

	hardenedAsked := func(model, material string) *storageapi.PrintInfo {
		f := file(model, material)
		f.Checks.Nozzles[0].Hardened = yes()

		return f
	}

	cases := []struct {
		name       string
		file       *storageapi.PrintInfo
		printer    printcompat.Printer
		unverified bool
		hardened   printcompat.Level
	}{
		{
			name:       "a filament type that may be one a person defined on the printer",
			file:       file("COREONE", "PETG-CF"),
			printer:    hardenedHead("CoreOne", "PETG-CF", "false"),
			unverified: true,
		},
		{
			name:       "PPS on a release firmware is a filament a person defined",
			file:       file("MK4", "PPS"),
			printer:    hardenedHead("MK4", "PPS", "false"),
			unverified: true,
		},
		{
			name:       "a nozzle of unknown hardness",
			file:       file("COREONE", "PPA"),
			printer:    hardenedHead("CoreOne", "PPA", "?"),
			unverified: true,
		},
		{
			name:       "the first firmware with the check",
			file:       file("COREONE", "PETG-CF"),
			printer:    onFirmware(hardenedHead("CoreOne", "PETG-CF", "false"), "6.8.0"),
			unverified: true,
		},
		{
			name:       "a printer that did not report its firmware",
			file:       file("COREONE", "PETG-CF"),
			printer:    onFirmware(hardenedHead("CoreOne", "PETG-CF", "false"), ""),
			unverified: true,
		},
		{
			name: "an MMU file is checked against the head's nozzle",
			file: func() *storageapi.PrintInfo {
				f := file("MK4S", "PLA", "PA11CF")
				f.Checks.Features = append(f.Checks.Features, "MMU3")

				return f
			}(),
			printer:    onFirmware(mmuPrinter("MK4S", "PLA", "PA11CF"), "6.9.0"),
			unverified: true,
		},
		{
			name:       "nozzle_hardened still warns beside an unverified abrasive check",
			file:       hardenedAsked("COREONE", "PETG-CF"),
			printer:    hardenedHead("CoreOne", "PETG-CF", "false"),
			unverified: true,
			hardened:   printcompat.LevelWarning,
		},
		{
			name:    "a hardened nozzle takes any filament",
			file:    file("COREONE", "PETG-CF"),
			printer: hardenedHead("CoreOne", "PETG-CF", "true"),
		},
		{
			name:    "a preset is never abrasive",
			file:    file("COREONE", "PETG"),
			printer: hardenedHead("CoreOne", "PETG", "?"),
		},
		{
			name:    "the firmware matches a preset name exactly",
			file:    file("COREONE", "PA"),
			printer: hardenedHead("CoreOne", "PA", "false"),
		},
		{
			name:    "the last firmware before the check",
			file:    file("COREONE", "PETG-CF"),
			printer: onFirmware(hardenedHead("CoreOne", "PETG-CF", "false"), "6.6.3"),
		},
		{
			name:    "a firmware outside the Buddy line does not make the check",
			file:    file("MK3S", "PETG-CF"),
			printer: onFirmware(hardenedHead("MK3S", "PETG-CF", "false"), "3.14.1"),
		},
		{
			name: "a file that names no filament type is not checked",
			file: func() *storageapi.PrintInfo {
				f := file("COREONE", "PETG-CF")
				f.Filaments[0].Material = ""

				return f
			}(),
			printer: hardenedHead("CoreOne", "PETG-CF", "false"),
		},
	}

	// Every preset of a release firmware, and the firmware's word for no
	// filament, is skipped on a nozzle that is not hardened.
	for _, material := range []string{"PLA", "PETG", "ASA", "PC", "PVB", "ABS", "HIPS", "PP", "FLEX", "PA", "---"} {
		// file reads a leading "-" as an unused filament, so the name is
		// set after it.
		f := file("COREONE", "PLA")
		f.Filaments[0].Material = material

		cases = append(cases, struct {
			name       string
			file       *storageapi.PrintInfo
			printer    printcompat.Printer
			unverified bool
			hardened   printcompat.Level
		}{
			name:    "the filament type " + material + " is skipped",
			file:    f,
			printer: hardenedHead("CoreOne", material, "false"),
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := printcompat.Evaluate(tc.file, tc.printer)

			if listed := unverified(got, printcompat.RuleFilamentAbrasive); listed != tc.unverified {
				t.Errorf("unverified filament_abrasive: got %v, want %v (unverified %+v)",
					listed, tc.unverified, got.Unverified)
			}

			if level := levelOf(got.Findings, printcompat.RuleFilamentAbrasive); level != "" {
				t.Errorf("filament_abrasive is a finding at %q", level)
			}

			if level := levelOf(got.Findings, printcompat.RuleNozzleHardened); level != tc.hardened {
				t.Errorf("nozzle_hardened: got %q, want %q (findings %+v)", level, tc.hardened, got.Findings)
			}
		})
	}
}
