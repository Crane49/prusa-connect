package app

import (
	"context"
	"errors"
	"sync"
	"time"
	"unsafe"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/printfile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fileapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A module that reads a file type answers for the core. When no module
// answers, the core's own reader reads the file. Nothing here knows a format:
// a module names its types, and the answer has the shape sdk/fileapi
// publishes.

// The time the core gives a module. A caller waits for its own limit and then
// reads the file itself. The call goes on until it ends, and its answer is
// kept for the next caller.
const (
	// analysisListWait is what the picture in a listing waits.
	analysisListWait = 2 * time.Second
	// analysisDetailWait is what a file's details and a printer's needs wait.
	analysisDetailWait = 20 * time.Second
	// analysisCallBudget is how long one call to a module may run, waiting
	// for a free place included.
	analysisCallBudget = 20 * time.Second
	// analysisSlots is how many calls run at once for one module.
	analysisSlots = 2
	// analysisEntries is how many answers are kept at once.
	analysisEntries = 256
	// analysisCacheShare divides core.preview_cache_bytes: the answers keep
	// one part of it and the core's own previews the rest.
	analysisCacheShare = 4
)

// noAnalysisWait marks a context whose caller takes a kept answer or the
// core's own reader, and never waits for a module.
type noAnalysisWait struct{}

// withoutAnalysisWait is the context of a module's own storage call. Without
// it, a module that reads the file it is describing through the storage wire
// would wait for its own answer.
func withoutAnalysisWait(ctx context.Context) context.Context {
	return context.WithValue(ctx, noAnalysisWait{}, true)
}

// analysisWaitOf is how long a caller of this context waits for a module.
func analysisWaitOf(ctx context.Context, wait time.Duration) time.Duration {
	if skip, _ := ctx.Value(noAnalysisWait{}).(bool); skip {
		return 0
	}

	return wait
}

// analysts is the supervision plane as this file reads it.
type analysts interface {
	AnalystFor(name string) (resourcehost.Analyst, bool)
	Analyze(ctx context.Context, analyst resourcehost.Analyst, params fileapi.Params) (fileapi.Analysis, error)
}

// fileAnalysis asks the module that reads a file's type, keeps what it
// answered, and runs one call per file version.
type fileAnalysis struct {
	host    analysts
	handles *fileHandles

	mu       sync.Mutex
	answers  map[analysisKey]analysisAnswer
	order    []analysisKey
	bytes    int
	maxBytes int
	flights  map[analysisKey]*analysisFlight
	slots    map[string]chan struct{}
}

// analysisKey is one version of one file as one release of one module reads
// it. A new file version or a new release is a new key, so nothing is
// invalidated.
type analysisKey struct {
	file    previewKey
	module  string
	release string
}

// analysisAnswer is what one call left: the preview the core serves, the
// facts the page lists, or that the module failed.
type analysisAnswer struct {
	preview printfile.Preview
	facts   *httpapi.FileAnalysis
	failed  bool
}

// analysisFlight is one call in progress. done is closed when answer is in.
type analysisFlight struct {
	done   chan struct{}
	answer analysisAnswer
}

func newFileAnalysis(host analysts, handles *fileHandles, maxBytes int) *fileAnalysis {
	return &fileAnalysis{
		host:     host,
		handles:  handles,
		answers:  map[analysisKey]analysisAnswer{},
		flights:  map[analysisKey]*analysisFlight{},
		slots:    map[string]chan struct{}{},
		maxBytes: maxBytes,
	}
}

// read answers what the module that reads the file's type said, when one
// does and it answered within wait. ok is false when the core reads the file
// itself. A wait of zero takes a kept answer and asks nothing.
func (a *fileAnalysis) read(
	ctx context.Context, storage string, info storageapi.FileInfo, wait time.Duration,
) (analysisAnswer, bool) {
	if a == nil || a.host == nil {
		return analysisAnswer{}, false
	}

	analyst, found := a.host.AnalystFor(info.Name)
	if !found {
		return analysisAnswer{}, false
	}

	key := analysisKey{
		file:   previewKey{storage: storage, path: info.Path, size: info.Size, modified: info.MTime},
		module: analyst.Namespace, release: analyst.Release,
	}

	a.mu.Lock()

	if held, kept := a.answers[key]; kept {
		a.mu.Unlock()

		return held, !held.failed
	}

	if wait <= 0 {
		a.mu.Unlock()

		return analysisAnswer{}, false
	}

	flight, running := a.flights[key]
	if !running {
		flight = &analysisFlight{done: make(chan struct{})}
		a.flights[key] = flight

		// The call outlives this caller: a listing that stops waiting after
		// two seconds still leaves the answer for the next one. The call
		// budget ends it.
		go a.run(context.WithoutCancel(ctx), key, flight, analyst, storage, info)
	}

	a.mu.Unlock()

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-flight.done:
		return flight.answer, !flight.answer.failed
	case <-timer.C:
		return analysisAnswer{}, false
	case <-ctx.Done():
		return analysisAnswer{}, false
	}
}

// run makes one call and files its answer. A call that ran out of time, or
// that the module never answered, is not filed, so the next caller asks
// again.
func (a *fileAnalysis) run(
	ctx context.Context, key analysisKey, flight *analysisFlight,
	analyst resourcehost.Analyst, storage string, info storageapi.FileInfo,
) {
	ctx, cancel := context.WithTimeout(ctx, analysisCallBudget)
	defer cancel()

	answer, keep := a.ask(ctx, analyst, storage, info)

	a.mu.Lock()
	if keep {
		a.store(key, answer)
	}

	delete(a.flights, key)
	a.mu.Unlock()

	flight.answer = answer
	close(flight.done)
}

// ask waits for a free place on the module and makes the call through a read
// handle for this one file. keep says whether the answer is the module's
// own: an analysis, a refusal it sent, or an answer over a bound. A call
// that ran out of time or reached a module that stopped is not.
func (a *fileAnalysis) ask(
	ctx context.Context, analyst resourcehost.Analyst, storage string, info storageapi.FileInfo,
) (analysisAnswer, bool) {
	slot := a.slotOf(analyst.Namespace)

	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return analysisAnswer{failed: true}, false
	}

	handle, end := a.handles.open(sinkPrincipal(analyst.Instance), storage, info)
	defer end()

	answered, err := a.host.Analyze(ctx, analyst, fileapi.Params{
		Handle: handle, Name: info.Name, Size: info.Size, Modified: info.MTime,
	})
	if err != nil {
		_, refused := errors.AsType[*wirerpc.Error](err)

		return analysisAnswer{failed: true}, ctx.Err() == nil && (refused || errors.Is(err, fileapi.ErrOutOfBounds))
	}

	return analysisAnswerOf(analyst.Namespace, answered), true
}

// slotOf is the places one module's calls take turns on.
func (a *fileAnalysis) slotOf(namespace string) chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	slot, ok := a.slots[namespace]
	if !ok {
		slot = make(chan struct{}, analysisSlots)
		a.slots[namespace] = slot
	}

	return slot
}

// store files one answer and drops the oldest until both bounds hold. The
// caller holds a.mu.
func (a *fileAnalysis) store(key analysisKey, answer analysisAnswer) {
	if _, seen := a.answers[key]; seen {
		return
	}

	a.answers[key] = answer
	a.order = append(a.order, key)
	a.bytes += analysisBytes(key, answer)

	for len(a.order) > analysisEntries || (a.bytes > a.maxBytes && len(a.order) > 1) {
		oldest := a.order[0]
		a.order = a.order[1:]
		a.bytes -= analysisBytes(oldest, a.answers[oldest])
		delete(a.answers, oldest)
	}
}

// analysisBytes is what one kept answer holds in memory.
func analysisBytes(key analysisKey, answer analysisAnswer) int {
	held := previewBytes(key.file, answer.preview) + len(key.module) + len(key.release)

	if answer.facts != nil {
		held += int(unsafe.Sizeof(*answer.facts)) + len(answer.facts.Title)
		for _, fact := range answer.facts.Facts {
			held += len(fact.Key) + len(fact.Label) + len(fact.Value)
		}

		for _, object := range answer.facts.Objects {
			held += len(object.ID) + len(object.Name)
		}
	}

	return held
}

// analysisAnswerOf turns a module's answer into what the core serves: the picture,
// decoded again here as the core's own reader decodes one, the details and
// the needs, and the facts the page lists.
func analysisAnswerOf(namespace string, answered fileapi.Analysis) analysisAnswer {
	out := analysisAnswer{
		preview: printfile.Preview{Thumbnail: pictureOf(answered.Thumbnails)},
		facts:   &httpapi.FileAnalysis{Provider: namespace, Title: answered.Title},
	}

	if answered.Print != nil {
		out.preview.Details = detailsFromModule(*answered.Print)
	}

	for _, fact := range answered.Facts {
		out.facts.Facts = append(out.facts.Facts, httpapi.FileFact(fact))
	}

	for _, object := range answered.Objects {
		out.facts.Objects = append(out.facts.Objects, httpapi.FileObject(object))
	}

	return out
}

// preferredPictureEdge is the long edge a page wants, as the core's own
// reader prefers it.
const preferredPictureEdge = 1024

// pictureOf picks the largest picture whose long edge is at most
// preferredPictureEdge, or the smallest one when all are larger, and keeps it
// only when it decodes.
func pictureOf(pictures []fileapi.Thumbnail) *printfile.Thumbnail {
	var chosen *fileapi.Thumbnail

	for index := range pictures {
		picture := &pictures[index]
		edge := max(picture.Width, picture.Height)

		switch {
		case chosen == nil:
			chosen = picture
		case edge <= preferredPictureEdge && (max(chosen.Width, chosen.Height) > preferredPictureEdge ||
			edge > max(chosen.Width, chosen.Height)):
			chosen = picture
		case edge > preferredPictureEdge && max(chosen.Width, chosen.Height) > edge:
			chosen = picture
		}
	}

	if chosen == nil {
		return nil
	}

	return printfile.Picture(chosen.Data)
}
