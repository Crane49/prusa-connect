package storageapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// filesFake is the host side under test. It records what it was asked and
// answers what the case wants.
type filesFake struct {
	info     storageapi.FileInfo
	chunk    storageapi.Chunk
	listing  storageapi.Listing
	err      error
	sawStat  storageapi.StatParams
	sawRead  storageapi.ReadParams
	sawList  storageapi.ListParams
	statCall int
	readCall int
}

func (f *filesFake) Stat(
	_ context.Context, params storageapi.StatParams,
) (storageapi.FileInfo, error) {
	f.sawStat = params
	f.statCall++

	return f.info, f.err
}

func (f *filesFake) Read(
	_ context.Context, params storageapi.ReadParams,
) (storageapi.Chunk, error) {
	f.sawRead = params
	f.readCall++

	return f.chunk, f.err
}

func (f *filesFake) List(
	_ context.Context, params storageapi.ListParams,
) (storageapi.Listing, error) {
	f.sawList = params

	return f.listing, f.err
}

func (f *filesFake) Fetch(
	_ context.Context, params storageapi.FetchParams, w io.Writer,
) (storageapi.Fetched, error) {
	f.sawRead = storageapi.ReadParams(params)
	f.readCall++

	if f.err != nil {
		return storageapi.Fetched{}, f.err
	}

	n, err := w.Write(f.chunk.Data)

	return storageapi.Fetched{Size: f.chunk.Size, Length: n}, err
}

func (f *filesFake) Write(
	_ context.Context, _ storageapi.WriteParams, _ io.Reader,
) (storageapi.Written, error) {
	return storageapi.Written{}, f.err
}

// answer installs the method set over one fake and runs one call through it,
// the way a session would, returning what crossed.
func answer(
	t *testing.T, files storageapi.Files, method wirerpc.Method, params any,
) (any, error) {
	t.Helper()

	handler, installed := storageapi.Handlers(files)[method]
	if !installed {
		t.Fatalf("%s is not in the method set", method)
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode the params: %v", err)
	}

	return handler(t.Context(), wirerpc.Call{Method: method, Params: encoded})
}

// A read over the published range is refused on the client's side, before a
// round trip, and again at the host, because a limit checked only in a client
// is a limit a peer that is not this client never made.
func TestAReadOverTheRangeIsRefusedOnBothSides(t *testing.T) {
	t.Parallel()

	over := storageapi.ReadParams{
		Storage: "bench", Path: "part.gcode", Length: storageapi.MaxReadBytes + 1,
	}

	if err := over.Validate(); !errors.Is(err, storageapi.ErrInvalidRange) {
		t.Fatalf("the client held the read to be %v, want %v", err, storageapi.ErrInvalidRange)
	}

	files := &filesFake{}

	_, err := answer(t, files, storageapi.MethodRead, over)

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Code != storageapi.CodeInvalidRange {
		t.Fatalf("the host answered %v, want code %d", err, storageapi.CodeInvalidRange)
	}

	if files.readCall != 0 {
		t.Fatal("a read outside the published bounds reached the host's storages")
	}
}

// A negative offset and a negative length are the same refusal, and neither
// reaches the host.
func TestANegativeRangeIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]storageapi.ReadParams{
		"the offset": {Storage: "bench", Path: "part.gcode", Offset: -1},
		"the length": {Storage: "bench", Path: "part.gcode", Length: -1},
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := params.Validate(); !errors.Is(err, storageapi.ErrInvalidRange) {
				t.Fatalf("the params were held to be %v, want %v", err, storageapi.ErrInvalidRange)
			}
		})
	}
}

// A call naming no storage and one naming no file are the same refusal as a
// storage that is not there, so nothing in the shape of a request says what
// this server holds.
func TestACallNamingNoTargetIsNoFile(t *testing.T) {
	t.Parallel()

	cases := map[string]storageapi.StatParams{
		"no storage":     {Path: "part.gcode"},
		"no file":        {Storage: "bench"},
		"a long storage": {Storage: strings.Repeat("a", storageapi.MaxStorageBytes+1), Path: "p"},
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := params.Validate(); !errors.Is(err, storageapi.ErrNoFile) {
				t.Fatalf("the params were held to be %v, want %v", err, storageapi.ErrNoFile)
			}
		})
	}
}

// A path over the bound is refused, and the refusal says the path was too
// long rather than anything about what the storage holds.
func TestAPathOverTheBoundIsRefused(t *testing.T) {
	t.Parallel()

	params := storageapi.StatParams{
		Storage: "bench", Path: strings.Repeat("a", storageapi.MaxPathBytes+1),
	}

	if err := params.Validate(); !errors.Is(err, storageapi.ErrInvalidPath) {
		t.Fatalf("the params were held to be %v, want %v", err, storageapi.ErrInvalidPath)
	}
}

// The host reads the params as the module wrote them, and the answer comes
// back whole.
func TestTheHostReadsAReadAsItWasWritten(t *testing.T) {
	t.Parallel()

	files := &filesFake{chunk: storageapi.Chunk{Data: []byte("G28\n"), Size: 4, EOF: true}}

	params := storageapi.ReadParams{
		Principal: "user:alpha", Storage: "bench", Path: "models/part.gcode",
		Offset: 128, Length: 4096,
	}

	answered, err := answer(t, files, storageapi.MethodRead, params)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if files.sawRead != params {
		t.Fatalf("the host was asked for %+v, want %+v", files.sawRead, params)
	}

	chunk, ok := answered.(storageapi.Chunk)
	if !ok || string(chunk.Data) != "G28\n" || !chunk.EOF {
		t.Fatalf("the answer is %+v, want the range the host read", answered)
	}
}

// Every refusal the host raises crosses as the code this contract numbers, so
// a module branches on the refusal and not on a message.
func TestAStorageRefusalCrossesAsItsSentinel(t *testing.T) {
	t.Parallel()

	cases := map[error]int{
		storageapi.ErrNoFile:     storageapi.CodeNoFile,
		storageapi.ErrUnreadable: storageapi.CodeUnreadable,
		storageapi.ErrDenied:     storageapi.CodeDenied,
	}

	for sentinel, code := range cases {
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()

			files := &filesFake{err: sentinel}

			_, err := answer(t, files, storageapi.MethodStat,
				storageapi.StatParams{Storage: "bench", Path: "part.gcode"})

			var coded *wirerpc.Error
			if !errors.As(err, &coded) || coded.Code != code {
				t.Fatalf("the refusal crossed as %v, want code %d", err, code)
			}
		})
	}
}

// A failure of the host's own crosses as one sentence and never as its text,
// which names this host's internals.
func TestAHostFailureCrossesAsOneSentence(t *testing.T) {
	t.Parallel()

	files := &filesFake{err: errors.New("dial 10.0.0.4:9000: connection refused")}

	_, err := answer(t, files, storageapi.MethodStat,
		storageapi.StatParams{Storage: "bench", Path: "part.gcode"})

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Message != storageapi.HostFailed {
		t.Fatalf("the failure crossed as %v, want %q", err, storageapi.HostFailed)
	}
}

// A host that serves no storage installs no method, so a module reads
// CodeMethodNotFound on its first call rather than reaching a nil.
func TestAHostWithNoStorageInstallsNoMethod(t *testing.T) {
	t.Parallel()

	if held := storageapi.Handlers(nil); len(held) != 0 {
		t.Fatalf("a host that serves no storage installed %v", held)
	}
}

// loopback is a session that answers a call with the host's own method set,
// so the client and the host are tested together with no process between
// them.
type loopback struct{ methods wirerpc.Methods }

func (l loopback) Call(
	ctx context.Context, method wirerpc.Method, params any,
) (json.RawMessage, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	answered, err := l.methods[method](ctx, wirerpc.Call{Method: method, Params: encoded})
	if err != nil {
		return nil, err
	}

	return json.Marshal(answered)
}

// A listing crosses the wire whole: the params as the module wrote them, and
// every entry and the cursor as the host answered them.
func TestAListCrossesTheWireWhole(t *testing.T) {
	t.Parallel()

	want := storageapi.Listing{
		Entries: []storageapi.ListEntry{
			{Name: "models", Dir: true, MTime: 1761210000},
			{Name: "part.gcode", Size: 4, MTime: 1761210000, SHA256: strings.Repeat("ab", 32)},
		},
		Next: "part.gcode",
	}

	files := &filesFake{listing: want}
	params := storageapi.ListParams{
		Principal: "user:alpha", Storage: "bench", Path: "shelf", Cursor: "a.gcode",
	}

	got, err := storageapi.NewClient(loopback{storageapi.Handlers(files)}).List(t.Context(), params)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the module read %+v, want %+v", got, want)
	}

	if files.sawList != params {
		t.Fatalf("the host was asked for %+v, want %+v", files.sawList, params)
	}
}

// A refusal of a listing reaches the module as the sentinel the host raised.
func TestARefusedListCrossesAsItsSentinel(t *testing.T) {
	t.Parallel()

	files := &filesFake{err: storageapi.ErrDenied}

	_, err := storageapi.NewClient(loopback{storageapi.Handlers(files)}).
		List(t.Context(), storageapi.ListParams{Storage: "bench"})
	if !errors.Is(err, storageapi.ErrDenied) {
		t.Fatalf("the refusal crossed as %v, want %v", err, storageapi.ErrDenied)
	}
}

// A listing of the storage's root names no path, and that is a call. A
// storage, a path and a cursor are held to the published bounds.
func TestAListIsHeldToTheBounds(t *testing.T) {
	t.Parallel()

	if err := (storageapi.ListParams{Storage: "bench"}).Validate(); err != nil {
		t.Fatalf("a listing of the root was refused: %v", err)
	}

	long := strings.Repeat("a", storageapi.MaxPathBytes+1)
	cases := map[string]struct {
		params storageapi.ListParams
		want   error
	}{
		"no storage":    {storageapi.ListParams{}, storageapi.ErrNoFile},
		"a long path":   {storageapi.ListParams{Storage: "bench", Path: long}, storageapi.ErrInvalidPath},
		"a long cursor": {storageapi.ListParams{Storage: "bench", Cursor: long}, storageapi.ErrInvalidPath},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := held.params.Validate(); !errors.Is(err, held.want) {
				t.Fatalf("the params were held to be %v, want %v", err, held.want)
			}
		})
	}
}
