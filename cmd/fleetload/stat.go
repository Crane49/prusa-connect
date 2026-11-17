package main

import (
	"encoding/json"
	"math"
	"net/http"
	"runtime"
	"runtime/metrics"
	"slices"
	"time"
)

// statPath is where the server child answers its counters.
const statPath = "/stat"

// The runtime/metrics names one snapshot reads.
const (
	metricCPUTotal    = "/cpu/classes/total:cpu-seconds"
	metricCPUIdle     = "/cpu/classes/idle:cpu-seconds"
	metricCPUGC       = "/cpu/classes/gc/total:cpu-seconds"
	metricAllocBytes  = "/gc/heap/allocs:bytes"
	metricAllocObject = "/gc/heap/allocs:objects"
	metricMemTotal    = "/memory/classes/total:bytes"
	metricMemReleased = "/memory/classes/heap/released:bytes"
	metricHeapLive    = "/memory/classes/heap/objects:bytes"
	metricGoroutines  = "/sched/goroutines:goroutines"
	metricGCCycles    = "/gc/cycles/total:gc-cycles"
	metricGCPauses    = "/gc/pauses:seconds"
)

// snapshot is one reading of the server's runtime counters. The CPU,
// allocation, GC and pause fields are cumulative: a rate is the difference
// between two readings.
type snapshot struct {
	At           time.Time `json:"at"`
	MaxProcs     int       `json:"maxprocs"`
	CPUTotal     float64   `json:"cpu_total_seconds"`
	CPUIdle      float64   `json:"cpu_idle_seconds"`
	CPUGC        float64   `json:"cpu_gc_seconds"`
	AllocBytes   uint64    `json:"alloc_bytes"`
	AllocObjects uint64    `json:"alloc_objects"`
	MemTotal     uint64    `json:"memory_total_bytes"`
	MemReleased  uint64    `json:"memory_released_bytes"`
	HeapLive     uint64    `json:"heap_live_bytes"`
	Goroutines   uint64    `json:"goroutines"`
	GCCycles     uint64    `json:"gc_cycles"`
	PauseBounds  []float64 `json:"pause_bounds"`
	PauseCounts  []uint64  `json:"pause_counts"`
	// OSCPU is the CPU time the operating system charged this process, see
	// processCPU.
	OSCPU time.Duration `json:"os_cpu_nanoseconds"`
}

// InUse is the memory the runtime has mapped and not released. GOMEMLIMIT
// limits this number.
func (s snapshot) InUse() uint64 { return s.MemTotal - s.MemReleased }

func statNames() []string {
	return []string{
		metricCPUTotal, metricCPUIdle, metricCPUGC,
		metricAllocBytes, metricAllocObject,
		metricMemTotal, metricMemReleased, metricHeapLive,
		metricGoroutines, metricGCCycles, metricGCPauses,
	}
}

// readStat samples the runtime.
func readStat() snapshot {
	names := statNames()
	samples := make([]metrics.Sample, len(names))

	for i, name := range names {
		samples[i].Name = name
	}

	metrics.Read(samples)

	out := snapshot{At: time.Now(), MaxProcs: runtime.GOMAXPROCS(0)}
	out.OSCPU, _ = processCPU()

	for _, sample := range samples {
		fill(&out, sample)
	}

	return out
}

// fill copies one sample into the snapshot.
func fill(out *snapshot, sample metrics.Sample) {
	switch sample.Name {
	case metricCPUTotal:
		out.CPUTotal = sample.Value.Float64()
	case metricCPUIdle:
		out.CPUIdle = sample.Value.Float64()
	case metricCPUGC:
		out.CPUGC = sample.Value.Float64()
	case metricAllocBytes:
		out.AllocBytes = sample.Value.Uint64()
	case metricAllocObject:
		out.AllocObjects = sample.Value.Uint64()
	case metricMemTotal:
		out.MemTotal = sample.Value.Uint64()
	case metricMemReleased:
		out.MemReleased = sample.Value.Uint64()
	case metricHeapLive:
		out.HeapLive = sample.Value.Uint64()
	case metricGoroutines:
		out.Goroutines = sample.Value.Uint64()
	case metricGCCycles:
		out.GCCycles = sample.Value.Uint64()
	case metricGCPauses:
		histogram := sample.Value.Float64Histogram()
		out.PauseBounds = finite(histogram.Buckets)
		out.PauseCounts = histogram.Counts
	}
}

// finite replaces the infinite outer bounds of a runtime histogram, because
// JSON cannot encode infinity. The lowest becomes zero and the highest
// becomes the bound below it.
func finite(bounds []float64) []float64 {
	out := slices.Clone(bounds)

	for i, bound := range out {
		if !math.IsInf(bound, 0) {
			continue
		}

		if i == 0 {
			out[i] = 0

			continue
		}

		out[i] = out[i-1]
	}

	return out
}

func writeStat(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(readStat()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// pauseQuantile returns one quantile of the GC pauses between two readings.
// It returns the upper bound of the bucket, so it never understates a pause.
func pauseQuantile(first, last snapshot, quantile float64) time.Duration {
	if len(last.PauseCounts) == 0 || len(first.PauseCounts) != len(last.PauseCounts) {
		return 0
	}

	var total uint64
	for i := range last.PauseCounts {
		total += last.PauseCounts[i] - first.PauseCounts[i]
	}

	if total == 0 {
		return 0
	}

	want := uint64(quantile * float64(total))

	var seen uint64

	for i := range last.PauseCounts {
		seen += last.PauseCounts[i] - first.PauseCounts[i]
		if seen >= want {
			return seconds(last.PauseBounds[i+1])
		}
	}

	return seconds(last.PauseBounds[len(last.PauseBounds)-1])
}

// pauseCount is how many GC pauses happened between two readings.
func pauseCount(first, last snapshot) uint64 {
	var total uint64

	for i := range last.PauseCounts {
		if i < len(first.PauseCounts) {
			total += last.PauseCounts[i] - first.PauseCounts[i]
		}
	}

	return total
}

const maxDuration = time.Duration(1<<63 - 1)

// seconds converts a bucket bound to a duration and clamps it to maxDuration.
func seconds(value float64) time.Duration {
	if value > maxDuration.Seconds() {
		return maxDuration
	}

	return time.Duration(value * float64(time.Second))
}
