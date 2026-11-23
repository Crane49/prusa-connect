package resourcehost

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// recordingFleet is what sits behind the vouching: it notes who each call was
// resolved for and answers nothing.
type recordingFleet struct{ saw []string }

func (r *recordingFleet) List(_ context.Context, p deviceapi.ListParams) (deviceapi.Page, error) {
	r.saw = append(r.saw, p.Principal)

	return deviceapi.Page{}, nil
}

func (r *recordingFleet) Get(_ context.Context, p deviceapi.GetParams) (deviceapi.Device, error) {
	r.saw = append(r.saw, p.Principal)

	return deviceapi.Device{}, nil
}

func (r *recordingFleet) Command(_ context.Context, p deviceapi.CommandParams) (deviceapi.Result, error) {
	r.saw = append(r.saw, p.Principal)

	return deviceapi.Result{}, nil
}

func (r *recordingFleet) Approve(
	_ context.Context, p deviceapi.ApproveParams,
) (deviceapi.Registration, error) {
	r.saw = append(r.saw, p.Principal)

	return deviceapi.Registration{}, nil
}

func (r *recordingFleet) Jobs(_ context.Context, p deviceapi.JobsParams) (deviceapi.Jobs, error) {
	r.saw = append(r.saw, p.Principal)

	return deviceapi.Jobs{}, nil
}

func (r *recordingFleet) Files(_ context.Context, p deviceapi.FilesParams) (deviceapi.Listing, error) {
	r.saw = append(r.saw, p.Principal)

	return deviceapi.Listing{}, nil
}

func (r *recordingFleet) JobHistory(
	_ context.Context, p deviceapi.JobHistoryParams,
) (deviceapi.JobPage, error) {
	r.saw = append(r.saw, p.Principal)

	return deviceapi.JobPage{}, nil
}

func (r *recordingFleet) Print(_ context.Context, p deviceapi.PrintParams) (deviceapi.Started, error) {
	r.saw = append(r.saw, p.Principal)

	return deviceapi.Started{}, nil
}

// sinkRef is one module instance's ref.
func sinkRef(t *testing.T, name string) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindSink, name)
	require.NoError(t, err)

	return instance
}

// fleetCalls is one call per method, each naming the principal it is given.
func fleetCalls(principal string) map[string]func(context.Context, deviceapi.Fleet) error {
	return map[string]func(context.Context, deviceapi.Fleet) error{
		"list": func(ctx context.Context, f deviceapi.Fleet) error {
			_, err := f.List(ctx, deviceapi.ListParams{Principal: principal})

			return err
		},
		"get": func(ctx context.Context, f deviceapi.Fleet) error {
			_, err := f.Get(ctx, deviceapi.GetParams{Principal: principal, ID: "p1"})

			return err
		},
		"command": func(ctx context.Context, f deviceapi.Fleet) error {
			_, err := f.Command(ctx, deviceapi.CommandParams{Principal: principal, ID: "p1", Verb: "v"})

			return err
		},
		"approve": func(ctx context.Context, f deviceapi.Fleet) error {
			_, err := f.Approve(ctx, deviceapi.ApproveParams{Principal: principal, Code: "K7PQ2XR4MN"})

			return err
		},
		"jobs": func(ctx context.Context, f deviceapi.Fleet) error {
			_, err := f.Jobs(ctx, deviceapi.JobsParams{Principal: principal, ID: "p1"})

			return err
		},
		"files": func(ctx context.Context, f deviceapi.Fleet) error {
			_, err := f.Files(ctx, deviceapi.FilesParams{Principal: principal, ID: "p1", Path: "usb"})

			return err
		},
	}
}

// A principal the host handed this instance in a request still open is
// vouched for on every method, and the call reaches the fleet as that person.
func TestAPrincipalOnAnOpenRequestIsVouchedForOnEveryMethod(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "planner")

	for name, call := range fleetCalls("user:alice") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &Host{}
			inner := &recordingFleet{}
			fleet := h.vouchFleet(instance, inner)

			leave := h.flights.board(instance, "user:alice", nil)
			defer leave()

			require.NoError(t, call(t.Context(), fleet))
			require.Equal(t, []string{"user:alice"}, inner.saw)
		})
	}
}

// A principal the host never handed this instance is refused on every method
// before the fleet is reached, and so is one it handed another instance, and
// so is one whose request has ended.
func TestAPrincipalTheHostDidNotHandThisInstanceIsRefusedOnEveryMethod(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "planner")
	other := sinkRef(t, "audit")

	for name, call := range fleetCalls("user:alice") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &Host{}
			inner := &recordingFleet{}
			fleet := h.vouchFleet(instance, inner)

			// Never handed.
			err := call(t.Context(), fleet)
			require.ErrorIs(t, err, deviceapi.ErrDenied)

			// Handed to another instance.
			leave := h.flights.board(other, "user:alice", nil)
			require.ErrorIs(t, call(t.Context(), fleet), deviceapi.ErrDenied)
			leave()

			// Handed to this one, and the request has ended.
			h.flights.board(instance, "user:alice", nil)()
			require.ErrorIs(t, call(t.Context(), fleet), deviceapi.ErrDenied)

			require.Empty(t, inner.saw, "a refused call reached the fleet")
		})
	}
}

// A call naming nobody is the module's own work and passes the vouching as it
// is, on every method. What it runs as is the fleet's decision.
func TestACallNamingNobodyIsTheModulesOwn(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "planner")

	for name, call := range fleetCalls("") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &Host{}
			inner := &recordingFleet{}

			require.NoError(t, call(t.Context(), h.vouchFleet(instance, inner)))
			require.Equal(t, []string{""}, inner.saw)
		})
	}
}

// heldFeed is a feed that writes one window and then holds the subscription
// open until its context ends, which is what a hub subscription does.
type heldFeed struct{ saw []string }

func (f *heldFeed) Subscribe(
	ctx context.Context, params busapi.SubscribeParams, write func(busapi.Window) error,
) error {
	f.saw = append(f.saw, params.Principal)

	if err := write(busapi.Window{Cursor: "origin:1"}); err != nil {
		return err
	}

	<-ctx.Done()

	return nil
}

// A subscription a person's request opened is vouched for while the request is
// open and ends when the request does: the person is gone, so there is nobody
// left to observe as.
func TestASubscriptionForAPersonEndsWithTheirRequest(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "planner")
	h := &Host{}
	inner := &heldFeed{}
	feed := h.vouchFeed(instance, inner)

	leave := h.flights.board(instance, "user:alice", nil)

	windows := make(chan busapi.Window, 1)
	ended := make(chan error, 1)

	go func() {
		ended <- feed.Subscribe(t.Context(), busapi.SubscribeParams{Principal: "user:alice"},
			func(w busapi.Window) error {
				windows <- w

				return nil
			})
	}()

	require.Equal(t, "origin:1", (<-windows).Cursor)

	// The request ends, and the subscription with it.
	leave()

	require.NoError(t, <-ended)
	require.Equal(t, []string{"user:alice"}, inner.saw)
}

// A subscription naming a principal the host did not hand this instance is
// refused before the hub is reached.
func TestASubscriptionAsAPrincipalTheHostDidNotHandIsRefused(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "planner")
	h := &Host{}
	inner := &heldFeed{}

	err := h.vouchFeed(instance, inner).Subscribe(t.Context(),
		busapi.SubscribeParams{Principal: "user:alice"}, func(busapi.Window) error { return nil })

	require.ErrorIs(t, err, busapi.ErrDenied)
	require.Empty(t, inner.saw, "a refused subscription reached the feed")
}

// A host that serves no fleet and no feed stays that way through the vouching,
// so the method set is empty rather than a wrapper around nothing.
func TestVouchingLeavesAHostThatServesNothingAlone(t *testing.T) {
	t.Parallel()

	h := &Host{}
	instance := sinkRef(t, "planner")

	require.Nil(t, h.vouchFleet(instance, nil))
	require.Nil(t, h.vouchFeed(instance, nil))
	require.Empty(t, deviceapi.Handlers(h.vouchFleet(instance, nil)))
	require.Empty(t, busapi.SubscribeHandler(h.vouchFeed(instance, nil)))
}

// linkRef is one link instance's ref.
func linkRef(t *testing.T, name string) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindLink, name)
	require.NoError(t, err)

	return instance
}

// reportingNodes is a host whose link instance has reported that it answers for
// these servers.
func reportingNodes(t *testing.T, instance ref.Ref, ids ...string) *Host {
	t.Helper()

	h := &Host{}
	require.Empty(t, h.flights.nodes.report(instance, ids, h.supervised()))

	return h
}

// A link names the server above it on every method and the call reaches the
// fleet as that node, although no request of this host ever carried it. What
// the node may then do is the authorization graph's answer and not this one.
func TestALinkActsAsAServerThatJoinedThisOne(t *testing.T) {
	t.Parallel()

	instance := linkRef(t, "fractal")

	for name, call := range fleetCalls("node:hq") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := reportingNodes(t, instance, "hq")
			inner := &recordingFleet{}

			require.NoError(t, call(t.Context(), h.vouchFleet(instance, inner)))
			require.Equal(t, []string{"node:hq"}, inner.saw)
		})
	}
}

// A link that has reported nothing answers for nobody. Its set is built from
// its own readings, so there is nothing to accept before the first one.
func TestALinkThatHasReportedNothingActsAsNoNode(t *testing.T) {
	t.Parallel()

	instance := linkRef(t, "fractal")

	for name, call := range fleetCalls("node:hq") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &Host{}
			inner := &recordingFleet{}

			require.ErrorIs(t, call(t.Context(), h.vouchFleet(instance, inner)), deviceapi.ErrDenied)
			require.Empty(t, inner.saw, "a refused call reached the fleet")
		})
	}
}

// A link is held to the servers it reported. An id it never named is refused,
// so a module cannot reach a grant by naming the id it was written for.
func TestALinkMayNotActAsANodeItDidNotReport(t *testing.T) {
	t.Parallel()

	instance := linkRef(t, "fractal")

	for name, call := range fleetCalls("node:hq") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := reportingNodes(t, instance, "depot")
			inner := &recordingFleet{}

			require.ErrorIs(t, call(t.Context(), h.vouchFleet(instance, inner)), deviceapi.ErrDenied)
			require.Empty(t, inner.saw, "a refused call reached the fleet")
		})
	}
}

// Two link instances never answer for one server. The second reading is refused
// whole, and the link that reported the id first keeps it.
func TestASecondLinkCannotTakeAServerTheFirstAnswersFor(t *testing.T) {
	t.Parallel()

	first := linkRef(t, "fractal")
	second := linkRef(t, "other")

	h := reportingNodes(t, first, "hq")

	// The records let an id go when its link is no longer supervised, so the
	// holder has to be running for the refusal to be the one under test.
	running := map[ref.Ref]struct{}{first: {}}

	require.Equal(t, []string{"hq"}, h.flights.nodes.report(second, []string{"hq"}, running))

	inner := &recordingFleet{}
	_, err := h.vouchFleet(second, inner).List(t.Context(), deviceapi.ListParams{Principal: "node:hq"})
	require.ErrorIs(t, err, deviceapi.ErrDenied)
	require.Empty(t, inner.saw, "a refused call reached the fleet")

	held := &recordingFleet{}
	_, err = h.vouchFleet(first, held).List(t.Context(), deviceapi.ListParams{Principal: "node:hq"})
	require.NoError(t, err)
	require.Equal(t, []string{"node:hq"}, held.saw)
}

// An id is let go when the link that reported it is no longer supervised, so an
// uninstalled module does not keep a server's name out of use.
func TestAServerIsFreeOnceTheLinkThatReportedItIsGone(t *testing.T) {
	t.Parallel()

	first := linkRef(t, "fractal")
	second := linkRef(t, "other")

	h := reportingNodes(t, first, "hq")

	require.Empty(t, h.flights.nodes.report(second, []string{"hq"}, h.supervised()))
	require.True(t, h.flights.nodes.holds(second, "hq"))
	require.False(t, h.flights.nodes.holds(first, "hq"))
}

// A reading replaces the one before it, so a server the link no longer reports
// is no longer one it may act as.
func TestALinkLetsGoOfAServerItStopsReporting(t *testing.T) {
	t.Parallel()

	instance := linkRef(t, "fractal")
	h := reportingNodes(t, instance, "hq", "depot")

	require.Empty(t, h.flights.nodes.report(instance, []string{"depot"}, h.supervised()))
	require.False(t, h.flights.nodes.holds(instance, "hq"))
	require.True(t, h.flights.nodes.holds(instance, "depot"))
}

// A module that is starting has reported nothing, so the set is rebuilt from
// the first reading of the run that is starting.
func TestALinkAnswersForNoServerUntilItReportsAgainAfterARestart(t *testing.T) {
	t.Parallel()

	instance := linkRef(t, "fractal")
	h := reportingNodes(t, instance, "hq")

	h.flights.nodes.forget(instance)
	require.False(t, h.flights.nodes.holds(instance, "hq"))

	require.Empty(t, h.flights.nodes.report(instance, []string{"hq"}, h.supervised()))
	require.True(t, h.flights.nodes.holds(instance, "hq"))
}

// The servers one reading names are the server above this one, which is the
// segment before the last of the node path, and every server below it. This
// site's own id is not one of them.
func TestTheServersOneReadingNames(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		reading linkapi.Status
		want    []string
	}{
		"a site at the top of its own tree": {
			reading: linkapi.Status{NodeID: "hq", NodePath: "hq"},
			want:    []string{},
		},
		"a site that joined one above it": {
			reading: linkapi.Status{NodeID: "floor", NodePath: "hq/floor"},
			want:    []string{"hq"},
		},
		"a site deeper in the tree": {
			reading: linkapi.Status{NodeID: "cell", NodePath: "hq/floor/cell"},
			want:    []string{"floor"},
		},
		"a site with servers below it": {
			reading: linkapi.Status{
				NodeID:   "hq",
				NodePath: "hq",
				Nodes:    []linkapi.Node{{ID: "floor"}, {ID: "depot"}},
			},
			want: []string{"floor", "depot"},
		},
		"a site with a server above and servers below": {
			reading: linkapi.Status{
				NodeID:   "floor",
				NodePath: "hq/floor",
				Nodes:    []linkapi.Node{{ID: "cell"}},
			},
			want: []string{"hq", "cell"},
		},
		"a reading with nothing in it": {
			reading: linkapi.Status{},
			want:    []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, nodesInReading(tc.reading))
		})
	}
}

// Every other kind of instance is refused the same principal. A sink reads the
// fleet for its own reasons and answers for nobody but itself.
func TestAnInstanceThatIsNotALinkMayNotActAsANode(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "planner")

	for name, call := range fleetCalls("node:hq") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &Host{}
			inner := &recordingFleet{}

			require.ErrorIs(t, call(t.Context(), h.vouchFleet(instance, inner)), deviceapi.ErrDenied)
			require.Empty(t, inner.saw, "a refused call reached the fleet")
		})
	}
}

// A link may name a node and nothing else. A person it is not serving is
// refused, so the one principal this allowance opens is the node.
func TestALinkMayNotActAsAPersonItIsNotServing(t *testing.T) {
	t.Parallel()

	instance := linkRef(t, "fractal")

	for _, principal := range []string{"user:alice", "service_account:sa-ui", "sink:audit", "nodes:hq"} {
		t.Run(principal, func(t *testing.T) {
			t.Parallel()

			h := &Host{}
			inner := &recordingFleet{}

			_, err := h.vouchFleet(instance, inner).List(t.Context(),
				deviceapi.ListParams{Principal: principal})

			require.ErrorIs(t, err, deviceapi.ErrDenied)
			require.Empty(t, inner.saw, "a refused call reached the fleet")
		})
	}
}

// A link watching the fleet for the server above it has no request to end, so
// the subscription runs until the caller's own context ends.
func TestASubscriptionForAServerThatJoinedThisOneOutlivesEveryRequest(t *testing.T) {
	t.Parallel()

	instance := linkRef(t, "fractal")
	h := reportingNodes(t, instance, "hq")
	inner := &heldFeed{}

	ctx, cancel := context.WithCancel(t.Context())

	windows := make(chan busapi.Window, 1)
	ended := make(chan error, 1)

	go func() {
		ended <- h.vouchFeed(instance, inner).Subscribe(ctx,
			busapi.SubscribeParams{Principal: "node:hq"},
			func(w busapi.Window) error {
				windows <- w

				return nil
			})
	}()

	require.Equal(t, "origin:1", (<-windows).Cursor)

	cancel()

	require.NoError(t, <-ended)
	require.Equal(t, []string{"node:hq"}, inner.saw)
}

// A sink subscribing as a node is refused before the hub is reached, which is
// the feed half of the rule the fleet half holds.
func TestASubscriptionAsANodeFromSomethingThatIsNotALinkIsRefused(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "planner")
	h := &Host{}
	inner := &heldFeed{}

	err := h.vouchFeed(instance, inner).Subscribe(t.Context(),
		busapi.SubscribeParams{Principal: "node:hq"}, func(busapi.Window) error { return nil })

	require.ErrorIs(t, err, busapi.ErrDenied)
	require.Empty(t, inner.saw, "a refused subscription reached the feed")
}

// recordingFiles notes who each storage call was made for.
type recordingFiles struct{ saw []string }

func (r *recordingFiles) Stat(_ context.Context, p storageapi.StatParams) (storageapi.FileInfo, error) {
	r.saw = append(r.saw, p.Principal)

	return storageapi.FileInfo{}, nil
}

func (r *recordingFiles) Read(_ context.Context, p storageapi.ReadParams) (storageapi.Chunk, error) {
	r.saw = append(r.saw, p.Principal)

	return storageapi.Chunk{}, nil
}

func (r *recordingFiles) Fetch(
	_ context.Context, p storageapi.FetchParams, _ io.Writer,
) (storageapi.Fetched, error) {
	r.saw = append(r.saw, p.Principal)

	return storageapi.Fetched{}, nil
}

func (r *recordingFiles) Write(
	_ context.Context, p storageapi.WriteParams, _ io.Reader,
) (storageapi.Written, error) {
	r.saw = append(r.saw, p.Principal)

	return storageapi.Written{}, nil
}

func (r *recordingFiles) Move(_ context.Context, p storageapi.MoveParams) (storageapi.Written, error) {
	r.saw = append(r.saw, p.Principal)

	return storageapi.Written{}, nil
}

func (r *recordingFiles) List(_ context.Context, p storageapi.ListParams) (storageapi.Listing, error) {
	r.saw = append(r.saw, p.Principal)

	return storageapi.Listing{}, nil
}

// A listing for a person is vouched for as a read is: refused before the
// storages are reached unless the host handed this instance that person in a
// request it is still serving.
func TestAListForAPersonIsVouchedFor(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "farm")
	h := &Host{}
	inner := &recordingFiles{}
	files := h.vouchStorage(instance, inner)
	params := storageapi.ListParams{Principal: "user:alice", Storage: "bench"}

	_, err := files.List(t.Context(), params)
	require.ErrorIs(t, err, storageapi.ErrDenied)

	leave := h.flights.board(instance, "user:alice", nil)
	defer leave()

	_, err = files.List(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, []string{"user:alice"}, inner.saw)
}

// A write in a person's name passes while that person's request is open, and
// is refused once it has ended, before the storage is reached. A write naming
// nobody is the module's own and passes as it is.
func TestAWriteInAPersonsNameNeedsTheirOpenRequest(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "slicer")
	params := storageapi.WriteParams{Principal: "user:alice", Storage: "shelf", Path: "a.gcode", Size: 1}

	h := &Host{}
	inner := &recordingFiles{}
	files := h.vouchStorage(instance, inner)

	leave := h.flights.board(instance, "user:alice", nil)
	_, err := files.Write(t.Context(), params, strings.NewReader("x"))
	require.NoError(t, err)
	leave()

	_, err = files.Write(t.Context(), params, strings.NewReader("x"))
	require.ErrorIs(t, err, storageapi.ErrDenied)

	params.Principal = ""
	_, err = files.Write(t.Context(), params, strings.NewReader("x"))
	require.NoError(t, err)

	require.Equal(t, []string{"user:alice", ""}, inner.saw)
}

// recordingFrames is a camera door that records who each frame was for.
type recordingFrames struct{ saw []string }

func (r *recordingFrames) Admit(context.Context, cameraapi.AcceptParams) error { return nil }

func (r *recordingFrames) Accept(_ context.Context, params cameraapi.AcceptParams, _ []byte) error {
	r.saw = append(r.saw, params.Principal)

	return nil
}

// A frame in a person's name is the request the host is serving them, and
// once it ends the name is refused. A frame for the module's own work passes
// to the ceiling below.
func TestAFrameInAPersonsNameNeedsTheirOpenRequest(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "buddy3d")
	params := cameraapi.AcceptParams{Principal: "user:alice", Camera: "bed-cam", Size: 1}

	h := &Host{}
	inner := &recordingFrames{}
	frames := h.vouchCameras(instance, inner)

	leave := h.flights.board(instance, "user:alice", nil)
	require.NoError(t, frames.Accept(t.Context(), params, []byte{0xFF}))
	leave()

	require.ErrorIs(t, frames.Admit(t.Context(), params), cameraapi.ErrDenied)
	require.ErrorIs(t, frames.Accept(t.Context(), params, []byte{0xFF}), cameraapi.ErrDenied)

	params.Principal = ""
	require.NoError(t, frames.Accept(t.Context(), params, []byte{0xFF}))

	require.Equal(t, []string{"user:alice", ""}, inner.saw)
	require.Nil(t, h.vouchCameras(instance, nil), "a host with no cameras served a door")
}
