package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func decode(body []byte, into any) error {
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("read the response: %w", err)
	}

	return nil
}

// How long the driver waits for its child.
const (
	startBudget    = 60 * time.Second
	stopBudget     = 30 * time.Second
	warmup         = 10 * time.Second
	sampleEvery    = time.Second
	clientTimeout  = 60 * time.Second
	profileTimeout = 5 * time.Minute
)

// idleConnsPerHost is how many connections the driver keeps open to the
// server. With net/http's default of two the probes would queue.
const idleConnsPerHost = 64

// drive starts the server, loads it, measures it and prints the report.
func drive(ctx context.Context, opts options) error {
	dir, cleanup, err := workingDir(opts)
	if err != nil {
		return err
	}

	defer cleanup()

	if err = refuseAStranger(opts); err != nil {
		return err
	}

	child, err := startChild(ctx, opts, dir)
	if err != nil {
		return err
	}

	defer child.stop()

	base := "http://" + opts.api
	client := newClient()

	if err = awaitHealthy(ctx, client, base); err != nil {
		return err
	}

	result, err := loadAndMeasure(ctx, opts, client, base)
	if err != nil {
		return err
	}

	result.dir = dir
	result.opts = opts

	return result.print(os.Stdout)
}

// loadAndMeasure runs the whole load against a server that is already up.
func loadAndMeasure(ctx context.Context, opts options, client *http.Client, base string) (*measurement, error) {
	out := &measurement{}
	log := simulatorLogger()

	printers, err := newFleet(opts, 0, opts.printers, log)
	if err != nil {
		return nil, err
	}

	fleetCtx, stopFleet := context.WithCancel(ctx)
	defer func() {
		stopFleet()
		printers.wait()
	}()

	// The clock starts before the ramp, so the cold start includes it.
	started := time.Now()

	printers.start(fleetCtx, opts.ramp)

	if _, err = awaitConnected(ctx, client, base, opts.printers, opts.settle); err != nil {
		return nil, fmt.Errorf("%w; %d stopped, first reasons: %s",
			err, printers.failures.Load(), strings.Join(printers.reasons(), " | "))
	}

	out.connect = time.Since(started)

	page, err := fleetPage(ctx, client, base, opts.printers)
	if err != nil {
		return nil, err
	}

	if out.grantTook, out.grants, err = seedGrants(ctx, client, base, page.Items, opts.grants); err != nil {
		return nil, err
	}

	subCtx, stopSubs := context.WithCancel(ctx)
	defer stopSubs()

	readers, subGroup := watch(subCtx, base, opts.subscribers)

	if err = sleep(ctx, warmup); err != nil {
		return nil, err
	}

	// Read the stream counters before and after the measured window.
	framesBefore, bytesBefore := readers.frames.Load(), readers.bytes.Load()

	if err = out.steady(ctx, opts, client, base, page); err != nil {
		return nil, err
	}

	out.frames = readers.frames.Load() - framesBefore
	out.streamBytes = readers.bytes.Load() - bytesBefore

	if final, pageErr := fleetPage(ctx, client, base, opts.printers); pageErr == nil {
		out.issues = final.Issues
	}

	_ = save(ctx, opts.out, "metrics.txt", base+"/metrics")

	out.arrival, out.arrivalErr = measureArrival(ctx, opts, client, base, log)

	stopSubs()
	subGroup.Wait()

	out.streamErrors = readers.errors.Load()
	out.sessions = printers.sessions()
	out.printerFailures = printers.failures.Load()
	out.printerReasons = printers.reasons()

	return out, nil
}

// steady is the measured window: the probes, the samples and the profiles,
// over a fleet that is connected and watched.
func (r *measurement) steady(ctx context.Context, opts options, client *http.Client, base string, page listing) error {
	r.probes = []*probe{
		{name: "GET /api/v1/devices (one page)", url: base + "/api/v1/devices"},
		{name: "GET /api/v1/devices?limit=" + strconv.Itoa(min(opts.printers, listingCap)), url: fmt.Sprintf(
			"%s/api/v1/devices?limit=%d", base, min(opts.printers, listingCap))},
		{name: "GET /api/v1/devices/{id}", url: base + "/api/v1/devices/" + firstID(page)},
		{name: "GET /api/v1/health", url: base + "/api/v1/health"},
	}

	probeCtx, stopProbes := context.WithTimeout(ctx, opts.duration)
	defer stopProbes()

	for _, one := range r.probes {
		go one.run(probeCtx, client)
	}

	go r.collectProfile(ctx, opts)

	var err error
	if r.samples, err = sampleFor(probeCtx, client, opts); err != nil {
		return err
	}

	return nil
}

// sampleFor reads the child's counters every second until the window closes.
func sampleFor(ctx context.Context, client *http.Client, opts options) ([]snapshot, error) {
	var samples []snapshot

	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()

	for {
		one, err := fetchStat(ctx, client, opts.admin)
		if err == nil {
			samples = append(samples, one)
		}

		select {
		case <-ctx.Done():
			if len(samples) < twoSamples {
				return nil, errors.New("the measured window produced fewer than two samples")
			}

			return samples, nil
		case <-ticker.C:
		}
	}
}

// collectProfile saves a CPU profile of the child under load, then the heap,
// goroutine and allocation profiles.
func (r *measurement) collectProfile(ctx context.Context, opts options) {
	if opts.out == "" {
		return
	}

	admin := "http://" + opts.admin

	r.profileErr = errors.Join(
		save(ctx, opts.out, "cpu.pprof", fmt.Sprintf("%s/debug/pprof/profile?seconds=%d",
			admin, int(opts.profile.Seconds()))),
		save(ctx, opts.out, "heap.pprof", admin+"/debug/pprof/heap"),
		save(ctx, opts.out, "goroutine.pprof", admin+"/debug/pprof/goroutine"),
		save(ctx, opts.out, "allocs.pprof", admin+"/debug/pprof/allocs"),
	)
}

func save(ctx context.Context, dir, name, url string) error {
	if dir == "" {
		return nil
	}

	// The request lasts as long as the CPU profile runs, so it needs its
	// own longer timeout.
	client := &http.Client{Timeout: profileTimeout}

	body, status, err := get(ctx, client, url)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", name, err)
	}

	if status != http.StatusOK {
		return fmt.Errorf("fetch %s: status %d", name, status)
	}

	return os.WriteFile(filepath.Join(dir, name), body, 0o600)
}

// measureArrival is how long one more printer takes to connect while the rest
// of the fleet is reporting.
func measureArrival(ctx context.Context, opts options, client *http.Client, base string, log *slog.Logger) (
	time.Duration, error,
) {
	late, err := newFleet(opts, opts.printers, 1, log)
	if err != nil {
		return 0, err
	}

	arriveCtx, stop := context.WithCancel(ctx)
	defer func() {
		stop()
		late.wait()
	}()

	started := time.Now()

	late.start(arriveCtx, 0)

	took, err := awaitConnected(ctx, client, base, opts.printers+1, opts.settle)
	if err != nil {
		return time.Since(started), err
	}

	return took, nil
}

// firstID is a device id for the probe. With an empty fleet the probe gets a
// 404, and the failure count shows it.
func firstID(page listing) string {
	if len(page.Items) == 0 {
		return "none"
	}

	return page.Items[0].ID
}

func fetchStat(ctx context.Context, client *http.Client, admin string) (snapshot, error) {
	var out snapshot

	body, status, err := get(ctx, client, "http://"+admin+statPath)
	if err != nil {
		return out, err
	}

	if status != http.StatusOK {
		return out, fmt.Errorf("%s answered %d", statPath, status)
	}

	return out, decode(body, &out)
}

// refuseAStranger stops a run whose addresses are already taken. Otherwise the
// run would measure a server left behind by an earlier run.
func refuseAStranger(opts options) error {
	dialer := &net.Dialer{Timeout: strangerTimeout}

	ctx, cancel := context.WithTimeout(context.Background(), strangerTimeout)
	defer cancel()

	for _, addr := range []string{opts.api, opts.admin} {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			continue
		}

		_ = conn.Close()

		return fmt.Errorf("something is already listening on %s, so this run would measure it", addr)
	}

	return nil
}

// strangerTimeout bounds the dials of refuseAStranger.
const strangerTimeout = 2 * time.Second

func awaitHealthy(ctx context.Context, client *http.Client, base string) error {
	deadline := time.Now().Add(startBudget)

	for {
		_, status, err := get(ctx, client, base+"/api/v1/health")
		if err == nil && status == http.StatusOK {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("the core did not answer on %s within %s", base, startBudget)
		}

		if err = sleep(ctx, pollInterval); err != nil {
			return err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func newClient() *http.Client {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: clientTimeout}
	}

	tuned := transport.Clone()
	tuned.MaxIdleConnsPerHost = idleConnsPerHost
	tuned.MaxConnsPerHost = idleConnsPerHost

	return &http.Client{Timeout: clientTimeout, Transport: tuned}
}

// workingDir is where the server's databases and configuration go. A
// directory the caller named is kept. A temporary one is removed unless -keep
// is set.
func workingDir(opts options) (string, func(), error) {
	if opts.dir != "" {
		if err := os.MkdirAll(opts.dir, 0o700); err != nil {
			return "", nil, fmt.Errorf("make the working directory: %w", err)
		}

		return opts.dir, func() {}, nil
	}

	dir, err := os.MkdirTemp("", "fleetload-")
	if err != nil {
		return "", nil, fmt.Errorf("make the working directory: %w", err)
	}

	if opts.keep {
		return dir, func() {}, nil
	}

	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// child is the server process this harness measures.
type child struct {
	cmd   *exec.Cmd
	admin string
	log   *os.File
	leash io.WriteCloser
}

// startChild runs this binary in the server role. The server is a separate
// process so that its CPU and heap do not include the simulated printers.
func startChild(ctx context.Context, opts options, dir string) (*child, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find this binary: %w", err)
	}

	logPath := filepath.Join(dir, "server.log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", logPath, err)
	}

	args := []string{
		"-role", roleServer,
		"-dir", dir,
		"-api", opts.api,
		"-admin", opts.admin,
		"-engine", opts.engine,
		"-dsn", opts.dsn,
		"-loglevel", opts.loglevel,
		"-printers", strconv.Itoa(opts.printers),
		"-maxprocs", strconv.Itoa(opts.maxprocs),
		"-memlimit", strconv.FormatInt(opts.memlimit, 10),
	}

	if opts.history {
		args = append(args, "-history")
	}

	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// The child stops when this pipe closes, also when this process is
	// killed.
	leash, err := cmd.StdinPipe()
	if err != nil {
		_ = logFile.Close()

		return nil, fmt.Errorf("open the leash to the server child: %w", err)
	}

	if err = cmd.Start(); err != nil {
		_ = logFile.Close()
		_ = leash.Close()

		return nil, fmt.Errorf("start the server child: %w", err)
	}

	return &child{cmd: cmd, admin: opts.admin, log: logFile, leash: leash}, nil
}

// stop asks the child to shut down and kills it after stopBudget.
func (c *child) stop() {
	defer func() { _ = c.log.Close() }()

	quitCtx, cancel := context.WithTimeout(context.Background(), stopBudget)
	defer cancel()

	_, _, _ = get(quitCtx, &http.Client{Timeout: stopBudget}, "http://"+c.admin+quitPath)

	// Closing the pipe also stops the child, in case the admin listener
	// never came up.
	_ = c.leash.Close()

	done := make(chan struct{})

	go func() {
		_ = c.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(stopBudget):
		_ = c.cmd.Process.Kill()
		<-done
	}
}
