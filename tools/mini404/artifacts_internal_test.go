package mini404

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestThePinTableCarriesTheMK4Row(t *testing.T) {
	t.Parallel()

	pins, err := LoadPins()
	if err != nil {
		t.Fatalf("load the pin table: %v", err)
	}

	pin, err := PinFor("mk4")
	if err != nil {
		t.Fatalf("the table has no MK4 row: %v", err)
	}
	// The 027c machine is the one the vendor harness uses and the one the
	// live lane proved; prusa-mk4-034 redscreens on i2c with this firmware.
	if pin.Machine != "prusa-mk4-027c" {
		t.Errorf("the MK4 machine is %q and should be prusa-mk4-027c", pin.Machine)
	}
	if pin.Preset == "" || pin.FirmwareRef == "" {
		t.Errorf("the MK4 row is incomplete: %+v", pin)
	}
	for _, p := range pins {
		if p.Mini404SHA == "" {
			t.Errorf("row %q carries no mini404_sha", p.Model)
		}
	}
}

func TestThePinTableCarriesTheCoreOneRow(t *testing.T) {
	t.Parallel()

	pin, err := PinFor("coreone")
	if err != nil {
		t.Fatalf("the table has no CoreOne row: %v", err)
	}
	// prusa-coreone is the machine the carried patch adds. It is a copy of
	// prusa-mk4-027c with a door sensor on the ADC channel the MK4 gives to
	// the case thermistor, which is what the CoreOne firmware demands before
	// it will run at all.
	if pin.Machine != "prusa-coreone" {
		t.Errorf("the CoreOne machine is %q and should be prusa-coreone", pin.Machine)
	}
	if pin.Preset != "coreone" {
		t.Errorf("the CoreOne preset is %q and should be coreone", pin.Preset)
	}
}

func TestAModelWithNoPinRowIsRefused(t *testing.T) {
	t.Parallel()

	_, err := PinFor("XL")
	if err == nil {
		t.Fatal("a model the table does not cover was accepted")
	}
	if !strings.Contains(err.Error(), PinFile) {
		t.Errorf("the error does not name the pin table: %v", err)
	}
}

func TestTheEmulatorComesFromTheEnvironmentWhenItIsSet(t *testing.T) {
	t.Setenv(EnvQEMU, "/somewhere/else/qemu-system-buddy")

	got, err := Resolver{Cache: t.TempDir()}.QEMUBinary()
	if err != nil {
		t.Fatalf("resolve the emulator: %v", err)
	}
	if got != "/somewhere/else/qemu-system-buddy" {
		t.Errorf("the emulator is %q and the environment named another path", got)
	}
}

func TestAMissingEmulatorNamesTheBuildScript(t *testing.T) {
	t.Setenv(EnvQEMU, "")

	_, err := Resolver{Cache: t.TempDir()}.QEMUBinary()
	if err == nil {
		t.Fatal("an empty cache resolved an emulator")
	}
	if !errors.Is(err, ErrNoArtifact) {
		t.Errorf("the error is not an ErrNoArtifact: %v", err)
	}
	if !strings.Contains(err.Error(), buildQEMUScript) {
		t.Errorf("the error does not name %s: %v", buildQEMUScript, err)
	}
}

func TestAnEmulatorInTheCacheIsResolvedByItsPinnedRevision(t *testing.T) {
	t.Setenv(EnvQEMU, "")

	cache := t.TempDir()
	sha, err := pinnedMini404SHA()
	if err != nil {
		t.Fatalf("read the pinned revision: %v", err)
	}
	want := filepath.Join(cache, "mini404-"+sha, QEMUBinaryName)
	writeFile(t, want, []byte("not really a binary"))

	got, err := Resolver{Cache: cache}.QEMUBinary()
	if err != nil {
		t.Fatalf("resolve the emulator: %v", err)
	}
	if got != want {
		t.Errorf("the emulator is %q and should be %q", got, want)
	}
}

func TestTheFirmwareComesFromTheEnvironmentWhenItIsSet(t *testing.T) {
	t.Setenv(EnvFirmware, "/somewhere/else/firmware.bin")

	got, err := Resolver{Cache: t.TempDir()}.FirmwareImage("MK4")
	if err != nil {
		t.Fatalf("resolve the firmware: %v", err)
	}
	if got != "/somewhere/else/firmware.bin" {
		t.Errorf("the firmware is %q and the environment named another path", got)
	}
}

func TestAMissingFirmwareNamesTheBuildScriptAndThePreset(t *testing.T) {
	t.Setenv(EnvFirmware, "")

	_, err := Resolver{Cache: t.TempDir()}.FirmwareImage("MK4")
	if err == nil {
		t.Fatal("an empty cache resolved a firmware image")
	}
	if !errors.Is(err, ErrNoArtifact) {
		t.Errorf("the error is not an ErrNoArtifact: %v", err)
	}
	for _, want := range []string{buildFirmwareScript, "mk4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
}

func TestAnUnpinnedFirmwareImageIsAllowed(t *testing.T) {
	t.Parallel()

	cache := t.TempDir()
	pin := Pin{Model: "MK4", Preset: "mk4", FirmwareRef: "master@abc123"}
	want := filepath.Join(cache, "firmware", pin.FirmwareRef+"-"+pin.Preset, FirmwareImageName)
	writeFile(t, want, []byte("firmware"))

	got, err := Resolver{Cache: cache}.firmwareFor(pin)
	if err != nil {
		t.Fatalf("an image with no pinned checksum was refused: %v", err)
	}
	if got != want {
		t.Errorf("the firmware is %q and should be %q", got, want)
	}
}

func TestAPinnedFirmwareChecksumIsVerifiedInBothDirections(t *testing.T) {
	t.Parallel()

	body := []byte("firmware")
	digest := sha256.Sum256(body)

	t.Run("a matching image resolves", func(t *testing.T) {
		t.Parallel()

		cache := t.TempDir()
		pin := Pin{Model: "MK4", Preset: "mk4", FirmwareRef: "r", FirmwareSHA256: hex.EncodeToString(digest[:])}
		want := filepath.Join(cache, "firmware", "r-mk4", FirmwareImageName)
		writeFile(t, want, body)

		got, err := Resolver{Cache: cache}.firmwareFor(pin)
		if err != nil {
			t.Fatalf("a matching image was refused: %v", err)
		}
		if got != want {
			t.Errorf("the firmware is %q and should be %q", got, want)
		}
	})

	t.Run("a mismatched image names the pin table", func(t *testing.T) {
		t.Parallel()

		cache := t.TempDir()
		pin := Pin{Model: "MK4", Preset: "mk4", FirmwareRef: "r", FirmwareSHA256: strings.Repeat("00", sha256.Size)}
		writeFile(t, filepath.Join(cache, "firmware", "r-mk4", FirmwareImageName), body)

		_, err := Resolver{Cache: cache}.firmwareFor(pin)
		if err == nil {
			t.Fatal("an image whose checksum does not match the pin was accepted")
		}
		if !strings.Contains(err.Error(), PinFile) {
			t.Errorf("the error does not name the pin table: %v", err)
		}
	})
}

func TestTheCacheRootFollowsXDG(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CACHE_HOME is the cache location on Linux only")
	}
	t.Setenv("XDG_CACHE_HOME", "/xdg-cache")

	got, err := CacheDir()
	if err != nil {
		t.Fatalf("locate the cache: %v", err)
	}
	if want := filepath.Join("/xdg-cache", "pcl-mini404"); got != want {
		t.Errorf("the cache root is %q and should be %q", got, want)
	}
}

func writeFile(t *testing.T, path string, body []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
