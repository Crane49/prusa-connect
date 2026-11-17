package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// The interface the tests of the ancestor shadow register, and the second node
// below the node under test. A downward broadcast goes to both nodes below.
const (
	siteName     = fractal.InterfaceName("Site1")
	siteInstance = fractal.InstanceID("1")
	maintenance  = fractal.PropertyName("maintenanceMode")
	secondID     = fractal.NodeID("printer-2")
)

// The keys of Site1 on hq and on hq/brno in an ancestor shadow.
var (
	siteKey     = shadow.Key{Node: hqPath, Interface: siteName, Instance: siteInstance}
	brnoSiteKey = shadow.Key{Node: brnoPath, Interface: siteName, Instance: siteInstance}
)

// selectAll is an ancestor selection that keeps every interface above.
var selectAll = shadow.Selection{Patterns: []fractal.Pattern{"*"}}

// ancestorHarness is one running node with two nodes below it, an ancestor
// shadow and one instance of Site1 with the exposure the test chose.
type ancestorHarness struct {
	node      *node.Node
	binding   *recordBinding
	owner     *testOwner
	iface     *node.Interface
	ancestors *shadow.Store
}

// newAncestorHarness builds and starts a node at path, below which printer-1
// and printer-2 are admitted. Its ancestor shadow keeps what sel selects. It
// deletes the messages the node sent during Start.
func newAncestorHarness(
	t *testing.T,
	path fractal.NodePath,
	sel shadow.Selection,
	exposure schema.Exposure,
) *ancestorHarness {
	t.Helper()
	ancestors, err := shadow.New(shadow.Config{Selection: sel})
	if err != nil {
		t.Fatalf("build the ancestor shadow: %v", err)
	}
	h := &ancestorHarness{
		binding:   &recordBinding{},
		owner:     &testOwner{snapshot: fractal.Props{maintenance: json.RawMessage(`false`)}},
		ancestors: ancestors,
	}
	h.node, err = node.New(node.Config{
		Binding:    h.binding,
		Admissions: children{printerID, secondID},
		Ancestors:  ancestors,
		Clock:      &fixedClock{at: now},
		Path:       path,
	})
	if err != nil {
		t.Fatalf("build the node: %v", err)
	}
	h.iface, err = h.node.Register(node.Instance{
		Owner:    h.owner,
		Name:     siteName,
		Instance: siteInstance,
		Exposure: exposure,
	})
	if err != nil {
		t.Fatalf("register the interface: %v", err)
	}
	if startErr := h.node.Start(t.Context()); startErr != nil {
		t.Fatalf("start the node: %v", startErr)
	}
	t.Cleanup(func() { _ = h.node.Close() })
	synctest.Wait()
	h.binding.Reset()
	return h
}

// siteReport builds the report of Site1 that sender makes at the time at.
func siteReport(sender fractal.NodePath, at fractal.Timestamp, on bool) fractal.AREP {
	return fractal.AREP{
		Version:   fractal.Version,
		Sender:    sender,
		Interface: siteName,
		Instance:  siteInstance,
		Timestamp: at,
		Props:     fractal.Props{maintenance: json.RawMessage(strconv.FormatBool(on))},
	}
}

// toBoth is what a node sends when it passes payload of type k to both nodes
// below it.
func toBoth(k fractal.Kind, payload []byte) []sent {
	return []sent{
		{dispatch: node.Dispatch{Kind: k, Payload: payload, Child: printerID}},
		{dispatch: node.Dispatch{Kind: k, Payload: payload, Child: secondID}},
	}
}

// wantSent fails the test unless the node sent exactly want, in that order.
func wantSent(t *testing.T, got, want []sent) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the node sent\n  %v\nwant\n  %v", got, want)
	}
}

// String shows one recorded message in a failure message.
func (s sent) String() string {
	return fmt.Sprintf("%s to %q durable=%v %s", s.dispatch.Kind, s.dispatch.Child, s.durable, s.dispatch.Payload)
}

// statuses returns the status of every interface in store. Every node of the
// tests is in the subtree of hq.
func statuses(store *shadow.Store) map[shadow.Key]shadow.Status {
	out := map[shadow.Key]shadow.Status{}
	for key := range store.Subtree(hqPath) {
		status, _, _ := store.Status(key)
		out[key] = status
	}
	return out
}

// TestAnInstanceIsReportedInTheDirectionsItIsExposed checks the rule of
// fractal.md, Interface Instance: a node reports an instance by REP to the node
// above when it is exposed upward, by one AREP to every node below when it is
// exposed downward, and never in a direction it is not exposed in. A root has
// no node above and still sends the AREP.
func TestAnInstanceIsReportedInTheDirectionsItIsExposed(t *testing.T) {
	t.Parallel()
	up := func(payload []byte) []sent {
		return []sent{{dispatch: node.Dispatch{Kind: fractal.KindREP, Payload: payload}}}
	}
	down := func(payload []byte) []sent { return toBoth(fractal.KindAREP, payload) }
	cases := []struct {
		name     string
		path     fractal.NodePath
		exposure schema.Exposure
		want     func(payload []byte) []sent
	}{
		{"upward from a node with a parent", brnoPath, schema.ExposureUpward, up},
		{"downward from a node with a parent", brnoPath, schema.ExposureDownward, down},
		{"both from a node with a parent", brnoPath, schema.ExposureBoth, func(p []byte) []sent {
			return append(down(p), up(p)...)
		}},
		{"upward from a root", hqPath, schema.ExposureUpward, func([]byte) []sent { return nil }},
		{"downward from a root", hqPath, schema.ExposureDownward, down},
		{"both from a root", hqPath, schema.ExposureBoth, down},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newAncestorHarness(t, c.path, shadow.Selection{}, c.exposure)
				props := fractal.Props{maintenance: json.RawMessage(`true`)}
				if err := h.iface.Report(t.Context(), props, now); err != nil {
					t.Fatalf("Report returned %v", err)
				}
				synctest.Wait()

				wantSent(t, h.binding.Sent(), c.want(encode(t, siteReport(c.path, now, true))))
			})
		})
	}
}

// TestAFullReportRequestReportsOnlyUpward checks that the answer to an RBR
// reports only an instance exposed upward, and passes the RBR to both nodes
// below whatever the exposure (fractal.md, Full State Report Request Message).
func TestAFullReportRequestReportsOnlyUpward(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		exposure schema.Exposure
		reported bool
	}{
		{"upward", schema.ExposureUpward, true},
		{"downward", schema.ExposureDownward, false},
		{"both", schema.ExposureBoth, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newAncestorHarness(t, brnoPath, shadow.Selection{}, c.exposure)
				rbr := encode(t, fractal.RBR{Version: fractal.Version})
				h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindRBR, Payload: rbr})
				synctest.Wait()

				var want []sent
				if c.reported {
					payload := encode(t, siteReport(brnoPath, now, false))
					want = append(want, sent{dispatch: node.Dispatch{Kind: fractal.KindREP, Payload: payload}})
				}
				want = append(want, toBoth(fractal.KindRBR, rbr)...)
				wantSent(t, h.binding.Sent(), want)
			})
		})
	}
}

// TestAnAncestorReportIsAppliedAndPassedOn checks that a node writes an AREP
// from above to its ancestor shadow and passes the bytes that arrived to both
// nodes below (fractal.md, Ancestor Reported Properties Message).
func TestAnAncestorReportIsAppliedAndPassedOn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newAncestorHarness(t, brnoPath, selectAll, schema.ExposureUpward)
		payload := encode(t, siteReport(hqPath, now, true))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindAREP, Payload: payload})
		synctest.Wait()

		wantSent(t, h.binding.Sent(), toBoth(fractal.KindAREP, payload))
		want := shadow.Snapshot{
			Key:       siteKey,
			Props:     fractal.Props{maintenance: json.RawMessage(`true`)},
			UpdatedAt: now,
			Status:    shadow.StatusOK,
		}
		if snap, ok := h.node.Ancestors().Get(siteKey); !ok || !reflect.DeepEqual(snap, want) {
			t.Errorf("the ancestor shadow holds %+v, want %+v", snap, want)
		}
	})
}

// TestAnAncestorReportIsPassedOnUnlessItIsOld checks that a node passes on an
// AREP for an interface it does not keep, and drops one whose timestamp is
// lower than the one its ancestor shadow holds (fractal.md, Ancestor Reported
// Properties Message, and Shadow Selection).
func TestAnAncestorReportIsPassedOnUnlessItIsOld(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		sel     shadow.Selection
		reports []fractal.Timestamp
		passed  []fractal.Timestamp
		kept    int
	}{
		{
			"an interface the node does not select",
			shadow.Selection{},
			[]fractal.Timestamp{now},
			[]fractal.Timestamp{now},
			0,
		},
		{"an interface the node selects", selectAll, []fractal.Timestamp{now}, []fractal.Timestamp{now}, 1},
		{
			"an older report than the one the shadow holds",
			selectAll,
			[]fractal.Timestamp{now, now - 1},
			[]fractal.Timestamp{now},
			1,
		},
		{
			"an older report the node does not keep",
			shadow.Selection{},
			[]fractal.Timestamp{now, now - 1},
			[]fractal.Timestamp{now, now - 1},
			0,
		},
		{
			"a report with the same timestamp",
			selectAll,
			[]fractal.Timestamp{now, now},
			[]fractal.Timestamp{now, now},
			1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newAncestorHarness(t, brnoPath, c.sel, schema.ExposureUpward)
				for _, at := range c.reports {
					payload := encode(t, siteReport(hqPath, at, true))
					h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindAREP, Payload: payload})
				}
				synctest.Wait()

				var want []sent
				for _, at := range c.passed {
					want = append(want, toBoth(fractal.KindAREP, encode(t, siteReport(hqPath, at, true)))...)
				}
				wantSent(t, h.binding.Sent(), want)
				if got := h.ancestors.Len(); got != c.kept {
					t.Errorf("the ancestor shadow holds %d interfaces, want %d", got, c.kept)
				}
			})
		})
	}
}

// TestAnAncestorLossIsAppliedAndPassedOn checks that an ALOS from above marks
// every node above its sender stale, leaves the sender as it is, and is
// passed to both nodes below as the bytes that arrived (fractal.md, Ancestor
// Connection Loss Message).
func TestAnAncestorLossIsAppliedAndPassedOn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newAncestorHarness(t, printerPath, selectAll, schema.ExposureUpward)
		for _, sender := range []fractal.NodePath{hqPath, brnoPath} {
			payload := encode(t, siteReport(sender, now, true))
			h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindAREP, Payload: payload})
		}
		h.binding.Reset()

		loss := encode(t, fractal.ALOS{Version: fractal.Version, Sender: brnoPath})
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindALOS, Payload: loss})
		synctest.Wait()

		wantSent(t, h.binding.Sent(), toBoth(fractal.KindALOS, loss))
		want := map[shadow.Key]shadow.Status{siteKey: shadow.StatusStale, brnoSiteKey: shadow.StatusOK}
		if got := statuses(h.ancestors); !maps.Equal(got, want) {
			t.Errorf("the ancestor shadow holds %v, want %v", got, want)
		}
	})
}

// TestANewPathRebuildsTheAncestorShadow checks the ancestor shadow across a
// change of Node Path. New keeps the named interfaces of the nodes above the
// path. SetPath with the same path keeps every value, and a new path drops the
// former parent and creates the new one as EMPTY. A late report from the former parent is then dropped (fractal.md,
// Synchronization of the Ancestor Shadow - Happy path, and After the Join
// Exchange).
func TestANewPathRebuildsTheAncestorShadow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		prahaSiteKey := shadow.Key{Node: "hq/praha", Interface: siteName, Instance: siteInstance}
		sel := shadow.Selection{Interfaces: []shadow.Key{siteKey, brnoSiteKey, prahaSiteKey}}
		h := newAncestorHarness(t, printerPath, sel, schema.ExposureUpward)
		fromBrno := node.Inbound{Kind: fractal.KindAREP, Payload: encode(t, siteReport(brnoPath, now, true))}
		steps := []struct {
			name string
			act  func()
			want map[shadow.Key]shadow.Status
		}{
			{"New keeps the named interfaces above the path", func() {}, map[shadow.Key]shadow.Status{
				siteKey: shadow.StatusEmpty, brnoSiteKey: shadow.StatusEmpty,
			}},
			{
				"a report from the parent is applied",
				func() { h.node.Deliver(t.Context(), fromBrno) },
				map[shadow.Key]shadow.Status{
					siteKey: shadow.StatusEmpty, brnoSiteKey: shadow.StatusOK,
				},
			},
			{"the same path again keeps the ancestor shadow", func() {
				if err := h.node.SetPath(t.Context(), printerPath); err != nil {
					t.Fatalf("set the same path: %v", err)
				}
			}, map[shadow.Key]shadow.Status{siteKey: shadow.StatusEmpty, brnoSiteKey: shadow.StatusOK}},
			{"a new path drops the former parent", func() {
				if err := h.node.SetPath(t.Context(), "hq/praha/printer-1"); err != nil {
					t.Fatalf("set the new path: %v", err)
				}
			}, map[shadow.Key]shadow.Status{siteKey: shadow.StatusEmpty, prahaSiteKey: shadow.StatusEmpty}},
			{"a late report from the former parent is dropped", func() { h.node.Deliver(t.Context(), fromBrno) },
				map[shadow.Key]shadow.Status{siteKey: shadow.StatusEmpty, prahaSiteKey: shadow.StatusEmpty}},
		}
		for _, step := range steps {
			step.act()
			synctest.Wait()
			if got := statuses(h.ancestors); !maps.Equal(got, step.want) {
				t.Fatalf("%s: the ancestor shadow holds %v, want %v", step.name, got, step.want)
			}
		}
	})
}

// TestTheAncestorShadowIsKeptApart checks that New refuses one store for both
// shadows, that Ancestors returns the store the node was given, and that a
// node given none gets a store of its own (fractal.md, Shadow).
func TestTheAncestorShadowIsKeptApart(t *testing.T) {
	t.Parallel()
	store, err := shadowStore()
	if err != nil {
		t.Fatalf("build the shadow: %v", err)
	}
	_, sharedErr := node.New(node.Config{Binding: &recordBinding{}, Shadow: store, Ancestors: store})
	if !errors.Is(sharedErr, node.ErrSharedShadow) {
		t.Errorf("New with one store for both shadows returned %v, want %v", sharedErr, node.ErrSharedShadow)
	}
	ancestors, err := shadow.New(shadow.Config{})
	if err != nil {
		t.Fatalf("build the ancestor shadow: %v", err)
	}
	given, err := node.New(node.Config{Binding: &recordBinding{}, Shadow: store, Ancestors: ancestors})
	if err != nil || given.Ancestors() != ancestors {
		t.Errorf("New returned %v and an ancestor shadow other than the one it was given", err)
	}
	built, err := node.New(node.Config{Binding: &recordBinding{}, Shadow: store})
	if err != nil || built.Ancestors() == nil || built.Ancestors() == built.Shadow() {
		t.Errorf("New returned %v, and a node with no ancestor shadow of its own", err)
	}
}

// TestASenderFromAboveMustBeAnAncestor checks the rule of fractal.md, Sender
// Validation: an RPC, an AREP or an ALOS from the node above whose sender is
// not a node above this one is dropped, not acted on and not passed on.
func TestASenderFromAboveMustBeAnAncestor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   func(t *testing.T) node.Inbound
	}{
		{"a method invocation from a node beside this one", func(t *testing.T) node.Inbound {
			m := invocation(printerPath)
			m.Sender = "hq/praha"
			return node.Inbound{Kind: fractal.KindRPC, Payload: encode(t, m)}
		}},
		{"a method invocation from this node itself", func(t *testing.T) node.Inbound {
			m := invocation(printerPath)
			m.Sender = brnoPath
			return node.Inbound{Kind: fractal.KindRPC, Payload: encode(t, m)}
		}},
		{"an ancestor report from this node itself", func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindAREP, Payload: encode(t, siteReport(brnoPath, now, true))}
		}},
		{"an ancestor report from a node below", func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindAREP, Payload: encode(t, siteReport(printerPath, now, true))}
		}},
		{"an ancestor loss from a node beside this one", func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindALOS, Payload: encode(t, fractal.ALOS{
				Version: fractal.Version, Sender: "hq/praha",
			})}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				n, binding, metrics := newCountingNode(t, nil)
				n.Deliver(t.Context(), c.in(t))
				synctest.Wait()

				want := map[node.DropReason]int{}
				want[node.DropSenderInvalid] = 1
				if got := metrics.all(); !maps.Equal(got, want) {
					t.Errorf("the node counted %v, want %v", got, want)
				}
				wantSent(t, binding.Sent(), nil)
			})
		})
	}
}

// arbrUp is the ARBR a node sends to the node above.
func arbrUp(t *testing.T) sent {
	t.Helper()
	return sent{
		dispatch: node.Dispatch{Kind: fractal.KindARBR, Payload: encode(t, fractal.ARBR{Version: fractal.Version})},
	}
}

// askFrom is an ARBR that arrived on the link of the node below with this id.
func askFrom(t *testing.T, id fractal.NodeID) node.Inbound {
	t.Helper()
	return node.Inbound{Kind: fractal.KindARBR, Payload: encode(t, fractal.ARBR{Version: fractal.Version}), From: id}
}

// growingChildren is an Admissions that a test admits more nodes to while the
// node runs, as a link that comes up does.
type growingChildren struct {
	ids []fractal.NodeID
	mu  sync.Mutex
}

// Admit adds id to the nodes directly below.
func (g *growingChildren) Admit(id fractal.NodeID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ids = append(g.ids, id)
}

// Admitted reports whether id was admitted.
func (g *growingChildren) Admitted(id fractal.NodeID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Contains(g.ids, id)
}

// Children iterates over a copy of the admitted ids.
func (g *growingChildren) Children() iter.Seq[fractal.NodeID] {
	g.mu.Lock()
	ids := slices.Clone(g.ids)
	g.mu.Unlock()
	return slices.Values(ids)
}

// stuckOwner is an Owner whose Snapshot returns only when its context ends.
type stuckOwner struct {
	*testOwner
}

// Snapshot waits for ctx to end and returns its error.
func (stuckOwner) Snapshot(ctx context.Context) (fractal.Props, fractal.Timestamp, error) {
	<-ctx.Done()
	return nil, 0, ctx.Err()
}

// TestAParentLinkComingUpAsksForTheAncestorState checks that a node sends no
// ARBR before the link to the node above is up, and one ARBR once it is, even
// with nothing above selected. A root has no node above and sends none. A node
// that gets its path after the link came up sends it then, and no second one
// once the limit runs out (fractal.md, Synchronization of the Ancestor Shadow - Happy path, and Synchronization of
// the Shadows - Reaction to Connection Loss and Reconnection).
func TestAParentLinkComingUpAsksForTheAncestorState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		path    fractal.NodePath
		setPath fractal.NodePath
		want    int
	}{
		{"a node with a parent", brnoPath, "", 1},
		{"a root", hqPath, "", 0},
		{"a node that gets its path after the link came up", "", brnoPath, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newAncestorHarness(t, c.path, shadow.Selection{}, schema.ExposureUpward)
				time.Sleep(time.Minute)
				synctest.Wait()
				wantSent(t, h.binding.OfKind(fractal.KindARBR), nil)

				h.node.ParentConnected(t.Context())
				if c.setPath != "" {
					if err := h.node.SetPath(t.Context(), c.setPath); err != nil {
						t.Fatalf("set the path: %v", err)
					}
				}
				synctest.Wait()
				time.Sleep(node.DefaultARBRMinInterval)
				synctest.Wait()

				wantSent(t, h.binding.OfKind(fractal.KindARBR), slices.Repeat([]sent{arbrUp(t)}, c.want))
			})
		})
	}
}

// TestTheBoundDelaysAndNeverSwallowsAnARBR checks the limit on the ARBR
// messages a node sends of its own. A link that goes down and up again within
// ARBRMinInterval gets one ARBR when the limit allows, and not one per flap.
// An ARBR owed while the link is down waits for the next ParentConnected
// (fractal.md, Ancestor Full State Report Request Message).
func TestTheBoundDelaysAndNeverSwallowsAnARBR(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newAncestorHarness(t, brnoPath, shadow.Selection{}, schema.ExposureUpward)
		arbr := arbrUp(t)
		steps := []struct {
			name string
			act  func()
			want int
		}{
			{"the link comes up", func() { h.node.ParentConnected(t.Context()) }, 1},
			{"the link flaps three times within the limit", func() {
				for range 3 {
					h.node.ParentLost(t.Context())
					h.node.ParentConnected(t.Context())
				}
			}, 1},
			{"the limit runs out", func() { time.Sleep(node.DefaultARBRMinInterval) }, 2},
			{"the link flaps and stays down past the limit", func() {
				h.node.ParentLost(t.Context())
				h.node.ParentConnected(t.Context())
				h.node.ParentLost(t.Context())
				time.Sleep(10 * node.DefaultARBRMinInterval)
			}, 2},
			{"the link comes up again", func() { h.node.ParentConnected(t.Context()) }, 3},
			{"nothing more is owed", func() { time.Sleep(10 * node.DefaultARBRMinInterval) }, 3},
		}
		for _, step := range steps {
			step.act()
			synctest.Wait()
			if got := len(h.binding.OfKind(fractal.KindARBR)); got != step.want {
				t.Fatalf("%s: the node sent %d ARBR messages in all, want %d", step.name, got, step.want)
			}
		}
		wantSent(t, h.binding.OfKind(fractal.KindARBR), []sent{arbr, arbr, arbr})
	})
}

// TestANewPathResetsTheBound checks that a node sends an ARBR on a new Node
// Path while its link is up, without waiting for ARBRMinInterval (fractal.md,
// Synchronization of the Ancestor Shadow - Happy path).
func TestANewPathResetsTheBound(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newAncestorHarness(t, brnoPath, shadow.Selection{}, schema.ExposureUpward)
		h.node.ParentConnected(t.Context())
		synctest.Wait()
		if err := h.node.SetPath(t.Context(), "hq/praha"); err != nil {
			t.Fatalf("set the new path: %v", err)
		}
		synctest.Wait()

		wantSent(t, h.binding.OfKind(fractal.KindARBR), []sent{arbrUp(t), arbrUp(t)})
	})
}

// TestAnAncestorReportRequestIsAnsweredAfterTheWindow checks the answer to
// ARBR messages from below. The node waits for ARBRWindow, then sends a full
// AREP of its downward instance to both nodes below and one ARBR to the node
// above, the reports first, however many ARBR messages the window collected.
// The full AREP has the time the owner read the values, or the current time
// when the owner returns none (fractal.md, Ancestor Full State Report Request
// Message and Ancestor Reported Properties Message).
func TestAnAncestorReportRequestIsAnsweredAfterTheWindow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		requests   int
		snapshotAt fractal.Timestamp
		stamped    fractal.Timestamp
	}{
		{"one request", 1, 0, now},
		{"three requests within the window", 3, 0, now},
		{"an owner that knows when it read the values", 1, now - 5000, now - 5000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newAncestorHarness(t, brnoPath, shadow.Selection{}, schema.ExposureDownward)
				h.owner.snapshotAt = c.snapshotAt
				for i := range c.requests {
					h.node.Deliver(t.Context(), askFrom(t, []fractal.NodeID{printerID, secondID}[i%2]))
				}
				synctest.Wait()
				wantSent(t, h.binding.Sent(), nil)

				time.Sleep(node.DefaultARBRWindow)
				synctest.Wait()

				want := append(toBoth(fractal.KindAREP, encode(t, siteReport(brnoPath, c.stamped, false))), arbrUp(t))
				wantSent(t, h.binding.Sent(), want)
			})
		})
	}
}

// TestAnAncestorReportRequestIsPassedOnVerbatim checks that the ARBR a node
// sends up for a window is the bytes that arrived from below, not one it
// built anew (fractal-nats.md, ARBR: "forwarded verbatim").
func TestAnAncestorReportRequestIsPassedOnVerbatim(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newAncestorHarness(t, brnoPath, shadow.Selection{}, schema.ExposureDownward)
		arrived := []byte(`{"version":1,"note":"from printer-1"}`)
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindARBR, Payload: arrived, From: printerID})
		time.Sleep(node.DefaultARBRWindow)
		synctest.Wait()

		up := h.binding.OfKind(fractal.KindARBR)
		want := []sent{{dispatch: node.Dispatch{Kind: fractal.KindARBR, Payload: arrived}}}
		wantSent(t, up, want)
	})
}

// TestTheWindowCoversALinkThatCameUpInIt checks that the answer to a window
// also goes to a node below whose link came up while the window was open
// (fractal.md, Ancestor Full State Report Request Message).
func TestTheWindowCoversALinkThatCameUpInIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		admissions := &growingChildren{ids: []fractal.NodeID{printerID}}
		binding := &recordBinding{}
		n, err := node.New(
			node.Config{Binding: binding, Admissions: admissions, Clock: &fixedClock{at: now}, Path: brnoPath},
		)
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		if _, regErr := n.Register(node.Instance{
			Owner:    &testOwner{snapshot: fractal.Props{maintenance: json.RawMessage(`false`)}},
			Name:     siteName,
			Instance: siteInstance,
			Exposure: schema.ExposureDownward,
		}); regErr != nil {
			t.Fatalf("register the interface: %v", regErr)
		}
		if startErr := n.Start(t.Context()); startErr != nil {
			t.Fatalf("start the node: %v", startErr)
		}
		t.Cleanup(func() { _ = n.Close() })

		n.Deliver(t.Context(), askFrom(t, printerID))
		synctest.Wait()
		admissions.Admit(secondID)
		n.ChildConnected(t.Context(), secondID)
		time.Sleep(node.DefaultARBRWindow)
		synctest.Wait()

		wantSent(
			t,
			binding.OfKind(fractal.KindAREP),
			toBoth(fractal.KindAREP, encode(t, siteReport(brnoPath, now, false))),
		)
	})
}

// TestASlowOwnerDoesNotStopTheAnswer checks that an owner whose Snapshot does
// not return delays the answer to a window only until its timeout. The node
// still reports its other instance, still sends the ARBR up, and answers the
// next window too.
func TestASlowOwnerDoesNotStopTheAnswer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newAncestorHarness(t, brnoPath, shadow.Selection{}, schema.ExposureDownward)
		if _, err := h.node.Register(node.Instance{
			Owner:    stuckOwner{&testOwner{}},
			Name:     siteName,
			Instance: "2",
			Exposure: schema.ExposureDownward,
		}); err != nil {
			t.Fatalf("register the stuck interface: %v", err)
		}
		want := append(toBoth(fractal.KindAREP, encode(t, siteReport(brnoPath, now, false))), arbrUp(t))
		for range 2 {
			h.binding.Reset()
			h.node.Deliver(t.Context(), askFrom(t, printerID))
			time.Sleep(time.Minute)
			synctest.Wait()
			wantSent(t, h.binding.Sent(), want)
		}
	})
}

// TestAnOwnerThatIgnoresTheContextDoesNotStopTheARBR checks that an owner whose
// Snapshot never returns, even after its context ends, holds back only the
// answer to a window. The node still sends its own ARBR when the link to the
// node above comes up (fractal.md, Synchronization of the Shadows - Reaction to
// Connection Loss and Reconnection).
func TestAnOwnerThatIgnoresTheContextDoesNotStopTheARBR(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newAncestorHarness(t, brnoPath, shadow.Selection{}, schema.ExposureUpward)
		release := make(chan struct{})
		if _, err := h.node.Register(node.Instance{
			Owner:    deafOwner{testOwner: &testOwner{}, release: release},
			Name:     siteName,
			Instance: "2",
			Exposure: schema.ExposureDownward,
		}); err != nil {
			t.Fatalf("register the deaf interface: %v", err)
		}
		t.Cleanup(func() { close(release) })
		h.node.Deliver(t.Context(), askFrom(t, printerID))
		time.Sleep(time.Hour)
		synctest.Wait()

		h.node.ParentConnected(t.Context())
		time.Sleep(node.DefaultARBRMinInterval)
		synctest.Wait()

		wantSent(t, h.binding.OfKind(fractal.KindARBR), []sent{arbrUp(t)})
	})
}

// TestALostParentMarksTheAncestorsAndTellsTheChildren checks ParentLost. The
// node marks its whole ancestor shadow stale, sends an ALOS with its own Node
// Path to both nodes below, and sends no ARBR while the link stays down
// (fractal.md, Ancestor Connection Loss Message).
func TestALostParentMarksTheAncestorsAndTellsTheChildren(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newAncestorHarness(t, printerPath, selectAll, schema.ExposureUpward)
		for _, sender := range []fractal.NodePath{hqPath, brnoPath} {
			payload := encode(t, siteReport(sender, now, true))
			h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindAREP, Payload: payload})
		}
		h.node.ParentConnected(t.Context())
		synctest.Wait()
		h.binding.Reset()

		h.node.ParentLost(t.Context())
		time.Sleep(time.Minute)
		synctest.Wait()

		wantSent(t, h.binding.Sent(), toBoth(fractal.KindALOS, encode(t, fractal.ALOS{
			Version: fractal.Version, Sender: printerPath,
		})))
		want := map[shadow.Key]shadow.Status{siteKey: shadow.StatusStale, brnoSiteKey: shadow.StatusStale}
		if got := statuses(h.ancestors); !maps.Equal(got, want) {
			t.Errorf("the ancestor shadow holds %v, want %v", got, want)
		}
	})
}

// deafOwner is an Owner whose Snapshot ignores its context and returns only
// once release is closed.
type deafOwner struct {
	*testOwner

	release chan struct{}
}

// Snapshot waits for release to close.
func (o deafOwner) Snapshot(context.Context) (fractal.Props, fractal.Timestamp, error) {
	<-o.release
	return nil, 0, nil
}
