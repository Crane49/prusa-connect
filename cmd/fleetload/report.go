package main

import (
	"fmt"
	"io"
	"runtime"
	"text/tabwriter"
	"time"
)

// measurement is everything one run produced.
type measurement struct {
	opts opts
	dir  string

	connect   time.Duration
	grantTook time.Duration
	grants    int

	samples []snapshot
	probes  []*probe

	arrival    time.Duration
	arrivalErr error

	frames          int64
	streamBytes     int64
	streamErrors    int64
	sessions        uint64
	printerFailures int64
	printerReasons  []string

	profileErr error
	issues     issues
}

// opts is the command line, which the report prints back.
type opts = options

// The units and the quantiles of the report.
const (
	mib     = 1 << 20
	percent = 100

	// twoSamples is the fewest samples a rate needs. It is also used as the
	// tab writer's column padding.
	twoSamples = 2

	quantile50 = 0.50
	quantile95 = 0.95
	quantile99 = 0.99
)

// print writes the whole measurement.
func (r *measurement) print(w io.Writer) error {
	if len(r.samples) < twoSamples {
		return fmt.Errorf("only %d samples were taken", len(r.samples))
	}

	first, last := r.samples[0], r.samples[len(r.samples)-1]
	wall := last.At.Sub(first.At)

	tab := tabwriter.NewWriter(w, 0, 0, twoSamples, ' ', 0)

	fmt.Fprintf(tab, "\nfleetload\t\n")
	fmt.Fprintf(tab, "machine\t%s/%s, GOMAXPROCS %d in the server\n", runtime.GOOS, runtime.GOARCH, last.MaxProcs)
	fmt.Fprintf(tab, "fleet\t%d printers at %s, model %s, ramp %s\n",
		r.opts.printers, r.opts.cadence, r.opts.model, r.opts.ramp)
	fmt.Fprintf(tab, "engine\t%s\n", r.opts.engine)
	fmt.Fprintf(tab, "readers\t%d on /api/v1/events\n", r.opts.subscribers)
	fmt.Fprintf(tab, "history\t%v\n", r.opts.history)
	fmt.Fprintf(tab, "window\t%s, %d samples, %s to %s\n", wall.Round(time.Second), len(r.samples),
		first.At.Format(time.TimeOnly), last.At.Format(time.TimeOnly))
	fmt.Fprintf(tab, "working dir\t%s\n", r.dir)

	r.printCPU(tab, first, last, wall)
	r.printMemory(tab, first, last)
	r.printGC(tab, first, last, wall)
	r.printFleet(tab, wall)
	r.printLatency(tab)

	return tab.Flush()
}

// printCPU reports the CPU time the operating system charged the process, and
// the runtime's own figure beside it. See processCPU for why they differ.
func (r *measurement) printCPU(w io.Writer, first, last snapshot, wall time.Duration) {
	busy := (last.OSCPU - first.OSCPU).Seconds()
	claimed := (last.CPUTotal - last.CPUIdle) - (first.CPUTotal - first.CPUIdle)
	gc := last.CPUGC - first.CPUGC
	cores := busy / wall.Seconds()

	fmt.Fprintf(w, "\ncpu, as the operating system charged it\t\n")
	fmt.Fprintf(w, "  busy\t%.2f cpu-seconds over %s\n", busy, wall.Round(time.Second))
	fmt.Fprintf(w, "  one core\t%.1f%%\n", cores*percent)
	fmt.Fprintf(w, "  the machine\t%.1f%% of %d cores\n", cores/float64(last.MaxProcs)*percent, last.MaxProcs)
	fmt.Fprintf(w, "  the runtime's own figure\t%.2f cpu-seconds, which this harness does not believe\n", claimed)
	fmt.Fprintf(w, "  the runtime's collector figure\t%.2f cpu-seconds, on the same accounting\n", gc)
}

func (r *measurement) printMemory(w io.Writer, first, last snapshot) {
	var peak uint64

	for _, one := range r.samples {
		peak = max(peak, one.InUse())
	}

	fmt.Fprintf(w, "\nmemory\t\n")
	fmt.Fprintf(w, "  runtime, first\t%.1f MiB\n", megabytes(float64(first.InUse())))
	fmt.Fprintf(w, "  runtime, last\t%.1f MiB\n", megabytes(float64(last.InUse())))
	fmt.Fprintf(w, "  runtime, peak\t%.1f MiB\n", megabytes(float64(peak)))
	drift := megabytes(float64(last.InUse())) - megabytes(float64(first.InUse()))

	fmt.Fprintf(w, "  drift\t%+.1f MiB over the window\n", drift)
	fmt.Fprintf(w, "  live heap, last\t%.1f MiB\n", megabytes(float64(last.HeapLive)))
	fmt.Fprintf(w, "  goroutines\t%d, from %d\n", last.Goroutines, first.Goroutines)
}

func (r *measurement) printGC(w io.Writer, first, last snapshot, wall time.Duration) {
	bytes := last.AllocBytes - first.AllocBytes
	objects := last.AllocObjects - first.AllocObjects

	fmt.Fprintf(w, "\ngarbage collector\t\n")
	fmt.Fprintf(w, "  allocation rate\t%.1f MiB/s\n", megabytes(float64(bytes))/wall.Seconds())
	fmt.Fprintf(w, "  object rate\t%.0f objects/s\n", float64(objects)/wall.Seconds())
	fmt.Fprintf(w, "  cycles\t%d over the window\n", last.GCCycles-first.GCCycles)
	fmt.Fprintf(w, "  pauses\t%d, p50 %s, p99 %s\n", pauseCount(first, last),
		pauseQuantile(first, last, quantile50), pauseQuantile(first, last, quantile99))
}

func (r *measurement) printFleet(w io.Writer, wall time.Duration) {
	fmt.Fprintf(w, "\nfleet\t\n")
	fmt.Fprintf(w, "  cold start\t%s to connect %d printers, of which %s is the harness ramp\n",
		r.connect.Round(time.Millisecond), r.opts.printers, r.opts.ramp)
	fmt.Fprintf(w, "  websocket sessions\t%d (a number above the fleet size means reconnections)\n", r.sessions)
	fmt.Fprintf(w, "  printers that stopped\t%d\n", r.printerFailures)
	fmt.Fprintf(w, "  grants written\t%d in %s\n", r.grants, r.grantTook.Round(time.Millisecond))

	if r.grants > 0 {
		fmt.Fprintf(w, "  one grant\t%s\n", (r.grantTook / time.Duration(r.grants)).Round(time.Microsecond))
	}

	streamed := megabytes(float64(r.streamBytes))

	fmt.Fprintf(w, "  stream frames\t%d, %.0f/s across %d readers\n",
		r.frames, float64(r.frames)/wall.Seconds(), r.opts.subscribers)
	fmt.Fprintf(w, "  stream bytes\t%.1f MiB, %.2f MiB/s\n", streamed, streamed/wall.Seconds())
	fmt.Fprintf(w, "  stream restarts\t%d\n", r.streamErrors)
	fmt.Fprintf(w, "  fleet issues\t%s\n", r.issues.named())

	arrival := r.arrival.Round(time.Millisecond).String()
	if r.arrivalErr != nil {
		arrival += " (" + r.arrivalErr.Error() + ")"
	}

	fmt.Fprintf(w, "  one late arrival\t%s with %d already reporting\n", arrival, r.opts.printers)

	if r.profileErr != nil {
		fmt.Fprintf(w, "  profiles\t%s\n", r.profileErr.Error())
	}
}

func (r *measurement) printLatency(w io.Writer) {
	fmt.Fprintf(w, "\nlatency, while the fleet reports\t\n")
	fmt.Fprintf(w, "  endpoint\tcalls\tfail\tbytes\tp50\tp95\tp99\tmax\n")

	for _, one := range r.probes {
		got := one.result()

		fmt.Fprintf(w, "  %s\t%d\t%d\t%d\t%s\t%s\t%s\t%s\n", got.name, got.calls, got.fails, got.bytes,
			round(got.median), round(got.p95), round(got.p99), round(got.slowest))
	}
}

func round(d time.Duration) time.Duration { return d.Round(time.Microsecond) }

// megabytes renders a byte count in mebibytes.
func megabytes(value float64) float64 { return value / mib }
