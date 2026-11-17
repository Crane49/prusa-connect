package mini404

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// The external flash phases and the budget the bootstrap boot gets.
const (
	// PhaseXFlash covers resolving the cached image and copying it for one run.
	PhaseXFlash = "xflash"
	// PhaseXFlashBootstrap covers the first boot on a zeroed image, where the
	// firmware unpacks its resources and formats a filesystem.
	PhaseXFlashBootstrap = "xflash-bootstrap"

	// DefaultBootstrapBudget is what the first boot gets. It is minutes because
	// the guest formats a filesystem and unpacks a resource bundle into it at
	// instruction-level emulation speed, and it is paid once per firmware image
	// on a machine.
	DefaultBootstrapBudget = 15 * time.Minute
	// bootstrapCloseBudget covers the QMP quit and reaping the process once the
	// bootstrap boot is done.
	bootstrapCloseBudget = 30 * time.Second
)

// XFlashRequest asks for the external flash image one firmware needs.
type XFlashRequest struct {
	// QEMUPath is the qemu-system-buddy binary, as Resolver.QEMUBinary returns
	// it. Only read when the cache misses and a boot has to produce the image.
	QEMUPath string
	// Pin is the row the firmware came from. Its preset names the build script
	// in the error a missing resource bundle produces.
	Pin Pin
	// FirmwarePath is the firmware image, as Resolver.FirmwareImage returns it.
	// Its digest is the cache key.
	FirmwarePath string
	// Dir is where the caller's private copy of the image is written. It must
	// exist.
	Dir string
	// Resolver locates the cache. The zero value is the real one.
	Resolver Resolver
	// Budget bounds the bootstrap boot. Zero means DefaultBootstrapBudget.
	Budget time.Duration
	// Logger says when a bootstrap starts and where its result was filed, so a
	// fifteen-minute wait does not look like a hang. Discarded when nil.
	Logger *slog.Logger
}

// XFlash is the external flash image for one run: a private copy of the cached
// image, produced by a bootstrap boot first when the cache misses.
//
// The cache key is the firmware's own digest, because the resources the first
// boot unpacks come out of that image and nothing else. The caller gets a copy
// rather than the cached file: the guest writes to its flash, and a cache the
// callers mutate is a cache whose contents depend on who ran last.
func XFlash(ctx context.Context, req XFlashRequest) (string, error) {
	digest, err := fileDigest(req.FirmwarePath)
	if err != nil {
		return "", fmt.Errorf("phase %q: %w", PhaseXFlash, err)
	}

	root, err := req.Resolver.cacheDir()
	if err != nil {
		return "", fmt.Errorf("phase %q: %w", PhaseXFlash, err)
	}

	cached := filepath.Join(root, "xflash", digest+".bin")
	if _, statErr := os.Stat(cached); statErr != nil {
		if err = bootstrapXFlash(ctx, req, cached); err != nil {
			return "", err
		}
	}

	mine := filepath.Join(req.Dir, "xflash.bin")
	if err = copyFile(cached, mine); err != nil {
		return "", fmt.Errorf("phase %q: %w", PhaseXFlash, err)
	}

	return mine, nil
}

// bootstrapXFlash boots once on a zeroed image with the firmware's own .bbf on
// a USB stick, which is what makes the firmware unpack its resources and format
// the filesystem, then files the result in the cache.
func bootstrapXFlash(ctx context.Context, req XFlashRequest, cached string) error {
	log := req.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	budget := req.Budget
	if budget <= 0 {
		budget = DefaultBootstrapBudget
	}

	bundle := filepath.Join(filepath.Dir(req.FirmwarePath), "firmware.bbf")
	if _, err := os.Stat(bundle); err != nil {
		return fmt.Errorf("phase %q: no firmware.bbf beside %s, and the first boot unpacks its resources "+
			"from one on a USB stick, so run %s %s and keep the .bbf the build produced: %w",
			PhaseXFlashBootstrap, req.FirmwarePath, buildFirmwareScript, req.Pin.Preset, err)
	}

	dir, err := os.MkdirTemp("", "mini404-bootstrap-")
	if err != nil {
		return fmt.Errorf("phase %q: create a bootstrap directory: %w", PhaseXFlashBootstrap, err)
	}

	defer func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			log.WarnContext(ctx, "removing the bootstrap directory failed", "dir", dir, "error", removeErr)
		}
	}()

	stick := filepath.Join(dir, "usb")
	if err = os.Mkdir(stick, 0o700); err != nil {
		return fmt.Errorf("phase %q: create the usb directory: %w", PhaseXFlashBootstrap, err)
	}

	if err = copyFile(bundle, filepath.Join(stick, "firmware.bbf")); err != nil {
		return fmt.Errorf("phase %q: %w", PhaseXFlashBootstrap, err)
	}

	// Connect is off for this boot. It needs no server, and a firmware dialling
	// a port nothing is on would spend the whole budget retrying.
	bank1, bank2, err := WriteEEPROMBanks(dir, ConnectSeed{})
	if err != nil {
		return fmt.Errorf("phase %q: %w", PhaseXFlashBootstrap, err)
	}

	blank := filepath.Join(dir, "xflash.bin")
	if err = zeroFile(blank, XFlashSize); err != nil {
		return fmt.Errorf("phase %q: %w", PhaseXFlashBootstrap, err)
	}

	printer, err := Launch(ctx, Config{
		QEMUPath:     req.QEMUPath,
		Machine:      req.Pin.Machine,
		FirmwarePath: req.FirmwarePath,
		EEPROMBank1:  bank1,
		EEPROMBank2:  bank2,
		XFlashPath:   blank,
		USBDir:       stick,
		Dir:          dir,
		Logger:       req.Logger,
	})
	if err != nil {
		return fmt.Errorf("phase %q: %w", PhaseXFlashBootstrap, err)
	}

	log.InfoContext(ctx, "bootstrapping the external flash, which happens once per firmware image",
		"firmware_ref", req.Pin.FirmwareRef, "budget", budget)

	waiting, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	// PrusaLink answering is the signal, and it is an assumption: it says the
	// network stack is up, and the resource unpack is believed to precede it
	// because the firmware serves its own pages out of the resources partition.
	// Nothing has confirmed that ordering on a live run. A stronger signal read
	// here is the fix, not a sleep at the call site.
	waitErr := WaitFunc(waiting, PhaseXFlashBootstrap, func(ctx context.Context) error {
		return WaitPrusaLink(ctx, printer.HTTPPort())
	})

	closing, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), bootstrapCloseBudget)
	defer closeCancel()

	// The image is only consistent once the guest is down: QEMU holds the drive
	// open and the guest's own filesystem has writes to flush.
	if closeErr := printer.Close(closing); closeErr != nil {
		if waitErr != nil {
			return fmt.Errorf("phase %q: %w (and closing the emulator: %w)",
				PhaseXFlashBootstrap, waitErr, closeErr)
		}

		return fmt.Errorf("phase %q: close the emulator: %w", PhaseXFlashBootstrap, closeErr)
	}

	if waitErr != nil {
		return fmt.Errorf("phase %q: %w (budget %s)", PhaseXFlashBootstrap, waitErr, budget)
	}

	if err = publishToCache(blank, cached); err != nil {
		return fmt.Errorf("phase %q: %w", PhaseXFlashBootstrap, err)
	}

	log.InfoContext(ctx, "cached the bootstrapped external flash", "path", cached)

	return nil
}

// fileDigest is the hex sha256 of a file.
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = f.Close() }()

	sum := sha256.New()
	if _, err = io.Copy(sum, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// copyFile writes src to dst, replacing it.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}

	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()

		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}

	if err = out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}

	return nil
}

// zeroFile creates a file of exactly size zero bytes, which is what MINI404
// itself creates for a drive nobody supplied.
func zeroFile(path string, size int64) error {
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	if err := os.Truncate(path, size); err != nil {
		return fmt.Errorf("size %s to %d bytes: %w", path, size, err)
	}

	return nil
}

// publishToCache installs a produced image at its cache path through a rename,
// so a run that dies mid-copy leaves no half-written image for the next one to
// boot.
func publishToCache(produced, cached string) error {
	if err := os.MkdirAll(filepath.Dir(cached), 0o700); err != nil {
		return fmt.Errorf("create the cache directory: %w", err)
	}

	staged := cached + ".partial"
	if err := copyFile(produced, staged); err != nil {
		return err
	}

	if err := os.Rename(staged, cached); err != nil {
		return fmt.Errorf("install %s: %w", cached, err)
	}

	return nil
}
