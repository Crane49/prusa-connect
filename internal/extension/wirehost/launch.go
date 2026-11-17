package wirehost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The refusals a launch returns.
var (
	// ErrNoPath reports Options with no executable to run.
	ErrNoPath = errors.New("wirehost: a launch names the executable to run")

	// ErrNoInstance reports Options with no instance name. Every log line the
	// child produces is bound to it, so a host with two children needs it.
	ErrNoInstance = errors.New("wirehost: a launch names the instance the child serves")

	// ErrHandshakeMissing reports a child that closed its standard output
	// before it wrote a handshake line.
	ErrHandshakeMissing = errors.New("wirehost: the child wrote no handshake line")

	// ErrHandshakeOversize reports a child whose first line is longer than the
	// specification allows before its first frame.
	ErrHandshakeOversize = errors.New("wirehost: the child's first line is over the handshake limit")

	// ErrRoutesUndescribed reports a child that names routes.serve in its info
	// and ships no OpenAPI fragment. The host mounts nothing it has no
	// description for. The refusal happens at launch, so the author reads it in
	// the log instead of a client finding routes no document covers.
	ErrRoutesUndescribed = errors.New("wirehost: a fragment is required for served routes")

	// ErrManifestDisagrees reports a child whose plugin.info answer is not the
	// module the manifest the operator installed from describes.
	ErrManifestDisagrees = errors.New("wirehost: the child is not the module its manifest describes")
)

// The bounds a launch applies. Each one is host policy. None of them goes on
// the wire, so a child never learns them and cannot depend on them.
const (
	// startTimeout bounds a start for a caller that brought no deadline of
	// its own. A caller's deadline bounds the whole start, so one number
	// applies. See startBound.
	startTimeout = 10 * time.Second

	// logLineBytes bounds one line of the child's standard error. A longer line
	// is split across several log records instead of being buffered whole, so a
	// child cannot make the host hold a line of any length it likes.
	logLineBytes = 8192

	// drainBackstop bounds the read of the pipes once the leader has exited and
	// its group is killed. A process that left the group with setsid survives
	// the kill and can hold a pipe open for as long as it runs.
	drainBackstop = 5 * time.Second
)

// splitAttribute names the log attribute a record carries when it holds part of
// a line. The next record from the same instance continues it, and one
// goroutine writes them, so the order is the child's.
//
// Without the attribute, two records read as two lines from the child. A stack
// trace or a JSON payload split in two would be read that way.
const splitAttribute = "split"

// Options is what a host provides to launch one child.
type Options struct {
	// Instance is the name every log line from this child is bound to.
	Instance string

	// Path is the executable to run.
	Path string
	// Args are its arguments.
	Args []string
	// Dir is the working directory. Empty means the host's own.
	Dir string
	// Env is the child's environment. Nil means the host's own.
	Env []string

	// Digest is the recorded checksum of the installed artefact, verified
	// before anything is executed. The empty Digest verifies nothing.
	Digest Digest
	// Artefact is the file Digest covers. Empty means Path, and a child run
	// through an interpreter names the installed script here.
	Artefact string

	// Contract is the lifecycle contract this host serves. The zero value
	// means wirerpc.Contract.
	Contract extension.Version

	// Manifest is the module.yaml the operator installed this artefact from,
	// and nil when the host has none. When it is set, identify checks the
	// child's own answer against it.
	Manifest *manifest.Manifest

	// Methods is the method set the host answers when the child calls it.
	Methods wirerpc.Methods
	// Window is the credit the host grants per stream. Zero means one chunk.
	Window uint32
	// MaxPayload bounds a frame the host accepts. Zero means the wire default.
	MaxPayload int
	// Stop bounds the wait after plugin.shutdown, before the host kills the
	// process. Zero means five seconds.
	Stop time.Duration

	// Logger receives the child's standard error, one record per line, and
	// what the host's side of the session could not send to the child.
	Logger *slog.Logger

	// Reaper is the host's Linux subreaper, or nil when it runs none. A child
	// started through it is never collected by it.
	Reaper *procgroup.Reaper
}

// Child is one running out-of-process extension.
type Child struct {
	instance string
	stop     time.Duration
	cmd      *exec.Cmd
	group    *procgroup.Group
	release  func()
	channel  *duplex
	stderr   io.Closer
	conn     *wirerpc.Conn
	info     wirerpc.Info
	fragment *routesapi.Fragment
	minor    uint32

	stopped sync.WaitGroup
	served  chan error
	closing sync.Once
	outcome error

	exited chan struct{}
	state  *os.ProcessState

	// reaping is held while the leader is reaped, so Kill never signals a
	// process group or job that is already gone.
	reaping   sync.Mutex
	reaped    bool
	unwatched func() bool
}

// Launch verifies the artefact, starts the child, reads its handshake line,
// settles the wire minor, and asks it what it is.
//
// Cancelling ctx stops the child. Everything the child writes to its standard
// error is copied into the logger, labelled with the instance, for as long as
// it runs.
func Launch(ctx context.Context, opts Options) (*Child, error) {
	if err := opts.check(); err != nil {
		return nil, err
	}

	if opts.Digest != "" {
		if err := opts.Digest.Verify(opts.artefact()); err != nil {
			return nil, err
		}
	}

	child, err := start(ctx, opts)
	if err != nil {
		return nil, err
	}

	err = child.open(ctx, opts)

	// open starts the last reader of the child's pipes, so the wait for them
	// begins only here.
	go child.wait()

	if err != nil {
		child.kill()
		child.reap()

		return nil, err
	}

	return child, nil
}

// Info is what the child answered plugin.info with.
func (c *Child) Info() wirerpc.Info {
	return c.info
}

// OpenAPI is the fragment the child shipped for its routes, already parsed by
// routesapi.ParseFragment, and nil for a child that shipped none.
func (c *Child) OpenAPI() *routesapi.Fragment {
	return c.fragment
}

// ServesRoutes reports whether the child declared an HTTP surface: it named
// routes.serve in its info, or it shipped a fragment describing one. A child
// that did neither serves no routes and a host mounts nothing for it.
func (c *Child) ServesRoutes() bool {
	return c.fragment != nil || c.info.Answers(routesapi.MethodServe)
}

// Minor is the wire minor the two sides settled on.
func (c *Child) Minor() uint32 {
	return c.minor
}

// Pid is the leader's process id. Once Exited is closed it can belong to
// another process.
func (c *Child) Pid() int {
	return c.cmd.Process.Pid
}

// Group is the child's process group, for reading its memory. It is released
// when Exited closes and must not be used after that. Kill the child through
// Child.Kill, never through Group().Kill: only Kill knows whether the leader
// has been reaped, after which its pgid can belong to another process.
func (c *Child) Group() *procgroup.Group {
	return c.group
}

// Exited is closed once the process has been reaped.
func (c *Child) Exited() <-chan struct{} {
	return c.exited
}

// ExitStop is how the process ended, and the zero Stop until Exited is closed.
// At is left for the caller, which knows when it noticed.
//
// A wait that failed leaves no exit status, which happens when something else
// in the process collected the child. That is ResultExited with code -1, the
// code os.ProcessState gives a process whose exit it does not know.
func (c *Child) ExitStop() supervisor.Stop {
	select {
	case <-c.exited:
	default:
		return supervisor.Stop{}
	}

	if c.state == nil {
		return supervisor.Stop{Result: supervisor.ResultExited, Code: -1}
	}

	if status, ok := c.state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return supervisor.Stop{Result: supervisor.ResultSignal, Signal: signalName(status.Signal())}
	}

	return supervisor.Stop{Result: supervisor.ResultExited, Code: c.state.ExitCode()}
}

// Conn is the session, for a contract's own calls.
func (c *Child) Conn() *wirerpc.Conn {
	return c.conn
}

// Close asks the child to stop, waits for it, and kills it when it does not
// go. It returns what the session ended with, and it is safe to call twice.
func (c *Child) Close() error {
	c.closing.Do(func() {
		c.outcome = c.shutdown()
	})

	return c.outcome
}

// shutdown runs the stop sequence once.
func (c *Child) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.stop)
	defer cancel()

	// A child that has already exited answers nothing, and that counts as an
	// ordinary stop.
	_, _ = c.conn.Call(ctx, wirerpc.MethodShutdown, nil)
	_ = c.conn.Close()

	select {
	case <-c.exited:
	case <-ctx.Done():
		c.kill()
		<-c.exited
	}

	return <-c.served
}

// open reads the handshake, starts the session, and asks the child what it is.
func (c *Child) open(ctx context.Context, opts Options) error {
	if err := c.handshake(ctx); err != nil {
		return err
	}

	var logger *slog.Logger
	if opts.Logger != nil {
		logger = opts.Logger.With("instance", opts.Instance)
	}

	conn, err := wirerpc.NewConn(c.channel, wirerpc.Options{
		Role:       wirerpc.RoleHost,
		Methods:    opts.Methods,
		Window:     opts.Window,
		MaxPayload: opts.MaxPayload,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("%s: build the session: %w", c.instance, err)
	}

	c.conn = conn
	c.served = make(chan error, 1)

	c.stopped.Go(func() {
		c.served <- conn.Serve(ctx)
	})

	return c.identify(ctx, opts)
}

// handshake reads the one line the child writes before its first frame and
// settles the minor both sides speak.
//
// The read blocks in the operating system and cannot see a context, so the
// channel is closed under it when the time is up. Otherwise a child that starts
// and then says nothing would be waited on for as long as it runs.
func (c *Child) handshake(ctx context.Context) error {
	ctx, cancel := startBound(ctx)
	defer cancel()

	// stop runs before cancel on the way out, so the close does not fire on a
	// line that arrived in time.
	stop := context.AfterFunc(ctx, func() { _ = c.channel.Close() })
	defer stop()

	line, err := c.channel.reader.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return fmt.Errorf("%s: %w", c.instance, ErrHandshakeOversize)
		}

		if ctx.Err() != nil {
			err = ctx.Err()
		}

		return fmt.Errorf("%s: %w: %w", c.instance, ErrHandshakeMissing, err)
	}

	said, err := wire.ParseHandshake(string(line))
	if err != nil {
		return fmt.Errorf("%s: %w", c.instance, err)
	}

	minor, err := wire.Negotiate(wire.Handshake{
		Protocol: wire.ProtocolMajor,
		MinMinor: wire.MinMinor,
		MaxMinor: wire.MaxMinor,
		Address:  wire.StdioAddress,
	}, said)
	if err != nil {
		return fmt.Errorf("%s: %w", c.instance, err)
	}

	c.minor = minor

	return nil
}

// identify calls the one method every child answers, and checks the answer
// against the contract this host serves.
func (c *Child) identify(ctx context.Context, opts Options) error {
	ctx, cancel := startBound(ctx)
	defer cancel()

	answer, err := c.conn.Call(ctx, wirerpc.MethodInfo, nil)
	if err != nil {
		return fmt.Errorf("%s: ask what the child is: %w", c.instance, err)
	}

	var info wirerpc.Info
	if err = json.Unmarshal(answer, &info); err != nil {
		return fmt.Errorf("%s: read the child's info: %w", c.instance, err)
	}

	if err = info.Validate(); err != nil {
		return fmt.Errorf("%s: %w", c.instance, err)
	}

	if err = info.Contract.CompatibleWith(opts.contract()); err != nil {
		return fmt.Errorf("%s: %w", c.instance, err)
	}

	if err = holdToManifest(opts.Manifest, info); err != nil {
		return fmt.Errorf("%s: %w", c.instance, err)
	}

	// The fragment is checked here and not when the routes are served, so a
	// child that describes its routes wrongly, or not at all, fails at launch
	// with the reason in the log. plugin.info is the earliest point the host
	// can know a child serves routes, and the last call before it is trusted.
	if info.Answers(routesapi.MethodServe) && len(info.OpenAPI) == 0 {
		return fmt.Errorf("%s: the module %s names routes.serve and ships no OpenAPI fragment, "+
			"attach one with routesapi.Describe: %w", c.instance, info.Name, ErrRoutesUndescribed)
	}

	if len(info.OpenAPI) > 0 {
		fragment, parseErr := routesapi.ParseFragment(info.OpenAPI)
		if parseErr != nil {
			return fmt.Errorf("%s: %w", c.instance, parseErr)
		}

		c.fragment = &fragment
	}

	c.info = info

	return nil
}

// startBound is how long one step of a start may take.
//
// A caller's deadline bounds the whole start. The supervisor bounds a start
// with the instance's own start limit and hands that context down, so the
// handshake, the first call and everything between them share the one number
// an operator configured. A caller with no deadline gets startTimeout.
func startBound(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, bounded := ctx.Deadline(); bounded {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, startTimeout)
}

// holdToManifest checks the running child against the manifest it was installed
// from.
//
// A module with a browser part is still one module. Its ui block sits in the
// same module.yaml as its process part, its bundle is installed under the
// namespace that manifest's module path gives, and the operator approved one
// set of permissions for both. So the child answering here has to be the module
// the manifest describes. A name, a release or a wire major that disagrees
// means a second artefact arrived under the first one's declaration, and its
// bundle would be served under a namespace nothing verified it for.
//
// A nil manifest checks nothing.
func holdToManifest(declared *manifest.Manifest, info wirerpc.Info) error {
	if declared == nil {
		return nil
	}

	if declared.Name != info.Name {
		return fmt.Errorf("the manifest declares the module %q and the child answers %q: %w",
			declared.Name, info.Name, ErrManifestDisagrees)
	}

	if declared.Version != "" && declared.Version != info.Version.String() {
		return fmt.Errorf("the manifest declares %s %s and the child answers %s: %w",
			declared.Name, declared.Version, info.Version, ErrManifestDisagrees)
	}

	if declared.ContractMajor == 0 {
		return nil
	}

	major, _, _, err := info.Contract.Parse()
	if err != nil {
		return fmt.Errorf("the child's contract: %w", err)
	}

	if major != declared.ContractMajor {
		return fmt.Errorf("the manifest declares contract_major %d and the child answers %s: %w",
			declared.ContractMajor, info.Contract, ErrManifestDisagrees)
	}

	return nil
}

// reap collects a child that was killed before it was accepted, the way Close
// collects one that was accepted. Without it the process stays a zombie.
func (c *Child) reap() {
	<-c.exited
}

// wait reaps the process and records how it ended.
//
// Once the leader has exited, the rest of its group is killed, so nothing a
// module started outlives it and no grandchild can hold a pipe open. Only then
// are the pipes read to their end, and only then is the leader reaped: Wait
// closes the pipes, and a reader still on one would lose the last thing the
// child said. A pipe still open after drainBackstop is held by a process that
// escaped the group, and the host closes its own end, so the session ends and
// the leader is reaped.
func (c *Child) wait() {
	if leaderExited(c.cmd.Process.Pid) == nil {
		_ = c.Kill()

		drained := make(chan struct{})
		go func() {
			c.stopped.Wait()
			close(drained)
		}()

		select {
		case <-drained:
		case <-time.After(drainBackstop):
			_ = c.channel.source.Close()
			_ = c.stderr.Close()
		}
	}

	c.stopped.Wait()

	c.reaping.Lock()
	_ = c.cmd.Wait()
	c.state = c.cmd.ProcessState
	c.reaped = true

	c.release()
	_ = c.group.Close()
	c.reaping.Unlock()

	c.unwatched()
	close(c.exited)
}

// Kill ends the child and every process it started, without asking. Once the
// child has been reaped it does nothing.
func (c *Child) Kill() error {
	c.reaping.Lock()
	defer c.reaping.Unlock()

	if c.reaped {
		return nil
	}

	return c.group.Kill() //nolint:wrapcheck // procgroup's error names the group.
}

// LimitMemory caps the memory of the child and every process it started,
// where the system can: a Windows job object. Elsewhere it does nothing. Once
// the child has been reaped it does nothing, because the job is closed.
func (c *Child) LimitMemory(bytes int64) error {
	c.reaping.Lock()
	defer c.reaping.Unlock()

	if c.reaped {
		return nil
	}

	return c.group.LimitMemory(bytes) //nolint:wrapcheck // procgroup's error names the limit.
}

// kill closes the session and kills the child.
func (c *Child) kill() {
	if c.conn != nil {
		_ = c.conn.Close()
	}

	if c.channel != nil {
		_ = c.channel.Close()
	}

	_ = c.Kill()
}

// defaultStop is the wait after plugin.shutdown before the host kills the
// process.
const defaultStop = 5 * time.Second

// start runs the process and begins copying its standard error.
func start(ctx context.Context, opts Options) (*Child, error) {
	// Not CommandContext: exec may call its Cancel after the leader is reaped,
	// and only Child.Kill knows when that has happened.
	cmd := exec.Command(opts.Path, opts.Args...) //nolint:gosec,noctx // see above; the caller names the artefact.
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: open the child's standard input: %w", opts.Instance, err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: open the child's standard output: %w", opts.Instance, err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: open the child's standard error: %w", opts.Instance, err)
	}

	procgroup.Prepare(cmd)

	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s: start %s: %w", opts.Instance, opts.Path, err)
	}

	release, err := startIn(opts.Reaper, cmd)
	if err != nil {
		return nil, fmt.Errorf("%s: start %s: %w", opts.Instance, opts.Path, err)
	}

	group, err := procgroup.Attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()

		release()

		return nil, fmt.Errorf("%s: group %s: %w", opts.Instance, opts.Path, err)
	}

	child := &Child{
		instance: opts.Instance,
		stop:     opts.stopAfter(),
		cmd:      cmd,
		group:    group,
		release:  release,
		exited:   make(chan struct{}),
		stderr:   stderr,
		channel: &duplex{
			// The handshake line and the frames behind it share one buffered
			// reader. A separate line reader over the same pipe would hold the
			// first frames in a buffer nothing else can see.
			reader: bufio.NewReaderSize(stdout, wire.MaxHandshakeBytes),
			source: stdout,
			writer: stdin,
		},
	}

	child.unwatched = context.AfterFunc(ctx, func() { _ = child.Kill() })

	child.stopped.Go(func() {
		mirror(ctx, opts.Logger, opts.Instance, stderr)
	})

	return child, nil
}

// startIn starts cmd through reaper, or directly when there is none.
func startIn(reaper *procgroup.Reaper, cmd *exec.Cmd) (release func(), err error) {
	if reaper != nil {
		return reaper.Start(cmd)
	}

	if err = cmd.Start(); err != nil {
		return nil, err
	}

	return func() {}, nil
}

// mirror copies the child's standard error into the host's log, labelled with
// the instance. A child that writes its logs to standard output corrupts the
// protocol, so standard error is where its messages appear.
//
// One record per line. A line over logLineBytes becomes several records, and
// every record but the last carries splitAttribute. It reads with a
// bufio.Reader and not a bufio.Scanner, because a Scanner over a longer line
// reports bufio.ErrTooLong, stops and cannot resume, which would lose the
// child's whole log from that line on.
//
// The long lines are split instead of truncated, because they are the ones
// worth reading. A stack trace, a JSON payload and a base64 blob are each one
// long line, and dropping the bytes would leave no sign in the log.
//
// It returns when the child closes its standard error, so a caller can wait on
// it.
func mirror(ctx context.Context, logger *slog.Logger, instance string, stderr io.Reader) {
	if logger == nil {
		_, _ = io.Copy(io.Discard, stderr)

		return
	}

	// The buffer is the bound. A line the child never ends costs the host this
	// much memory and no more.
	lines := bufio.NewReaderSize(stderr, logLineBytes)

	// The diagnostics bundle keys on module_stderr (observability.ModuleStderrKey,
	// which this package may not import).
	bound := logger.With("instance", instance, "module_stderr", true)

	for {
		// ReadSlice returns what it holds when the buffer fills before the
		// newline arrives, so the fragment is in hand and the next call
		// continues the same line.
		chunk, err := lines.ReadSlice('\n')
		partial := errors.Is(err, bufio.ErrBufferFull)

		switch {
		case partial:
			bound.InfoContext(ctx, string(chunk), splitAttribute, true)
		case len(chunk) > 0:
			bound.InfoContext(ctx, string(trimNewline(chunk)))
		}

		if err != nil && !partial {
			return
		}
	}
}

// trimNewline removes the line ending a child wrote, and nothing else. A child
// on Windows ends its lines with CRLF, and a carriage return left in the
// message reaches every reader of the log as an escape.
func trimNewline(line []byte) []byte {
	return bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
}

// duplex is the pair of pipes for one child: its standard output read back, and
// its standard input written to.
type duplex struct {
	reader *bufio.Reader
	source io.Closer
	writer io.WriteCloser
}

// Read returns the reader's error unwrapped. A frame codec compares it against
// io.EOF to tell an end between two frames from an end inside one.
func (d *duplex) Read(p []byte) (int, error) {
	return d.reader.Read(p)
}

func (d *duplex) Write(p []byte) (int, error) {
	return d.writer.Write(p)
}

// Close closes both pipes. That is what releases a session waiting on a child
// that stopped talking.
func (d *duplex) Close() error {
	return errors.Join(d.writer.Close(), d.source.Close())
}

// artefact is the file the digest covers.
func (o Options) artefact() string {
	if o.Artefact != "" {
		return o.Artefact
	}

	return o.Path
}

// contract is the lifecycle contract this host serves.
func (o Options) contract() extension.Version {
	if o.Contract != "" {
		return o.Contract
	}

	return wirerpc.Contract
}

// stopAfter is how long the host waits before it kills the process.
func (o Options) stopAfter() time.Duration {
	if o.Stop > 0 {
		return o.Stop
	}

	return defaultStop
}

// check reports what makes a launch impossible before anything runs.
func (o Options) check() error {
	if o.Path == "" {
		return ErrNoPath
	}

	if o.Instance == "" {
		return ErrNoInstance
	}

	return nil
}
