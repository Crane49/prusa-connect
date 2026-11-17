// Starting a module: a core on a free port with the module installed, and
// for a UI its dev server in front of the core.

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// target is the core a module is started in. It is also part of the name of
// the data directory under the checkout's bin/.
type target string

const (
	// targetDemo is the demo: simulated printers and a ready-made account.
	targetDemo target = "demo"
	// targetApp is the real app, which asks for an account on its first run.
	targetApp target = "app"
)

// The first ports tried. A port in use is skipped.
const (
	firstCorePort = 8080
	firstVitePort = 5173
)

// How long a start waits for a child to answer. The first start builds the
// web app, which can take minutes.
const (
	answerTimeout = 15 * time.Minute
	answerPoll    = 250 * time.Millisecond
	stopTimeout   = 10 * time.Second
)

// passwordBytes is the number of random bytes in the demo password. logTail
// is the number of log lines shown for a child that stopped.
const (
	passwordBytes = 12
	logTail       = 20
)

// The menu numbers of the start question.
const (
	startDemo = 1
	startApp  = 2
)

// launch is one start of a module: which core, the module, and the ports.
type launch struct {
	into     target
	name     string
	dir      string
	checkout string
	// workDir is where the command was run. Printed paths are relative to it.
	workDir string
	// ui is true for a module with a bundle, which gets a dev server. edit
	// is the file the author changes first, relative to dir.
	ui   bool
	edit string
	core int
	vite int
	// password is the demo account's password. It is empty for the real app.
	password string
}

// data is the directory with this core's configuration, written by
// install.sh, and its logs.
func (l launch) data() string { return filepath.Join(l.checkout, "bin", l.name+"-"+string(l.into)) }

// overlay is the file `module add` declares the module in. install.sh uses
// the same path for each target.
func (l launch) overlay() string {
	if l.into == targetDemo {
		return filepath.Join(l.data(), "demo", "resources.json")
	}

	return filepath.Join(l.data(), "resources.json")
}

// installed reports whether this core already declares the module.
func (l launch) installed() bool {
	declared, err := os.ReadFile(l.overlay())

	return err == nil && bytes.Contains(declared, []byte(strconv.Quote(l.name)))
}

// laterLine is the command that starts the module later, in the demo.
func laterLine(request answers, dir string) string {
	line := []string{"go", "run", "./cmd/new-module", "-start", string(targetDemo),
		"-dir", scaffold.ShellWord(shown(request.workDir, dir))}

	if request.checkout != "" {
		line = append(line, "-checkout", scaffold.ShellWord(request.checkout))
	}

	return strings.Join(line, " ")
}

// askStart asks which core to start the module in. It returns "" for "not
// now" and for an ended stream.
func askStart(a *asker) target {
	picked := a.choose(questions["start"],
		[]string{"demo, with simulated printers", "the real app", "not now"}, startDemo)

	switch {
	case a.ended:
		return ""
	case picked == startDemo:
		return targetDemo
	case picked == startApp:
		return targetApp
	default:
		return ""
	}
}

// startLater handles -start: it starts the module at -dir in the core
// -start names, installing it first when needed.
func startLater(ctx context.Context, out io.Writer, a *asker, env environment, request answers) error {
	dir := request.dir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(request.workDir, dir)
	}

	checkout, err := scaffold.FindCheckout(request.checkout, request.workDir)
	if err != nil {
		return fmt.Errorf("-start runs the checkout's server: %w", err)
	}

	document, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	if err != nil {
		return fmt.Errorf("-dir is a module's folder: %w", err)
	}

	read, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	l, err := launchFor(ctx, target(request.start), read.Name, filepath.Clean(dir), filepath.FromSlash(checkout),
		request.workDir)
	if err != nil {
		return err
	}

	color := a != nil && a.color
	if err = ensureInstalled(ctx, env, &stepper{out: out, color: color}, l); err != nil {
		return err
	}

	return env.launch(ctx, out, color, l)
}

// launchFor picks a free port for the core, one for the dev server when
// there is a bundle, and a random password for the demo.
func launchFor(ctx context.Context, into target, name, dir, checkout, workDir string) (launch, error) {
	l := launch{into: into, name: name, dir: dir, checkout: checkout, workDir: workDir}

	var err error

	if l.core, err = freePort(ctx, "127.0.0.1", firstCorePort); err != nil {
		return launch{}, err
	}

	if _, err = os.Stat(filepath.Join(dir, "ui", "package.json")); err == nil {
		l.ui, l.edit = true, editedIn(dir)

		if l.vite, err = freePort(ctx, "localhost", firstVitePort); err != nil {
			return launch{}, err
		}
	}

	if into == targetDemo {
		secret := make([]byte, passwordBytes)
		if _, err = rand.Read(secret); err != nil {
			return launch{}, fmt.Errorf("make the demo's password: %w", err)
		}

		// URL-safe, because the password passes through make's DEMO_ARGS
		// and a shell.
		l.password = base64.RawURLEncoding.EncodeToString(secret)
	}

	return l, nil
}

// editedIn is the file the author changes first: the module's first
// component in table order, or ui/src/index.ts when it has none.
func editedIn(dir string) string {
	for _, slot := range manifest.Slots() {
		name := path.Base((scaffold.Contribution{Slot: slot.Name}).File())
		file := filepath.Join("ui", "src", name)

		if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
			return file
		}
	}

	return filepath.Join("ui", "src", "index.ts")
}

// freePort returns the first port from first up that nothing answers on and
// that binds. On macOS a bind alone can succeed on a port a server already has.
func freePort(ctx context.Context, host string, first int) (int, error) {
	dialer := net.Dialer{Timeout: time.Second}

	var binder net.ListenConfig

	for port := first; port <= 65535; port++ {
		address := net.JoinHostPort(host, strconv.Itoa(port))

		if conn, err := dialer.DialContext(ctx, "tcp", address); err == nil {
			_ = conn.Close() // a probe: the connection carried nothing.

			continue
		}

		held, err := binder.Listen(ctx, "tcp", address)
		if err != nil {
			continue
		}

		if err = held.Close(); err != nil {
			return 0, fmt.Errorf("free %s: %w", address, err)
		}

		return port, nil
	}

	return 0, fmt.Errorf("no port from %d up is free on %s", first, host)
}

func ensureInstalled(ctx context.Context, env environment, steps *stepper, l launch) error {
	if l.installed() {
		return nil
	}

	return installInto(ctx, env, steps, l.dir, l.into, l.workDir)
}

// installInto runs install.sh for into. Its output is shown only on failure.
func installInto(ctx context.Context, env environment, steps *stepper, dir string, into target, workDir string) error {
	erase := waiting(steps.out, steps.color, "building and installing, the first time takes a few minutes")
	said, err := env.install(ctx, dir, into)

	erase()

	if err != nil {
		return fmt.Errorf("sh install.sh in %s: %w\n%s", shown(workDir, dir), err, strings.TrimSpace(string(said)))
	}

	done := paint(steps.color, styleGreen, "✔") + " built and installed"
	if into == targetApp {
		done += " in the real app"
	}

	steps.next("%s", done)

	return nil
}

// waiting prints a progress line and returns a function that erases it. The
// function is safe to call twice. Without color nothing is printed.
func waiting(out io.Writer, color bool, what string) func() {
	if !color {
		return func() {}
	}

	fmt.Fprint(out, paint(true, styleDim, "   "+what+"..."))

	return sync.OnceFunc(func() { fmt.Fprint(out, "\r\x1b[K") })
}

// runInstall runs the module's install.sh with TARGET set to into.
func runInstall(ctx context.Context, dir string, into target) ([]byte, error) {
	install := exec.CommandContext(ctx, "sh", "install.sh")
	install.Dir = dir
	install.Env = append(os.Environ(), "TARGET="+string(into))

	said, err := install.CombinedOutput()
	if err != nil {
		return said, fmt.Errorf("%w", err)
	}

	return said, nil
}

// runningBlock is the text printed once every child answers.
func runningBlock(l launch, color bool) string {
	address := "http://127.0.0.1:" + strconv.Itoa(l.core)
	if l.ui {
		address = "http://localhost:" + strconv.Itoa(l.vite)
	}

	how := "real app · first run asks you to set up an account"
	if l.into == targetDemo {
		how = "demo · sign in as demo / " + l.password
	}

	var block strings.Builder

	fmt.Fprintf(&block, "%s %s  %s   (%s)\n", paint(color, styleGreen, "✔"), paint(color, styleBold, "running"),
		address, how)

	logs := []string{shown(l.workDir, filepath.Join(l.data(), "core.log"))}

	if l.ui {
		fmt.Fprintf(&block, "  edit %s, the page updates as you save\n", shown(l.workDir, filepath.Join(l.dir, l.edit)))
		block.WriteString("  Ctrl+C stops both\n")

		logs = append(logs, shown(l.workDir, filepath.Join(l.data(), "vite.log")))
	} else {
		block.WriteString("  Ctrl+C stops it\n")
	}

	block.WriteString(paint(color, styleDim, "  logs "+strings.Join(logs, ", ")) + "\n")

	return block.String()
}

// coreCommand returns the command for l's core: `make demo` for the demo, or
// `make embed-app` and `go run` for the real app.
func coreCommand(ctx context.Context, l launch) *exec.Cmd {
	config := filepath.Join(l.data(), "config.yaml")
	address := "127.0.0.1:" + strconv.Itoa(l.core)

	var command *exec.Cmd

	if l.into == targetDemo {
		// The Makefile expands DEMO_ARGS into a shell line, so each word is
		// quoted for that shell.
		words := []string{
			"-config", config, "-dir", filepath.Join(l.data(), "demo"), "-addr", address, "-password", l.password,
		}
		for i, word := range words {
			words[i] = scaffold.ShellWord(word)
		}

		//nolint:gosec // G204: every argument is a path or a port built here.
		command = exec.CommandContext(ctx, "make", "demo", "DEMO_ARGS="+strings.Join(words, " "))
	} else {
		command = exec.CommandContext(ctx, "sh", "-c",
			`make embed-app && exec go run -tags full ./cmd/prusa-connect core -config "$1" -addr "$2"`,
			"sh", config, address)
	}

	command.Dir = l.checkout

	return command
}

// viteCommand returns the command for the module's dev server, pointed at
// l's core.
func viteCommand(ctx context.Context, l launch) *exec.Cmd {
	//nolint:gosec // G204: the port is a number, passed without a shell.
	command := exec.CommandContext(ctx, "pnpm", "dev", "--port", strconv.Itoa(l.vite), "--strictPort")
	command.Dir = filepath.Join(l.dir, "ui")
	command.Env = append(os.Environ(), "PCL_CORE_URL=http://127.0.0.1:"+strconv.Itoa(l.core))

	return command
}

// child is one process the launch started. Its output goes to its own log.
type child struct {
	what   string
	log    string
	cmd    *exec.Cmd
	exited chan struct{}
}

// startChild starts command in its own process group and logs its output.
// Ctrl+C then reaches only this process, which stops each group itself.
func startChild(what, log string, command *exec.Cmd) (*child, error) {
	file, err := os.Create(log)
	if err != nil {
		return nil, fmt.Errorf("open the %s log: %w", what, err)
	}

	command.Stdout, command.Stderr = file, file
	ownGroup(command)

	command.Cancel = func() error { interruptGroup(command); return nil }
	command.WaitDelay = stopTimeout

	if err = command.Start(); err != nil {
		_ = file.Close() // nothing was written to it.

		return nil, fmt.Errorf("start the %s: %w", what, err)
	}

	started := &child{what: what, log: log, cmd: command, exited: make(chan struct{})}

	go func() {
		_ = command.Wait() // the exit is reported through exited, and the log says why.
		_ = file.Close()   // the process that wrote it has ended.

		endGroup(command)
		close(started.exited)
	}()

	return started, nil
}

// stopped is the error for a child that exited by itself. It includes the
// end of the log.
func (c *child) stopped() error {
	said, err := os.ReadFile(c.log)
	if err != nil {
		return fmt.Errorf("the %s stopped, and %s cannot be read: %w", c.what, c.log, err)
	}

	lines := strings.Split(strings.TrimSpace(string(said)), "\n")
	if len(lines) > logTail {
		lines = lines[len(lines)-logTail:]
	}

	return fmt.Errorf("the %s stopped. The end of %s:\n%s", c.what, c.log, strings.Join(lines, "\n"))
}

// errNoAnswer means a child runs but does not answer in time.
var errNoAnswer = errors.New("does not answer")

// answer waits until address answers. It fails when a child exits first.
func answer(ctx context.Context, address string, children ...*child) error {
	ctx, cancel := context.WithTimeout(ctx, answerTimeout)
	defer cancel()

	client := &http.Client{Timeout: time.Second}

	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return fmt.Errorf("ask %s: %w", address, err)
		}

		if response, failed := client.Do(request); failed == nil {
			_ = response.Body.Close() // any answer is enough, and the body is not read.

			return nil
		}

		for _, c := range children {
			select {
			case <-c.exited:
				return c.stopped()
			default:
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%s %w after %s: %w", address, errNoAnswer, answerTimeout, ctx.Err())
		case <-time.After(answerPoll):
		}
	}
}

// runLaunch starts l's core and, for a bundle, its dev server. It returns nil
// on Ctrl+C and an error when a child exits by itself.
func runLaunch(ctx context.Context, out io.Writer, color bool, l launch) error {
	defer takeTerminal(os.Stdin)()

	signalled, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	running, cancel := context.WithCancel(signalled)

	var children []*child

	// Stop every child and wait for it on any return path.
	defer func() {
		cancel()

		for _, c := range children {
			<-c.exited
		}
	}()

	err := launchAll(running, out, color, l, &children)

	switch {
	case signalled.Err() != nil:
		return nil
	case err != nil:
		return err
	}

	// A nil channel never receives, so a module without a bundle waits on
	// the core only.
	var viteExited chan struct{}
	if len(children) > 1 {
		viteExited = children[1].exited
	}

	select {
	case <-signalled.Done():
		return nil
	case <-children[0].exited:
		return children[0].stopped()
	case <-viteExited:
		return children[1].stopped()
	}
}

// launchAll starts the core, waits for it to answer, then does the same for
// the dev server. The dev server needs the core up first.
func launchAll(ctx context.Context, out io.Writer, color bool, l launch, children *[]*child) error {
	what := "the demo"
	if l.into == targetApp {
		what = "the real app"
	}

	erase := waiting(out, color, "starting "+what+", the first start builds the app")
	defer erase()

	core, err := startChild("core", filepath.Join(l.data(), "core.log"), coreCommand(ctx, l))
	if err != nil {
		return err
	}

	*children = append(*children, core)

	if err = answer(ctx, "http://127.0.0.1:"+strconv.Itoa(l.core)+"/", core); err != nil {
		return err
	}

	if l.ui {
		vite, failed := startChild("dev server", filepath.Join(l.data(), "vite.log"), viteCommand(ctx, l))
		if failed != nil {
			return failed
		}

		*children = append(*children, vite)

		if err = answer(ctx, "http://localhost:"+strconv.Itoa(l.vite)+"/", core, vite); err != nil {
			return err
		}
	}

	erase()
	fmt.Fprint(out, runningBlock(l, color))

	return nil
}
