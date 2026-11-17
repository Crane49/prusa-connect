// Command firmwareprinter boots one emulated printer on real Prusa firmware and
// attaches it to a prusa-connect server already running on this machine.
//
// It is the developer's printer. The lane in test/firmware asserts things and
// then tears everything down; this boots the same machine from the same
// artefacts and leaves it up, so the API and the frontend can be poked at with
// real firmware on the other end of the socket.
//
// The server address is seeded into the printer's EEPROM before the guest
// boots, because the printer dials the server and never the other way round.
// A server that is not listening yet is reported and then ignored: the
// firmware retries, so it is fine to start one afterwards.
//
// Nothing is built here. A missing artefact is an error naming the script that
// produces it. The first run for a firmware image also bootstraps its external
// flash, which is a quarter of an hour and happens once.
//
//	firmwareprinter
//	firmwareprinter -server 127.0.0.1:8081 -model MK4 -verbose
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prusa3d/prusa-connect/tools/mini404"
)

// The defaults and the budgets.
const (
	// defaultServer is the device-facing listener config/config.yaml offers for
	// core.printer.addr. The shipped value is empty, which is the listener
	// switched off; :443 is what the file's own example and docs/certificates
	// spell, so it is what a developer's server is most likely on.
	defaultServer = "127.0.0.1:443"

	// defaultToken is the Connect token seeded into the EEPROM. Holding a token
	// is what makes the boot headless: the firmware skips the on-screen
	// registration and dials GET /p/ws straight away. The value is arbitrary
	// because the xbuddy connector admits any token while its auth mode is
	// none, and it is twenty characters because that is the width of the
	// firmware's own field.
	//nolint:gosec // G101: not a credential. It is the arbitrary string the guest presents to a
	// server that admits any token, and it is in the source so a developer can recognise it.
	defaultToken = "pcl-firmwareprinter1"

	// guestGateway is the host as the guest sees it. QEMU's user-mode network
	// puts the host behind this address, so a server on the host's loopback is
	// reachable at it and nowhere else.
	guestGateway = "10.0.2.2"

	// defaultBudget covers a warm boot on a cached external flash. The
	// bootstrap that produces that flash has its own budget, because it is a
	// different order of wait.
	defaultBudget = 12 * time.Minute
	// progressPeriod is how often the wait says which phase it is in, so a boot
	// that takes minutes does not look like a hang.
	progressPeriod = 30 * time.Second
	// probeBudget bounds the one dial that reports whether a server is there.
	probeBudget = 2 * time.Second
	// closeBudget covers the QMP quit and reaping the QEMU process.
	closeBudget = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "firmwareprinter:", err)
		os.Exit(1)
	}
}

func run() error {
	server := flag.String("server", defaultServer,
		"host:port of the running server's device-facing listener, core.printer.addr")
	model := flag.String("model", "MK4", "printer model to boot, matched against the pin table")
	token := flag.String("token", defaultToken, "Connect token seeded into the EEPROM, at most 20 characters")
	budget := flag.Duration("budget", defaultBudget, "how long the boot may take")
	bootstrap := flag.Duration("bootstrap-budget", mini404.DefaultBootstrapBudget,
		"how long the one-off external flash bootstrap may take")
	dir := flag.String("dir", "", "scratch directory for flash images and logs, created when empty")
	keep := flag.Bool("keep", false, "keep the scratch directory")
	verbose := flag.Bool("verbose", false, "stream the emulator's own output")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level(*verbose)}))

	host, port, err := splitServer(*server)
	if err != nil {
		return err
	}

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

	// Said before the boot rather than after it, because a developer who forgot
	// to start the server has ten minutes to start it while this boots.
	reportServer(ctx, log, *server, host, port)

	seed := mini404.ConnectSeed{Host: seedHost(host), Port: port, TLS: false, Token: *token, Enabled: true}
	bank1, bank2, err := mini404.WriteEEPROMBanks(scratch, seed)
	if err != nil {
		return err
	}

	xflash, err := mini404.XFlash(ctx, mini404.XFlashRequest{
		QEMUPath:     qemu,
		Pin:          pin,
		FirmwarePath: firmware,
		Dir:          scratch,
		Budget:       *bootstrap,
		Logger:       log,
	})
	if err != nil {
		return err
	}

	cfg := mini404.Config{
		QEMUPath:     qemu,
		Machine:      pin.Machine,
		FirmwarePath: firmware,
		EEPROMBank1:  bank1,
		EEPROMBank2:  bank2,
		XFlashPath:   xflash,
		Dir:          scratch,
		Logger:       log,
	}
	if *verbose {
		cfg.ConsoleLog = os.Stderr
	}

	log.Info("booting",
		"model", pin.Model,
		"machine", pin.Machine,
		"firmware_ref", pin.FirmwareRef,
		"connect", seed.Host+":"+strconv.Itoa(int(seed.Port)),
		"scratch", scratch)

	started := time.Now()

	printer, err := mini404.Launch(ctx, cfg)
	if err != nil {
		return err
	}

	shutdown := func() error {
		closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeBudget)
		defer cancel()

		return printer.Close(closing)
	}

	if waitErr := awaitPrusaLink(ctx, log, printer, *budget); waitErr != nil {
		fmt.Fprintln(os.Stderr, printer.OutputTail())

		return errors.Join(waitErr, shutdown())
	}

	fmt.Fprintf(os.Stdout, "model            %s (%s)\n", pin.Model, pin.Machine)
	fmt.Fprintf(os.Stdout, "firmware         %s\n", pin.FirmwareRef)
	fmt.Fprintf(os.Stdout, "prusalink        http://127.0.0.1:%d\n", printer.HTTPPort())
	fmt.Fprintf(os.Stdout, "script console   %s\n", printer.Console().Addr())
	fmt.Fprintf(os.Stdout, "dials            %s:%d, token %s\n", seed.Host, seed.Port, seed.Token)
	fmt.Fprintf(os.Stdout, "booted in        %s\n", time.Since(started).Round(time.Millisecond))
	fmt.Fprintf(os.Stdout, "scratch          %s\n", scratch)
	fmt.Fprintf(os.Stdout, "\nThe printer is up. Press Ctrl-C to stop it.\n")

	// The guest outliving this process is the one failure mode that costs a
	// developer their afternoon, so a QEMU that dies on its own ends the wait
	// as surely as the signal does.
	select {
	case <-ctx.Done():
		log.Info("stopping")
	case <-printer.Wait():
		log.Warn("the emulator exited on its own")
		fmt.Fprintln(os.Stderr, printer.OutputTail())
	}

	if err = shutdown(); err != nil {
		return fmt.Errorf("close the printer: %w", err)
	}

	return nil
}

// splitServer reads the device-facing address the firmware is told to dial.
func splitServer(address string) (host string, port uint16, err error) {
	hostPart, portPart, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("read -server %q as host:port: %w", address, err)
	}

	number, err := strconv.ParseUint(portPart, 10, 16)
	if err != nil {
		return "", 0, fmt.Errorf("read the port in -server %q: %w", address, err)
	}

	if number == 0 {
		return "", 0, fmt.Errorf("-server %q names port 0, and the port has to be known "+
			"before the guest boots because it is written into the EEPROM", address)
	}

	return hostPart, uint16(number), nil
}

// seedHost is the host written into the EEPROM for the one the developer named.
//
// A server on this machine is reached from inside the guest at the user-mode
// network's gateway, never at 127.0.0.1, which inside the guest is the guest.
// Any other host is written as given, so a server on the network works too.
func seedHost(host string) string {
	if host == "" || host == "localhost" {
		return guestGateway
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return guestGateway
	}

	return host
}

// reportServer says whether anything is listening where the guest is about to
// dial. It never fails the run: the firmware retries for as long as it is up,
// so a server started later is picked up without a reboot.
func reportServer(ctx context.Context, log *slog.Logger, address, host string, port uint16) {
	target := address
	if host == "" {
		target = net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	}

	probing, cancel := context.WithTimeout(ctx, probeBudget)
	defer cancel()

	var dialer net.Dialer

	conn, err := dialer.DialContext(probing, "tcp", target)
	if err != nil {
		log.WarnContext(ctx, "nothing is listening where the printer will dial, which is fine if the server "+
			"is started before the boot finishes", "server", target, "error", err)

		return
	}

	if closeErr := conn.Close(); closeErr != nil {
		log.WarnContext(ctx, "closing the probe connection failed", "server", target, "error", closeErr)
	}

	log.InfoContext(ctx, "the server is listening", "server", target)
}

// awaitPrusaLink waits for the guest's HTTP stack, reporting the phase while it
// waits so a long boot is legible.
func awaitPrusaLink(ctx context.Context, log *slog.Logger, printer *mini404.Printer, budget time.Duration) error {
	waiting, cancel := context.WithTimeout(ctx, budget)
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

	return mini404.WaitPrusaLink(waiting, printer.HTTPPort())
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
