// Command mini404smoke boots one emulated printer from the pinned artefacts and
// waits for its HTTP stack to answer.
//
// It is the check that the substrate works: artefacts resolve, the invocation is
// accepted, the firmware boots on empty flash, the script console answers, and
// teardown is clean. It asserts nothing about the protocol. The lane that does
// that lives elsewhere and uses the same package.
//
// Nothing is built here. A missing artefact is an error naming the script that
// produces it.
//
//	mini404smoke
//	mini404smoke -model MK4 -budget 15m -verbose
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/prusa3d/prusa-connect/tools/mini404"
)

// The budgets.
const (
	// inertConnectPort is written into the seeded EEPROM so the firmware
	// has a complete Connect configuration. Nothing listens on it; the
	// smoke proves the boot, not the dial.
	inertConnectPort = 8080

	// defaultBudget covers a cold boot. The first run on a zero-filled xflash
	// has the firmware format the filesystem before it serves anything, and
	// instruction-level emulation makes that minutes rather than seconds.
	defaultBudget = 10 * time.Minute
	// progressPeriod is how often the wait says which phase it is in, so a run
	// that is going to take ten minutes does not look like a hang.
	progressPeriod = 30 * time.Second
	// consoleBudget is one script console round trip once the stack is up.
	consoleBudget = 30 * time.Second
	// closeBudget covers the QMP quit and the process reap.
	closeBudget = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mini404smoke:", err)
		os.Exit(1)
	}
}

func run() error {
	model := flag.String("model", "MK4", "printer model to boot, matched against the pin table")
	budget := flag.Duration("budget", defaultBudget, "how long the boot may take")
	dir := flag.String("dir", "", "scratch directory for flash images and logs, created when empty")
	keep := flag.Bool("keep", false, "keep the scratch directory")
	verbose := flag.Bool("verbose", false, "stream the emulator's own output")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level(*verbose)}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pin, err := mini404.PinFor(*model)
	if err != nil {
		return err
	}
	resolver := mini404.Resolver{Logger: log}
	qemu, err := resolver.QEMUBinary()
	if err != nil {
		return err
	}
	firmware, err := resolver.FirmwareImage(pin.Model)
	if err != nil {
		return err
	}

	scratch, cleanup, err := scratchDir(*dir, *keep, log)
	if err != nil {
		return err
	}
	defer cleanup()

	cfg := mini404.Config{
		QEMUPath:     qemu,
		Machine:      pin.Machine,
		FirmwarePath: firmware,
		Dir:          scratch,
		Logger:       log,
	}
	if cfg.EEPROMBank1, cfg.EEPROMBank2, cfg.XFlashPath, err = blankMedia(scratch); err != nil {
		return err
	}

	// A blank EEPROM parks the firmware in the first-run wizard with no way
	// to click through headless, so the smoke seed skips it. The Connect
	// values are inert: nothing listens on the seeded port.
	seed := mini404.ConnectSeed{Host: "10.0.2.2", Port: inertConnectPort, Enabled: true}
	if cfg.EEPROMBank1, cfg.EEPROMBank2, err = mini404.WriteEEPROMBanks(scratch, seed); err != nil {
		return err
	}

	// The bbf beside the firmware image carries the GUI resources the
	// firmware unpacks into xflash on first boot. Offering it on the usb
	// stick is what makes a blank xflash reach the home screen.
	if bbf := strings.TrimSuffix(firmware, ".bin") + ".bbf"; fileExists(bbf) {
		usb := filepath.Join(scratch, "usb")
		if err = os.MkdirAll(usb, 0o750); err != nil {
			return err
		}
		if err = copyFile(bbf, filepath.Join(usb, "firmware.bbf")); err != nil {
			return err
		}
		cfg.USBDir = usb
	}
	if *verbose {
		cfg.ConsoleLog = os.Stderr
	}

	log.Info("booting",
		"model", pin.Model,
		"machine", pin.Machine,
		"firmware_ref", pin.FirmwareRef,
		"qemu", qemu,
		"scratch", scratch)

	started := time.Now()
	printer, err := mini404.Launch(ctx, cfg)
	if err != nil {
		return err
	}
	launched := time.Since(started)

	shutdown := func() error {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeBudget)
		defer cancel()
		return printer.Close(closeCtx)
	}

	if waitErr := awaitPrusaLink(ctx, log, printer, *budget); waitErr != nil {
		fmt.Fprintln(os.Stderr, printer.OutputTail())
		return errors.Join(waitErr, shutdown())
	}
	booted := time.Since(started)

	shot := filepath.Join(scratch, "lcd.png")
	shotCtx, cancel := context.WithTimeout(ctx, consoleBudget)
	defer cancel()
	if shotErr := printer.Console().Screenshot(shotCtx, shot); shotErr != nil {
		fmt.Fprintln(os.Stderr, printer.OutputTail())
		return errors.Join(fmt.Errorf("take a screenshot: %w", shotErr), shutdown())
	}
	screenshot := time.Since(started) - booted

	if closeErr := shutdown(); closeErr != nil {
		return fmt.Errorf("close the printer: %w", closeErr)
	}

	fmt.Fprintf(os.Stdout, "model            %s (%s)\n", pin.Model, pin.Machine)
	fmt.Fprintf(os.Stdout, "firmware         %s\n", pin.FirmwareRef)
	fmt.Fprintf(os.Stdout, "http port        %d\n", printer.HTTPPort())
	fmt.Fprintf(os.Stdout, "launch           %s\n", launched.Round(time.Millisecond))
	fmt.Fprintf(os.Stdout, "prusalink up     %s\n", booted.Round(time.Millisecond))
	fmt.Fprintf(os.Stdout, "screenshot       %s -> %s\n", screenshot.Round(time.Millisecond), shot)
	fmt.Fprintf(os.Stdout, "total            %s\n", time.Since(started).Round(time.Millisecond))
	return nil
}

// awaitPrusaLink waits for the guest's HTTP stack, reporting the phase while it
// waits so a long cold boot is legible.
func awaitPrusaLink(ctx context.Context, log *slog.Logger, printer *mini404.Printer, budget time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(progressPeriod)
		defer ticker.Stop()
		start := time.Now()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				log.InfoContext(ctx, "waiting", "phase", "prusalink-http",
					"elapsed", time.Since(start).Round(time.Second), "budget", budget)
			}
		}
	}()

	select {
	case <-printer.Wait():
		return errors.New("qemu exited before the HTTP stack answered")
	default:
	}
	return mini404.WaitPrusaLink(waitCtx, printer.HTTPPort())
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	//nolint:gosec // G703: dst is built from this tool's own flags.
	return os.WriteFile(dst, data, 0o600)
}

// blankMedia writes the three empty flash images. Zero-filled is what MINI404
// itself creates when a drive is not supplied
// (hw/arm/prusa/stm32_common/stm32_common.c, create_if_not_exist), so the
// firmware initialises them the same way it would on a first boot.
func blankMedia(dir string) (bank1, bank2, xflash string, err error) {
	media := []struct {
		path *string
		name string
		size int64
	}{
		{&bank1, "eeprom-bank1.bin", mini404.EEPROMBankSize},
		{&bank2, "eeprom-bank2.bin", mini404.EEPROMBankSize},
		{&xflash, "xflash.bin", mini404.XFlashSize},
	}
	for _, m := range media {
		path := filepath.Join(dir, m.name)
		if writeErr := os.WriteFile(path, nil, 0o600); writeErr != nil {
			return "", "", "", fmt.Errorf("create %s: %w", path, writeErr)
		}
		if truncErr := os.Truncate(path, m.size); truncErr != nil {
			return "", "", "", fmt.Errorf("size %s to %d bytes: %w", path, m.size, truncErr)
		}
		*m.path = path
	}
	return bank1, bank2, xflash, nil
}

func scratchDir(dir string, keep bool, log *slog.Logger) (string, func(), error) {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", nil, fmt.Errorf("create %s: %w", dir, err)
		}
		return dir, func() {}, nil
	}

	// Short prefix on purpose: the QMP socket path underneath it has to stay
	// inside the unix socket path limit.
	created, err := os.MkdirTemp("", "mini404-")
	if err != nil {
		return "", nil, fmt.Errorf("create a scratch directory: %w", err)
	}
	if keep {
		return created, func() { log.Info("kept the scratch directory", "dir", created) }, nil
	}
	return created, func() {
		if removeErr := os.RemoveAll(created); removeErr != nil {
			log.Warn("removing the scratch directory failed", "dir", created, "error", removeErr)
		}
	}, nil
}

func level(verbose bool) slog.Level {
	if verbose {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}
