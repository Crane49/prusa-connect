package mini404

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenSeed is the seed the checked-in fixture was generated from. Changing
// either without regenerating the other is the failure this test exists to
// catch. See testdata/eeprom/README.md for the command.
var goldenSeed = ConnectSeed{
	Host:    "10.0.2.2",
	Port:    8080,
	TLS:     false,
	Token:   "0123456789abcdefghij",
	Enabled: true,
}

// TestBanksMatchTheVendorGenerator is the load-bearing test in this file. The
// fixture came out of the firmware repository's own eeprom.py, so a byte
// difference means our port and the format the firmware reads have diverged.
func TestBanksMatchTheVendorGenerator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		seed   ConnectSeed
		prefix string
	}{
		{
			name:   "a preset token, so firmware dials the websocket directly",
			seed:   goldenSeed,
			prefix: "",
		},
		{
			name:   "no token, so the Connect Token item is omitted entirely",
			seed:   ConnectSeed{Host: "10.0.2.2", Port: 8080, TLS: false, Enabled: true},
			prefix: "no-token-",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			bank1, bank2, err := WriteEEPROMBanks(dir, tc.seed)
			if err != nil {
				t.Fatalf("write banks: %v", err)
			}

			for i, produced := range []string{bank1, bank2} {
				golden := filepath.Join("testdata", "eeprom", tc.prefix+filepath.Base(produced))
				compareBankToGolden(t, golden, produced, i+1)
			}
		})
	}
}

func compareBankToGolden(t *testing.T, golden, produced string, bank int) {
	t.Helper()

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden bank %d: %v", bank, err)
	}

	got, err := os.ReadFile(produced)
	if err != nil {
		t.Fatalf("read produced bank %d: %v", bank, err)
	}

	if len(got) != eepromFileSize {
		t.Errorf("bank %d is %d bytes, MINI404 mounts a %d byte drive", bank, len(got), eepromFileSize)
	}

	if bytes.Equal(want, got) {
		return
	}

	if len(want) != len(got) {
		t.Fatalf("bank %d is %d bytes, the golden is %d", bank, len(got), len(want))
	}

	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("bank %d differs from %s at offset %#x: got %#02x, the golden has %#02x. "+
				"Regenerate the fixture deliberately, see testdata/eeprom/README.md",
				bank, golden, i, got[i], want[i])
		}
	}
}

func TestSeedValidationRefusesAValueTheFieldCannotHold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		seed ConnectSeed
		want error
	}{
		{
			name: "a host of exactly the field size leaves no room for the terminator",
			seed: ConnectSeed{Host: strings.Repeat("h", connectHostFieldSize)},
			want: ErrHostTooLong,
		},
		{
			name: "a host over the field size",
			seed: ConnectSeed{Host: strings.Repeat("h", 64)},
			want: ErrHostTooLong,
		},
		{
			name: "a token of exactly the field size leaves no room for the terminator",
			seed: ConnectSeed{Host: "10.0.2.2", Token: strings.Repeat("t", connectTokenFieldSize)},
			want: ErrTokenTooLong,
		},
		{
			name: "a token over the field size",
			seed: ConnectSeed{Host: "10.0.2.2", Token: strings.Repeat("t", 64)},
			want: ErrTokenTooLong,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			_, _, err := WriteEEPROMBanks(dir, tc.seed)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatalf("read dir: %v", readErr)
			}

			if len(entries) != 0 {
				t.Errorf("a refused seed wrote %d files, it must write none", len(entries))
			}
		})
	}
}

func TestSeedValidationAcceptsTheLongestValueThatFits(t *testing.T) {
	t.Parallel()

	seed := ConnectSeed{
		Host:    strings.Repeat("h", connectHostFieldSize-1),
		Port:    443,
		TLS:     true,
		Token:   strings.Repeat("t", connectTokenFieldSize-1),
		Enabled: true,
	}

	if _, _, err := WriteEEPROMBanks(t.TempDir(), seed); err != nil {
		t.Fatalf("write banks: %v", err)
	}
}

// TestEEPROMBanksAreReproducible holds the property the golden test depends on:
// one seed produces one pair of images, with nothing in the format that varies
// between runs.
func TestEEPROMBanksAreReproducible(t *testing.T) {
	t.Parallel()

	first, err := eepromImage(goldenSeed)
	if err != nil {
		t.Fatalf("first image: %v", err)
	}

	second, err := eepromImage(goldenSeed)
	if err != nil {
		t.Fatalf("second image: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Error("two images from one seed differ")
	}
}

// TestItemIDsMatchTheFirmwareHashes pins the ten hashes the firmware compiles
// into journal_hashes.hpp. A rename upstream changes an id, and a record the
// firmware does not recognise is silently ignored, so this failing is cheaper
// than a printer that boots with default settings.
func TestItemIDsMatchTheFirmwareHashes(t *testing.T) {
	t.Parallel()

	want := map[string]uint16{
		"Run Selftest":          94,
		"Run XYZ Calibration":   13783,
		"Run First Layer":       12341,
		"FSensor Enabled":       4025,
		"Selftest Result Gears": 14355,
		"Connect Host":          4769,
		"Connect Token":         3216,
		"Connect Port":          8267,
		"Connect TLS":           2437,
		"Connect Enabled":       1037,
	}

	for name, id := range want {
		if got := eepromItemID(name); got != id {
			t.Errorf("%q hashes to %d, the firmware compiles in %d", name, got, id)
		}
	}
}
