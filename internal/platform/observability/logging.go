package observability

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// The sampling defaults, in zap's shape: the first few lines of one message per
// tick, then one in many.
//
// Sampling applies below info only. A warning or an error is rare and is the
// line somebody is reading. A debug line inside a loop over a whole fleet is
// the one that fills a disk.
const (
	// DefaultLogSampleFirst is how many lines of one message per tick pass
	// before sampling begins.
	DefaultLogSampleFirst = 10
	// DefaultLogSampleThereafter is the divisor applied after that: one line in
	// this many is kept.
	DefaultLogSampleThereafter = 100
	// DefaultLogSampleTick is the window the count resets over.
	DefaultLogSampleTick = time.Second
	// maxSampledMessages bounds the per-tick table. A log message is a constant
	// by convention, so code that follows the convention never reaches this
	// bound, and code that does not cannot turn the sampler into a leak.
	maxSampledMessages = 1024
)

// The level label's values. They are the slog names. A level between two of
// them is reported as the standard level at or below it, so a custom level does
// not open a series of its own.
const (
	levelDebug = "debug"
	levelInfo  = "info"
	levelWarn  = "warn"
	levelError = "error"
)

// logLevels is every value the level label takes. A constructor binds one
// handle per entry.
var logLevels = []string{levelDebug, levelInfo, levelWarn, levelError}

// LogLevels returns every value the level label takes, so a composition root
// can resolve one handle per level and refuse a registry that will not answer
// for one of them. A failed lookup would leave that level counted nowhere.
func LogLevels() []string { return slices.Clone(logLevels) }

// LogSampling is how much of the noise below info reaches the log.
//
// The zero value is the defaults above. Thereafter of 1 keeps everything, for
// a deployment debugging one printer.
type LogSampling struct {
	First      int
	Thereafter int
	Tick       time.Duration
}

// LogConfig is what a counting handler is built from.
type LogConfig struct {
	// Emitted returns the counter for lines passed on at one level, and
	// Dropped the counter for lines sampling discarded there. Both are called
	// once per level at construction, so nothing looks a handle up per line.
	// A nil function records nothing, which is a build with no registry.
	Emitted func(level string) Counter
	Dropped func(level string) Counter

	// Sampling is what happens below info. The zero value is the defaults.
	Sampling LogSampling

	// Now is the clock the tick is measured on. Nil is time.Now.
	Now func() time.Time
}

// LogHandler counts every line it passes on into pcl_log_lines_total and every
// line sampling discarded into pcl_log_lines_dropped_total, both by level.
//
// Both series exist because uncounted sampling is invisible. An operator
// reading a debug volume that suddenly fell cannot otherwise tell a quiet
// process from a sampled one.
//
// It counts what it emits, not what it was offered. The dropped counter is the
// other half of the same total.
type LogHandler struct {
	inner   slog.Handler
	emitted map[string]Counter
	dropped map[string]Counter
	sample  *sampler
}

var _ slog.Handler = (*LogHandler)(nil)

// NewLogHandler wraps inner. The wrapper decides everything in Handle. Enabled
// returns the inner handler's answer unchanged: a caller that skipped building
// a record on a sampled level would let sampling change what the process
// computes and not only what it writes.
func NewLogHandler(inner slog.Handler, cfg LogConfig) *LogHandler {
	return &LogHandler{
		inner:   inner,
		emitted: bindLevels(cfg.Emitted),
		dropped: bindLevels(cfg.Dropped),
		sample:  newSampler(cfg.Sampling, cfg.Now),
	}
}

// Enabled reports what the wrapped handler reports.
func (h *LogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle samples, counts and passes on.
//
// ctx.Done() is not consulted. A cancelled request still logs its own
// cancellation, and dropping that line would delete the evidence of the case
// somebody is investigating.
func (h *LogHandler) Handle(ctx context.Context, record slog.Record) error {
	level := LogLevelName(record.Level)

	if !h.sample.allow(record.Level, record.Message) {
		count(h.dropped, level)

		return nil
	}

	count(h.emitted, level)

	return h.inner.Handle(ctx, record)
}

// WithAttrs returns a handler over the same counters and the same sampler. A
// per-request logger built by WithAttrs is the usual shape here, and a sampler
// per logger would sample nothing.
func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.with(h.inner.WithAttrs(attrs))
}

// WithGroup returns a handler over the same counters and the same sampler.
func (h *LogHandler) WithGroup(name string) slog.Handler {
	return h.with(h.inner.WithGroup(name))
}

func (h *LogHandler) with(inner slog.Handler) slog.Handler {
	return &LogHandler{inner: inner, emitted: h.emitted, dropped: h.dropped, sample: h.sample}
}

// LogLevelName is the value of the level label for one slog level: the standard
// level at or below it, so a custom level does not open a series of its own.
func LogLevelName(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return levelError
	case level >= slog.LevelWarn:
		return levelWarn
	case level >= slog.LevelInfo:
		return levelInfo
	}

	return levelDebug
}

// bindLevels resolves one handle per level once, so the log path reads a small
// fixed map and never asks the registry.
func bindLevels(lookup func(string) Counter) map[string]Counter {
	if lookup == nil {
		return nil
	}

	handles := make(map[string]Counter, len(logLevels))

	for _, level := range logLevels {
		if handle := lookup(level); handle != nil {
			handles[level] = handle
		}
	}

	return handles
}

func count(handles map[string]Counter, level string) {
	if handle, ok := handles[level]; ok {
		handle.Add(1)
	}
}

// sampler is zap's rule: the first few lines of one message per tick, then one
// in many. It is keyed on the message, which is a constant by convention here
// while the varying part of a line is an attribute, so the key identifies the
// log site and not the event.
type sampler struct {
	first      int
	thereafter int
	tick       time.Duration
	now        func() time.Time

	mu      sync.Mutex
	started time.Time
	seen    map[string]int
}

func newSampler(cfg LogSampling, now func() time.Time) *sampler {
	if now == nil {
		now = time.Now
	}

	s := &sampler{
		first:      cfg.First,
		thereafter: cfg.Thereafter,
		tick:       cfg.Tick,
		now:        now,
		seen:       map[string]int{},
	}

	if s.first <= 0 {
		s.first = DefaultLogSampleFirst
	}

	if s.thereafter <= 0 {
		s.thereafter = DefaultLogSampleThereafter
	}

	if s.tick <= 0 {
		s.tick = DefaultLogSampleTick
	}

	return s
}

// allow reports whether this record is written. Info and above always are.
// Sampling a warning would hide the line somebody is looking for, and save a
// volume that is already small.
func (s *sampler) allow(level slog.Level, message string) bool {
	if level >= slog.LevelInfo {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.started.IsZero() || now.Sub(s.started) >= s.tick || len(s.seen) >= maxSampledMessages {
		s.started = now
		clear(s.seen)
	}

	s.seen[message]++
	seen := s.seen[message]

	if seen <= s.first {
		return true
	}

	return (seen-s.first)%s.thereafter == 0
}
