package mini404

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// The budgets and sizes. All wall-clock: a QEMU process is not in a synctest
// bubble.
const (
	// DefaultICount is the determinism setting the vendor harness runs these
	// machines at. Puppy boards want 5, which is why it is a Config field.
	// Earlier research.
	DefaultICount = 2

	// EEPROMBankSize is one of the two pflash banks the Buddy boards carry.
	EEPROMBankSize = 8 << 10
	// XFlashSize is the external SPI flash exposed as mtd index 0.
	XFlashSize = 8 << 20

	// earlyExitWindow is how long a started QEMU has to stay alive before the
	// launch counts as successful. A port reserved and closed is a port another
	// process can take in the gap before QEMU binds it, and QEMU answers that
	// by exiting immediately. Anything within this window is retried with fresh
	// ports rather than reported as a broken machine.
	earlyExitWindow = 2 * time.Second
	// launchAttempts bounds the retry above.
	launchAttempts = 3

	// quitBudget is how long Close waits for QMP to bring the guest down before
	// it reaches for SIGKILL.
	quitBudget = 5 * time.Second
	// waitDelay is the grace QEMU gets to flush its pipes after a kill, which
	// is what keeps a stuck writer from holding cmd.Wait open forever.
	waitDelay = 5 * time.Second

	// outputTailBytes is the size of the in-memory ring the last of QEMU's
	// stderr goes into. The full stream is on disk in Config.Dir.
	outputTailBytes = 64 << 10

	// qmpSocketPathLimit is the portable ceiling on a unix socket path. Linux
	// allows 108 bytes including the terminator; anything longer is silently
	// truncated by bind, so it is refused here where the error can name the
	// directory that caused it.
	qmpSocketPathLimit = 100
)

// ErrClosed is a call on a printer that has already been closed.
var ErrClosed = errors.New("the printer is closed")

// Config is one emulated printer. The zero value is not runnable: the machine,
// the firmware image, the flash images and a runtime directory are all
// required, because none of them has a defensible default.
type Config struct {
	// QEMUPath is the qemu-system-buddy binary. Resolve it with QEMUBinary.
	QEMUPath string
	// Machine is the MINI404 machine name, such as prusa-mk4-034. It comes off
	// the pin table, not out of a test.
	Machine string
	// FirmwarePath is the firmware image passed as -kernel, either a .bin or an
	// ELF. It must be a noboot variant: MINI404 loads it at the reset vector
	// with no bootloader in front of it.
	FirmwarePath string
	// EEPROMBank1 and EEPROMBank2 are the two 8 KiB pflash images. The
	// firmware's persistent store is journalled across the pair.
	EEPROMBank1 string
	EEPROMBank2 string
	// XFlashPath is the 8 MiB external flash image exposed as mtd index 0.
	XFlashPath string
	// USBDir is a host directory exposed to the guest as a FAT usb stick.
	// Empty means no stick is attached.
	USBDir string
	// ICount is the instruction-count rate. Zero means DefaultICount.
	ICount int
	// Dir holds the runtime files: the QMP socket and the QEMU output log. It
	// must exist and be short enough for a unix socket path.
	Dir string
	// ConsoleLog receives QEMU's own stdout and stderr as it arrives, which is
	// the firmware's serial output among other things. Nil discards it; the
	// same bytes go to Dir/qemu.log either way.
	ConsoleLog io.Writer
	// Logger records launch decisions, retries and teardown. Discarded when
	// nil.
	Logger *slog.Logger
}

// ports are the two host ports one printer reserves: the PrusaLink forward and
// the script console chardev.
type ports struct {
	http      int
	scriptcon int
}

// Printer is one running emulator. It is safe for concurrent use.
type Printer struct {
	cmd   *exec.Cmd
	log   *slog.Logger
	ports ports

	console *ScriptConsole
	qmp     *QMP

	tail *ringWriter
	out  *os.File

	done chan struct{}
	// waitErr is written once, before done is closed, so a read after done is
	// closed needs no lock.
	waitErr error

	closeOnce sync.Once
	closeErr  error
}

// Launch starts one emulator and returns as soon as the process is up. It does
// not wait for the firmware to boot: that is what the Wait probes in
// readiness.go are for.
func Launch(ctx context.Context, cfg Config) (*Printer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	var errs []error
	for attempt := 1; attempt <= launchAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		p, err := start(ctx, cfg, log)
		if err != nil {
			errs = append(errs, fmt.Errorf("attempt %d: %w", attempt, err))
			continue
		}

		alive, err := p.settled(ctx)
		if alive {
			log.InfoContext(ctx, "mini404 launched",
				"machine", cfg.Machine,
				"http_port", p.ports.http,
				"scriptcon_port", p.ports.scriptcon,
				"attempt", attempt)
			return p, nil
		}

		// The process is already gone, so Close has nothing to kill; it still
		// closes the log file and the tail.
		_ = p.Close(ctx)
		errs = append(errs, fmt.Errorf("attempt %d: %w", attempt, err))
		log.WarnContext(ctx, "mini404 exited during startup, retrying with fresh ports",
			"machine", cfg.Machine,
			"http_port", p.ports.http,
			"scriptcon_port", p.ports.scriptcon,
			"attempt", attempt,
			"error", err,
			"output_tail", p.OutputTail())
	}
	return nil, fmt.Errorf("mini404 did not stay up in %d attempts: %w", launchAttempts, errors.Join(errs...))
}

func (cfg Config) validate() error {
	required := []struct {
		name  string
		value string
	}{
		{"QEMUPath", cfg.QEMUPath},
		{"Machine", cfg.Machine},
		{"FirmwarePath", cfg.FirmwarePath},
		{"EEPROMBank1", cfg.EEPROMBank1},
		{"EEPROMBank2", cfg.EEPROMBank2},
		{"XFlashPath", cfg.XFlashPath},
		{"Dir", cfg.Dir},
	}
	for _, r := range required {
		if r.value == "" {
			return fmt.Errorf("mini404: Config.%s is required", r.name)
		}
	}
	if n := len(qmpSocketPath(cfg.Dir)); n > qmpSocketPathLimit {
		return fmt.Errorf(
			"mini404: the QMP socket path is %d bytes and the limit is %d, so Config.Dir %q is too deep",
			n, qmpSocketPathLimit, cfg.Dir)
	}
	return nil
}

// start reserves ports, spawns QEMU and begins reaping it. A returned Printer
// may already have exited: settled is what decides that.
func start(ctx context.Context, cfg Config, log *slog.Logger) (*Printer, error) {
	prt, err := reservePorts(ctx)
	if err != nil {
		return nil, err
	}

	out, err := os.Create(filepath.Join(cfg.Dir, "qemu.log"))
	if err != nil {
		return nil, fmt.Errorf("create the qemu log: %w", err)
	}

	tail := &ringWriter{limit: outputTailBytes}
	sinks := []io.Writer{out, tail}
	if cfg.ConsoleLog != nil {
		sinks = append(sinks, cfg.ConsoleLog)
	}
	stream := io.MultiWriter(sinks...)

	args := qemuArgs(cfg, prt)
	// The context is stripped on purpose. Cancellation has to run the QMP quit
	// before anything signals the process, and Close owns that order, so a
	// cancelled launch context must not reach exec's own killer.
	//nolint:gosec // the emulator path is operator input, not request input
	cmd := exec.CommandContext(context.WithoutCancel(ctx), cfg.QEMUPath, args...)
	cmd.Stdout = stream
	cmd.Stderr = stream
	cmd.WaitDelay = waitDelay

	if startErr := cmd.Start(); startErr != nil {
		_ = out.Close()
		return nil, fmt.Errorf("start %s: %w", cfg.QEMUPath, startErr)
	}
	log.DebugContext(ctx, "qemu started", "argv", append([]string{cfg.QEMUPath}, args...))

	p := &Printer{
		cmd:   cmd,
		log:   log,
		ports: prt,
		tail:  tail,
		out:   out,
		done:  make(chan struct{}),
	}
	p.console = newScriptConsole(net.JoinHostPort("127.0.0.1", strconv.Itoa(prt.scriptcon)), log)
	p.qmp = newQMP(qmpSocketPath(cfg.Dir), log)

	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// settled reports whether the process survived earlyExitWindow. A false comes
// with the reason.
func (p *Printer) settled(ctx context.Context) (bool, error) {
	timer := time.NewTimer(earlyExitWindow)
	defer timer.Stop()

	select {
	case <-p.done:
		return false, fmt.Errorf("qemu exited within %s: %w", earlyExitWindow, p.waitErr)
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return true, nil
	}
}

// qemuArgs is the whole invocation, in a fixed order so a test can assert it.
//
// The netdev id must be exactly mini-eth. The SoC's Ethernet model looks the
// backend up by that name, and any other id gives a guest with the link down
// and no error anywhere. The guest reaches the host at 10.0.2.2, which is the
// slirp gateway.
func qemuArgs(cfg Config, prt ports) []string {
	icount := cfg.ICount
	if icount == 0 {
		icount = DefaultICount
	}

	args := []string{
		"-machine", cfg.Machine,
		"-kernel", cfg.FirmwarePath,
		"-display", "none",
		"-icount", strconv.Itoa(icount),
		// The SOF interrupt fires per USB frame and costs more than it buys on
		// a headless run.
		"-global", "STM32F4xx-usb.disable_sof_interrupt=true",
		"-netdev", fmt.Sprintf("user,id=mini-eth,hostfwd=tcp:127.0.0.1:%d-:80", prt.http),
		"-drive", "if=pflash,index=0,format=raw,file=" + cfg.EEPROMBank1,
		"-drive", "if=pflash,index=1,format=raw,file=" + cfg.EEPROMBank2,
		"-drive", "if=mtd,index=0,format=raw,file=" + cfg.XFlashPath,
	}
	if cfg.USBDir != "" {
		args = append(args,
			"-drive", "id=usbstick,format=raw,file=fat:rw:"+cfg.USBDir,
			"-device", "usb-storage,drive=usbstick",
		)
	}
	return append(args,
		"-chardev", fmt.Sprintf("socket,id=p404-scriptcon,host=127.0.0.1,port=%d,server=on,wait=off", prt.scriptcon),
		"-global", "p404-scriptcon.no_echo=on",
		"-qmp", "unix:"+qmpSocketPath(cfg.Dir)+",server=on,wait=off",
	)
}

// qmpSocketPath is a unix socket address and not a file path: QEMU reads it
// after `unix:` and the dialer hands it to AF_UNIX, and both take a forward
// slash on every host, where filepath.Join on Windows would write a backslash
// into the argv.
func qmpSocketPath(dir string) string { return path.Join(filepath.ToSlash(dir), "qmp.sock") }

// reservePorts takes two loopback ports by binding and closing. There is no
// way to hand a listening socket to QEMU, so the gap between the close and
// QEMU's bind is unavoidable; Launch retries on the loss.
func reservePorts(ctx context.Context) (ports, error) {
	var cfg net.ListenConfig

	var prt ports
	for _, target := range []*int{&prt.http, &prt.scriptcon} {
		l, listenErr := cfg.Listen(ctx, "tcp4", "127.0.0.1:0")
		if listenErr != nil {
			return ports{}, fmt.Errorf("reserve a loopback port: %w", listenErr)
		}
		addr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			_ = l.Close()
			return ports{}, fmt.Errorf("a tcp4 listener reported a %T address", l.Addr())
		}
		*target = addr.Port
		if closeErr := l.Close(); closeErr != nil {
			return ports{}, fmt.Errorf("release the reserved port: %w", closeErr)
		}
	}
	return prt, nil
}

// Console is the script console client. It connects on first use.
func (p *Printer) Console() *ScriptConsole { return p.console }

// QMP is the monitor client. It connects on first use.
func (p *Printer) QMP() *QMP { return p.qmp }

// HTTPPort is the loopback port forwarded to the guest's port 80, which is
// PrusaLink.
func (p *Printer) HTTPPort() int { return p.ports.http }

// Wait is closed when the QEMU process has exited.
func (p *Printer) Wait() <-chan struct{} { return p.done }

// OutputTail is the last of QEMU's combined output, for a diagnostic. The whole
// stream is in Dir/qemu.log.
func (p *Printer) OutputTail() string { return p.tail.String() }

// Close brings the guest down through QMP, kills whatever is left, and waits
// for the process. It is idempotent.
func (p *Printer) Close(ctx context.Context) error {
	p.closeOnce.Do(func() { p.closeErr = p.shutdown(ctx) })
	return p.closeErr
}

func (p *Printer) shutdown(ctx context.Context) error {
	defer func() {
		p.console.Close()
		p.qmp.Close()
		if err := p.out.Close(); err != nil {
			p.log.WarnContext(ctx, "closing the qemu log failed", "error", err)
		}
	}()

	select {
	case <-p.done:
		return p.exitErr()
	default:
	}

	quitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), quitBudget)
	defer cancel()
	if err := p.qmp.Quit(quitCtx); err != nil {
		p.log.WarnContext(ctx, "qmp quit failed, killing qemu", "error", err)
	}

	select {
	case <-p.done:
		return p.exitErr()
	case <-quitCtx.Done():
	}

	if err := p.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill qemu: %w", err)
	}
	<-p.done
	return p.exitErr()
}

// exitErr drops the exit status of a process we asked to stop. A guest brought
// down by QMP quit or by a signal is not a failure of the run.
func (p *Printer) exitErr() error {
	if _, ok := errors.AsType[*exec.ExitError](p.waitErr); ok {
		return nil
	}
	return p.waitErr
}

// ringWriter keeps the last limit bytes written to it.
type ringWriter struct {
	limit int
	mu    sync.Mutex
	buf   bytes.Buffer
}

func (w *ringWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(b)
	if excess := w.buf.Len() - w.limit; excess > 0 {
		w.buf.Next(excess)
	}
	return len(b), nil
}

func (w *ringWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.String()
}
