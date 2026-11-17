package mini404

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// The environment overrides and the scripts that fill the cache. Both overrides
// exist for the same reason: a developer bisecting a firmware bug wants their
// own build in the loop without editing a pin table.
const (
	// EnvQEMU points at a qemu-system-buddy binary, bypassing the cache and the
	// pinned MINI404 revision.
	EnvQEMU = "PCL_QEMU"
	// EnvFirmware points at a firmware image, bypassing the cache and the
	// checksum in the pin table.
	EnvFirmware = "PCL_FIRMWARE_BIN"

	buildQEMUScript     = "scripts/build-mini404.sh"
	buildFirmwareScript = "scripts/build-firmware.sh"

	// QEMUBinaryName is the target the buddy-softmmu build produces.
	QEMUBinaryName = "qemu-system-buddy"
	// FirmwareImageName is the file scripts/build-firmware.sh copies into the
	// cache.
	FirmwareImageName = "firmware.bin"

	// PinFile is the pin table, embedded so a resolution failure names a
	// version even when the checkout is elsewhere.
	PinFile = "knowngood.json"
)

//go:embed knowngood.json
var pinTable []byte

// ErrNoArtifact is a resolution that found nothing on disk. It carries the
// script that produces the missing file in its message.
var ErrNoArtifact = errors.New("the pinned artefact is not in the cache")

// Pin is one row of the pin table: a printer model, the machine that emulates
// it, and the two artefact versions verified together.
type Pin struct {
	// Model is the printer model as internal/printer/profile spells it.
	Model string `json:"model"`
	// Machine is the MINI404 machine name passed to -machine.
	Machine string `json:"machine"`
	// Preset is the Prusa-Firmware-Buddy build preset.
	Preset string `json:"preset"`
	// FirmwareRef is the firmware revision the preset was built from, in
	// branch@sha form.
	FirmwareRef string `json:"firmware_ref"`
	// FirmwareSHA256 is the hex digest of the built image. Empty means the
	// image is not pinned yet, which is allowed and logged.
	FirmwareSHA256 string `json:"firmware_sha256"`
	// Mini404SHA is the MINI404 revision the emulator is built from. Every row
	// carries the same value: one build serves the whole table.
	Mini404SHA string `json:"mini404_sha"`
}

// LoadPins reads the embedded pin table.
func LoadPins() ([]Pin, error) {
	var pins []Pin
	if err := json.Unmarshal(pinTable, &pins); err != nil {
		return nil, fmt.Errorf("parse %s: %w", PinFile, err)
	}
	if len(pins) == 0 {
		return nil, fmt.Errorf("%s holds no rows", PinFile)
	}
	return pins, nil
}

// PinFor is the row for one printer model, matched without regard to case.
func PinFor(model string) (Pin, error) {
	pins, err := LoadPins()
	if err != nil {
		return Pin{}, err
	}
	for _, p := range pins {
		if strings.EqualFold(p.Model, model) {
			return p, nil
		}
	}
	return Pin{}, fmt.Errorf("%s has no row for model %q", PinFile, model)
}

// Resolver finds the pinned build artefacts on disk. The zero value resolves
// against the real cache and logs nothing.
type Resolver struct {
	// Logger records which artefact was taken and why a check was skipped.
	// Discarded when nil.
	Logger *slog.Logger
	// Cache overrides the artefact cache root. Empty means the XDG location.
	Cache string
}

// CacheDir is the artefact cache root, ${XDG_CACHE_HOME:-~/.cache}/pcl-mini404.
func CacheDir() (string, error) { return Resolver{}.cacheDir() }

// QEMUBinary is the qemu-system-buddy built from the pinned MINI404 revision.
// It never builds anything: a miss is an error naming the build script.
func (r Resolver) QEMUBinary() (string, error) {
	log := r.logger()

	if override := os.Getenv(EnvQEMU); override != "" {
		log.Info("using the emulator from the environment, so the pinned revision is not in force",
			"env", EnvQEMU, "path", override)
		return override, nil
	}

	sha, err := pinnedMini404SHA()
	if err != nil {
		return "", err
	}
	cache, err := r.cacheDir()
	if err != nil {
		return "", err
	}

	path := filepath.Join(cache, "mini404-"+sha, QEMUBinaryName)
	if statErr := regularFile(path); statErr != nil {
		return "", fmt.Errorf(
			"%w: no %s for MINI404 %s at %s, so run %s (or set %s): %w",
			ErrNoArtifact, QEMUBinaryName, sha, path, buildQEMUScript, EnvQEMU, statErr)
	}
	log.Debug("resolved the emulator", "path", path, "mini404_sha", sha)
	return path, nil
}

// FirmwareImage is the image built for one model's pin row. A row that carries
// a checksum is verified; an empty checksum is allowed and logged, because the
// first build is what produces the value.
func (r Resolver) FirmwareImage(model string) (string, error) {
	if override := os.Getenv(EnvFirmware); override != "" {
		r.logger().Info("using the firmware from the environment, so the pinned checksum is not verified",
			"env", EnvFirmware, "path", override)
		return override, nil
	}

	pin, err := PinFor(model)
	if err != nil {
		return "", err
	}
	return r.firmwareFor(pin)
}

func (r Resolver) firmwareFor(pin Pin) (string, error) {
	log := r.logger()

	cache, err := r.cacheDir()
	if err != nil {
		return "", err
	}

	path := filepath.Join(cache, "firmware", pin.FirmwareRef+"-"+pin.Preset, FirmwareImageName)
	if statErr := regularFile(path); statErr != nil {
		return "", fmt.Errorf(
			"%w: no %s for %s at %s, so run %s %s (or set %s): %w",
			ErrNoArtifact, FirmwareImageName, pin.FirmwareRef, path,
			buildFirmwareScript, pin.Preset, EnvFirmware, statErr)
	}

	if pin.FirmwareSHA256 == "" {
		log.Warn("the firmware image is not pinned yet, so its contents are unverified",
			"model", pin.Model, "firmware_ref", pin.FirmwareRef, "pin_file", PinFile)
		return path, nil
	}

	digest, err := fileSHA256(path)
	if err != nil {
		return "", err
	}
	if digest != pin.FirmwareSHA256 {
		return "", fmt.Errorf(
			"%s has sha256 %s and %s pins %s for %s, so one of the two is stale",
			path, digest, PinFile, pin.FirmwareSHA256, pin.Model)
	}
	log.Debug("resolved the firmware", "path", path, "sha256", digest)
	return path, nil
}

func (r Resolver) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.New(slog.DiscardHandler)
}

func (r Resolver) cacheDir() (string, error) {
	if r.Cache != "" {
		return r.Cache, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate the cache directory: %w", err)
	}
	return filepath.Join(base, "pcl-mini404"), nil
}

// pinnedMini404SHA is the emulator revision the whole table shares. Two rows
// disagreeing would mean two emulator builds, which the cache layout and the
// build script do not offer.
func pinnedMini404SHA() (string, error) {
	pins, err := LoadPins()
	if err != nil {
		return "", err
	}
	sha := pins[0].Mini404SHA
	if sha == "" {
		return "", fmt.Errorf("%s row %q carries no mini404_sha", PinFile, pins[0].Model)
	}
	for _, p := range pins[1:] {
		if p.Mini404SHA != sha {
			return "", fmt.Errorf(
				"%s pins MINI404 %s for %s and %s for %s, and one build serves the whole table",
				PinFile, sha, pins[0].Model, p.Mini404SHA, p.Model)
		}
	}
	return sha, nil
}

func regularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, copyErr := io.Copy(h, f); copyErr != nil {
		return "", fmt.Errorf("hash %s: %w", path, copyErr)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
