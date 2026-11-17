package files_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// fakeNode records the instances registered with it and every report.
type fakeNode struct {
	mu        sync.Mutex
	instances map[fractal.InstanceID]fractalnode.Instance
	reports   []report
}

// report is one state report, with the root hash it carried and the names of
// the properties it carried.
type report struct {
	Instance fractal.InstanceID
	RootHash string
	Names    []fractal.PropertyName
}

func (n *fakeNode) Register(inst fractalnode.Instance) (files.Reporter, error) {
	if err := inst.Instance.Validate(); err != nil {
		return nil, err
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.instances == nil {
		n.instances = map[fractal.InstanceID]fractalnode.Instance{}
	}

	n.instances[inst.Instance] = inst

	return reporterFunc(func(_ context.Context, props fractal.Props, _ fractal.Timestamp) error {
		if err := inst.Definition.ValidateREP(props); err != nil {
			return err
		}

		n.mu.Lock()
		defer n.mu.Unlock()

		n.reports = append(n.reports, report{
			Instance: inst.Instance, RootHash: string(props["rootHash"]), Names: slices.Sorted(maps.Keys(props)),
		})

		return nil
	}), nil
}

func (n *fakeNode) instance(t *testing.T, id fractal.InstanceID) fractalnode.Instance {
	t.Helper()

	n.mu.Lock()
	defer n.mu.Unlock()

	inst, ok := n.instances[id]
	if !ok {
		t.Fatalf("no instance %s is registered", id)
	}

	return inst
}

type reporterFunc func(ctx context.Context, props fractal.Props, at fractal.Timestamp) error

func (f reporterFunc) Report(ctx context.Context, props fractal.Props, at fractal.Timestamp) error {
	return f(ctx, props, at)
}

// runStores runs the content stores of p on a fake node until the test ends.
func runStores(t *testing.T, p *publishing) *fakeNode {
	t.Helper()

	node := &fakeNode{}

	stores, err := files.NewContentStores(p.publisher, node, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("build the content stores: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = stores.Run(ctx)

		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return node
}

// latest is the last report of every instance.
func (n *fakeNode) latest() map[fractal.InstanceID]string {
	n.mu.Lock()
	defer n.mu.Unlock()

	out := map[fractal.InstanceID]string{}
	for _, one := range n.reports {
		out[one.Instance] = one.RootHash
	}

	return out
}

// awaitLatest waits until the last report of every instance is the one in
// want. A report reads the root hash when it is sent, so only the last one is
// certain.
func awaitLatest(t *testing.T, node *fakeNode, want map[fractal.InstanceID]string) {
	t.Helper()

	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if reflect.DeepEqual(node.latest(), want) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("the node last reported %v, want %v", node.latest(), want)
}

func quoted(id fractalfiles.Identity) string { return `"` + id.Text() + `"` }

// TestStoreReportsRootHash: a storage is registered with no root hash while
// no scan of it has ended, and reported with its root hash after one.
func TestStoreReportsRootHash(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.share(t, "local", "lib")
	p.st.breakFolder("local", "lib", true)
	node := runStores(t, p)

	if err := p.publisher.Scan(t.Context()); err == nil {
		t.Fatal("a scan of a folder the storage cannot list returned no error")
	}

	awaitLatest(t, node, map[fractal.InstanceID]string{"local": "null"})

	p.st.breakFolder("local", "lib", false)
	p.scan(t)

	root := quoted(p.root(t, "local"))
	awaitLatest(t, node, map[fractal.InstanceID]string{"local": root})

	props, _, err := node.instance(t, "local").Owner.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	want := fractal.Props{
		"rootHash":     json.RawMessage(root),
		"mediaPresent": json.RawMessage(`true`),
		"removable":    json.RawMessage(`false`),
		"capacity":     json.RawMessage(`null`),
		"free":         json.RawMessage(`null`),
	}
	if !reflect.DeepEqual(props, want) {
		t.Errorf("Snapshot is %s, want %s", props, want)
	}
}

// TestStoreReportsOnlyChanges: the first report of an instance carries every
// property, and a later one only the root hash, the one property that changes
// (fractal.md, Reported Properties Message).
func TestStoreReportsOnlyChanges(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.share(t, "local", "lib")
	node := runStores(t, p)
	p.scan(t)
	first := quoted(p.root(t, "local"))
	awaitLatest(t, node, map[fractal.InstanceID]string{"local": first})

	p.st.put("local", "lib/b.gcode", "b", true)
	p.scan(t)
	second := quoted(p.root(t, "local"))
	awaitLatest(t, node, map[fractal.InstanceID]string{"local": second})

	node.mu.Lock()
	defer node.mu.Unlock()

	every := []fractal.PropertyName{"capacity", "free", "mediaPresent", "removable", "rootHash"}
	only := []fractal.PropertyName{"rootHash"}

	// The stores may handle the first announcement before the first scan
	// installed its tree, and then report the root hash null first.
	want := []report{{"local", first, every}, {"local", second, only}}
	if len(node.reports) > 0 && node.reports[0].RootHash == "null" {
		want = []report{{"local", "null", every}, {"local", first, only}, {"local", second, only}}
	}

	if !reflect.DeepEqual(node.reports, want) {
		t.Errorf("the reports were %v, want %v", node.reports, want)
	}
}

// TestStoreNamedByStorage: a storage name is a DNS label, and its hyphen
// becomes an underscore in the instance id, which follows the Identifier grammar.
func TestStoreNamedByStorage(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("nas-1", "lib/a.gcode", "a", true)
	p.share(t, "nas-1", "lib")
	node := runStores(t, p)
	p.scan(t)

	awaitLatest(t, node, map[fractal.InstanceID]string{"nas_1": quoted(p.root(t, "nas-1"))})
}

// TestStoreStaysAfterRemoval: a storage the operator removed keeps its
// instance and reports no root hash.
func TestStoreStaysAfterRemoval(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.storages = []string{"usb"}
	p.st.put("usb", "x.gcode", "x", true)
	node := runStores(t, p)
	p.scan(t)
	awaitLatest(t, node, map[fractal.InstanceID]string{"usb": quoted(fractalfiles.EmptyListing)})

	p.st.dropStorage("usb")
	p.scan(t)
	awaitLatest(t, node, map[fractal.InstanceID]string{"usb": "null"})
}

// TestListAnswers: List answers a listing the instance publishes as unpadded
// base64url, and the codes of the extension for anything else.
func TestListAnswers(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.st.put("usb", "jobs/b.gcode", "b", true)
	p.share(t, "local", "lib")
	p.share(t, "usb", "jobs")
	node := runStores(t, p)
	p.scan(t)
	awaitLatest(t, node, map[fractal.InstanceID]string{
		"local": quoted(p.root(t, "local")), "usb": quoted(p.root(t, "usb")),
	})

	lib, libID := listing(t, fileEntry("a.gcode", "a"))
	_, usbRoot := listing(t, folderEntry("jobs", mustListing(t, fileEntry("b.gcode", "b"))))

	type answer struct {
		result string
		code   fractal.Code
	}

	for _, row := range []struct {
		name   string
		params []json.RawMessage
		want   answer
	}{
		{"a listing", text(libID.Text()), answer{result: `"` + base64.RawURLEncoding.EncodeToString(lib) + `"`}},
		{"a file", text(fractalfiles.Sum([]byte("a")).Text()), answer{code: fractal.CodeObjectNotFound}},
		{"another storage", text(usbRoot.Text()), answer{code: fractal.CodeObjectNotFound}},
		{"not published", text(fractalfiles.Sum([]byte("x")).Text()), answer{code: fractal.CodeObjectNotFound}},
		{"malformed", text("sha256:nope"), answer{code: fractal.CodeInvalidParams}},
		{"missing", nil, answer{code: fractal.CodeInvalidParams}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			result, err := node.instance(t, "local").Owner.Invoke(t.Context(),
				fractalnode.Call{Method: "List", Params: row.params})

			got := answer{result: string(result)}

			if coded, ok := errors.AsType[*fractal.Error](err); ok {
				got.code = coded.Code
			} else if err != nil {
				t.Fatalf("Invoke returned %v, which carries no code", err)
			}

			if got != row.want {
				t.Errorf("List answered %+v, want %+v", got, row.want)
			}
		})
	}
}

func text(s string) []json.RawMessage {
	encoded, _ := json.Marshal(s)

	return []json.RawMessage{encoded}
}

func mustListing(t *testing.T, entries ...fractalfiles.Entry) []byte {
	t.Helper()

	data, _ := listing(t, entries...)

	return data
}

// TestListOnEveryStorage: a listing two storages publish is answered by both.
func TestListOnEveryStorage(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.st.put("usb", "lib/a.gcode", "a", true)
	p.share(t, "local", "lib")
	p.share(t, "usb", "lib")
	node := runStores(t, p)
	p.scan(t)

	shared, sharedID := listing(t, fileEntry("a.gcode", "a"))
	want := `"` + base64.RawURLEncoding.EncodeToString(shared) + `"`

	got := map[fractal.InstanceID]string{}

	for _, storage := range []fractal.InstanceID{"local", "usb"} {
		awaitLatest(t, node, map[fractal.InstanceID]string{
			"local": quoted(p.root(t, "local")), "usb": quoted(p.root(t, "usb")),
		})

		result, err := node.instance(t, storage).Owner.Invoke(t.Context(),
			fractalnode.Call{Method: "List", Params: text(sharedID.Text())})
		if err != nil {
			t.Fatalf("List on %s: %v", storage, err)
		}

		got[storage] = string(result)
	}

	if !reflect.DeepEqual(got, map[fractal.InstanceID]string{"local": want, "usb": want}) {
		t.Errorf("List answered %v, want the listing on both", got)
	}
}

// TestListRefusesOtherMethod: an instance answers List only.
func TestListRefusesOtherMethod(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.share(t, "local", "lib")
	node := runStores(t, p)
	p.scan(t)
	awaitLatest(t, node, map[fractal.InstanceID]string{"local": quoted(p.root(t, "local"))})

	_, err := node.instance(t, "local").Owner.Invoke(t.Context(), fractalnode.Call{Method: "Other"})

	coded, ok := errors.AsType[*fractal.Error](err)
	if !ok || coded.Code != fractal.CodeObjectNotFound {
		t.Errorf("Invoke returned %v, want ERR_OBJECT_NOT_FOUND", err)
	}
}
