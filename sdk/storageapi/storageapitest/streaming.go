package storageapitest

import (
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// TransferSize is how large the object a provider is streamed is: 512 MiB.
//
// The number is chosen against the deployment this product is sized for.
// Earlier research sets GOMEMLIMIT to 384 MiB on a 1 GB Pi, so an object this
// size cannot be held in the heap at all: a provider that buffers it is asking
// the kernel to kill the server, not asking the garbage collector to work
// harder.
const TransferSize int64 = 512 << 20

// transferBytes is TransferSize for the unsigned counters the bounds are read
// from. The exported constant is signed because that is what
// storageapi.WriteResult reports and what a caller compares it against.
const transferBytes uint64 = 512 << 20

// heapBound is how much live heap the transfer may occupy. Earlier research §"nothing
// may scale with file size" budgets the entire transfer story at four concurrent
// transfers of a 1 MiB pooled buffer, so 32 MiB is eight times what one transfer
// is allowed and one sixteenth of the object being moved. A provider that lands
// between the two is buffering something proportional to the file.
const heapBound = 32 << 20

// allocBound is how many bytes the transfer may allocate in total, counting what
// has already been collected. It is the half of the measurement a sampler cannot
// miss: a provider that reads the object into memory and drops it has to have
// allocated at least TransferSize, whatever the sampler happened to observe.
const allocBound = 64 << 20

// childEnv makes the test binary run the transfer under the bound instead of
// spawning the process that does. The bound is enforced by killing the process
// that breaches it, so it cannot be enforced in the process running the suite.
const childEnv = "STORAGEAPITEST_STREAM_CHILD"

// exitOverBudget is what the child exits with when the watchdog fires. It is
// distinct from 1, which is what a failed test exits with, so the parent can say
// which of the two bounds was breached.
const exitOverBudget = 3

// measuredLine is the prefix the bounded process prints once it has moved the
// object. The parent requires it, because a -test.run that selects nothing
// exits zero and would otherwise read as a pass.
const measuredLine = "storageapitest: streamed "

// sampleInterval is how often the watchdog reads the heap. A provider that
// buffers holds the object for the rest of the copy, which is hundreds of
// milliseconds at any disk speed, so this cannot miss one.
const sampleInterval = 2 * time.Millisecond

// heapMetric and allocMetric are the two runtime/metrics counters the bounds are
// read from. Neither stops the world, which runtime.ReadMemStats does, so the
// watchdog does not slow the transfer it is measuring.
const (
	heapMetric  = "/memory/classes/heap/objects:bytes"
	allocMetric = "/gc/heap/allocs:bytes"
)

// RunStreaming proves that a provider moves an object without holding it,
// which is an earlier design decision and the property earlier research states as "nothing else
// should scale with file size".
//
// It streams an object larger than the heap a Pi-sized deployment is given
// through Save and back out through Open, inside a second process whose live
// heap is bounded. The second process is the point: a machine with plenty of
// memory streams a large file just as happily whether or not the provider
// buffers it, so the case has to be run somewhere the buffering is fatal.
//
// Call it from a provider's own test:
//
//	func TestNothingScalesWithFileSize(t *testing.T) {
//	    storageapitest.RunStreaming(t, func(t *testing.T) storageapitest.Subject {
//	        return storageapitest.Subject{Provider: ..., Root: dir}
//	    })
//	}
//
// The subject is built again inside the second process, so the factory must not
// depend on anything the parent set up.
func RunStreaming(t *testing.T, newSubject MakeSubject) {
	t.Helper()

	if testing.Short() {
		t.Skip("streaming half a gigabyte through the provider is not a short test")
	}

	if os.Getenv(childEnv) == "1" {
		streamUnderBound(t, newSubject)

		return
	}

	spawnBounded(t)
}

// spawnBounded runs this same test again in a process whose heap the watchdog
// will kill, and reports what that process said.
func spawnBounded(t *testing.T) {
	t.Helper()

	// The command is this test binary and the argument is this test's own name,
	// quoted for the regexp -test.run takes. Neither reaches here from outside
	// the process.
	//nolint:gosec // G204: os.Args[0] and t.Name() are the running test binary and its own case.
	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v")
	cmd.Env = append(os.Environ(), childEnv+"=1")

	out, err := cmd.CombinedOutput()
	report := indent(string(out))

	var exit *exec.ExitError

	switch {
	case err == nil && !strings.Contains(string(out), measuredLine):
		// A -test.run that matched nothing exits zero and prints "no tests to
		// run", which would certify every provider without moving a byte.
		t.Fatalf("the bounded process exited cleanly without streaming anything, so this case "+
			"measured nothing:\n%s", report)
	case err == nil:
		t.Log("the bounded process streamed the object and stayed inside the bound:\n" + report)
	case errors.As(err, &exit) && exit.ExitCode() == exitOverBudget:
		t.Fatalf("the provider's live heap passed %s while streaming %s, so something it does "+
			"scales with file size:\n%s", bytesAs(heapBound), bytesAs(transferBytes), report)
	default:
		t.Fatalf("the bounded process failed (%v):\n%s", err, report)
	}
}

// streamUnderBound is the body, and runs only in the spawned process.
func streamUnderBound(t *testing.T, newSubject MakeSubject) {
	t.Helper()

	subject := newSubject(t)
	ctx := t.Context()

	caps := storageapi.Probe(subject.Provider)
	if !caps.Write || !caps.Browse {
		t.Skip("streaming needs a provider that can be written to and read back")
	}

	stop := watchHeap(heapBound)
	defer stop()

	before := readMetric(allocMetric)

	const relPath = "streaming/large.gcode"

	written := crc32.NewIEEE()

	// Save is Create, copy and Commit, so the bound covers the whole of a write
	// and not just the copy: a provider that staged the object in memory and
	// published it at Commit is caught here too.
	saved, err := storageapi.Save(ctx, subject.Provider, relPath, io.TeeReader(source(TransferSize), written))
	if err != nil {
		t.Fatalf("Save streaming %s: %v", bytesAs(transferBytes), err)
	}

	if saved.Size != TransferSize {
		t.Fatalf("Save reported %d bytes, want %d", saved.Size, TransferSize)
	}

	reader, err := subject.Provider.Open(ctx, relPath)
	if err != nil {
		t.Fatalf("Open after streaming %s: %v", bytesAs(transferBytes), err)
	}

	read := crc32.NewIEEE()

	downloaded, err := io.Copy(read, reader)

	if closeErr := reader.Close(); closeErr != nil {
		t.Errorf("closing the object after reading it: %v", closeErr)
	}

	if err != nil {
		t.Fatalf("reading the object back: %v", err)
	}

	if downloaded != TransferSize {
		t.Fatalf("read %d bytes back, want %d", downloaded, TransferSize)
	}

	if written.Sum32() != read.Sum32() {
		t.Fatalf("the object read back does not match what was written: %08x != %08x",
			read.Sum32(), written.Sum32())
	}

	allocated := readMetric(allocMetric) - before
	peak := stop()

	t.Logf(measuredLine+"%s each way, peak heap %s (bound %s), allocated %s (bound %s)",
		bytesAs(transferBytes), bytesAs(peak), bytesAs(heapBound),
		bytesAs(allocated), bytesAs(allocBound))

	// The watchdog samples, so it can only report a peak it happened to see. The
	// allocation counter is cumulative and cannot be sampled past: a provider
	// that read the object into memory and let it go had to allocate it first.
	if allocated > allocBound {
		t.Fatalf("streaming %s allocated %s in total, over the %s bound, so something on the "+
			"path holds a share of the object rather than a buffer",
			bytesAs(transferBytes), bytesAs(allocated), bytesAs(allocBound))
	}
}

// watchHeap kills this process when its live heap passes limit, and returns a
// function reporting the largest heap seen. Calling the returned function stops
// the watchdog, and calling it again reports the same figure.
//
// Killing is what makes the bound a bound. A test that asserted on a heap figure
// afterwards would still have let the process allocate the object, which on the
// hardware this product runs on is the OOM killer's decision rather than the
// test's.
func watchHeap(limit uint64) func() uint64 {
	var peak atomic.Uint64

	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}

			live := readMetric(heapMetric)

			for {
				seen := peak.Load()
				if live <= seen || peak.CompareAndSwap(seen, live) {
					break
				}
			}

			if live <= limit {
				continue
			}

			fmt.Fprintf(os.Stderr,
				"storageapitest: live heap reached %s, over the %s bound\n",
				bytesAs(live), bytesAs(limit))
			os.Exit(exitOverBudget)
		}
	}()

	var once bool

	return func() uint64 {
		if !once {
			once = true

			close(done)
			<-stopped
		}

		return peak.Load()
	}
}

// readMetric reads one runtime/metrics counter. The names are compile-time
// constants of this package, so a sample that does not arrive is this package's
// bug rather than a runtime condition to handle.
func readMetric(name string) uint64 {
	sample := [1]metrics.Sample{{Name: name}}
	metrics.Read(sample[:])

	var read uint64

	for _, one := range sample {
		if one.Value.Kind() != metrics.KindUint64 {
			panic("storageapitest: " + name + " is not reported by this runtime")
		}

		read = one.Value.Uint64()
	}

	return read
}

// source is a reader of exactly n bytes of non-repeating-enough content, made
// from one small block so producing it costs nothing that scales with n.
// Zeroes would let a sparse file or a compressing filesystem hide the transfer
// rather than perform it.
func source(n int64) io.Reader {
	const blockSize = 64 << 10

	block := make([]byte, blockSize)
	for i := range block {
		// A cheap full-period generator over the byte range, so the block has no
		// run a filesystem can collapse and no dependency on math/rand.
		block[i] = byte(i*31 + i/251)
	}

	return io.LimitReader(&repeating{block: block}, n)
}

// repeating serves one block over and over, without allocating per Read.
type repeating struct {
	block  []byte
	offset int
}

func (r *repeating) Read(p []byte) (int, error) {
	written := 0

	for written < len(p) {
		copied := copy(p[written:], r.block[r.offset:])
		written += copied
		r.offset += copied

		if r.offset == len(r.block) {
			r.offset = 0
		}
	}

	return written, nil
}

// bytesAs prints a byte count the way an operator reading a failure would write
// it, because "536870912" in a message about memory is a number nobody checks.
func bytesAs(n uint64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	value, exponent := float64(n)/unit, 0
	for value >= unit && exponent < 3 {
		value /= unit
		exponent++
	}

	return fmt.Sprintf("%.1f %ciB", value, "KMGT"[exponent])
}

// indent shifts a captured process's output right, so it reads as evidence
// inside a failure rather than as more of the failure.
func indent(s string) string {
	trimmed := strings.TrimRight(s, "\n")
	if trimmed == "" {
		return "  (no output)"
	}

	return "  " + strings.ReplaceAll(trimmed, "\n", "\n  ")
}
