package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// optionalSession is a dialled printer's session with every optional interface
// the host asks for: files, file changes, uploads, a probe, link counters and
// presence.
type optionalSession struct {
	filesSession

	probe       connectorapi.Set
	presence    chan struct{}
	unreachable bool
	closing     sync.Once
}

func (*optionalSession) CreateFolder(context.Context, string) error { return nil }

func (*optionalSession) DeleteFile(context.Context, string) error { return nil }

func (*optionalSession) DeleteFolder(context.Context, string) error { return nil }

func (*optionalSession) Send(context.Context, connectorapi.Upload) error { return nil }

func (s *optionalSession) Probe() connectorapi.Set { return s.probe }

func (*optionalSession) Dropped() uint64 { return 0 }

func (*optionalSession) Refused() uint64 { return 0 }

func (s *optionalSession) Connected() bool { return !s.unreachable }

func (s *optionalSession) PresenceChanged() <-chan struct{} { return s.presence }

// Close ends the state stream, so the plane can release the printer when the
// test ends. A driver ends the stream when its session closes.
func (s *optionalSession) Close() error {
	s.closing.Do(func() { close(s.patches) })

	return nil
}

// The dial loop puts its own type around a driver's session. The host has to
// find each optional interface the driver has through it, and must not find
// one the driver does not have.
func TestADialledSessionKeepsEveryOptionalInterfaceTheDriverHas(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		session connectorapi.Session
		has     bool
	}{
		{"a driver with every interface", &optionalSession{}, true},
		{"a driver with none", &countedSession{}, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			watched, _ := watchSession(testCase.session)

			_, files := capability.As[connectorapi.Filesystem](watched)
			require.Equal(t, testCase.has, files, "the driver's files")

			_, manages := capability.As[connectorapi.FileManager](watched)
			require.Equal(t, testCase.has, manages, "the driver's file changes")

			_, uploads := capability.As[connectorapi.Transfers](watched)
			require.Equal(t, testCase.has, uploads, "the driver's uploads")

			_, probes := capability.As[connectorapi.ReportsProbe](watched)
			require.Equal(t, testCase.has, probes, "the driver's probe")

			_, counts := capability.As[linkCounters](watched)
			require.Equal(t, testCase.has, counts, "the driver's link counters")

			_, present := capability.As[capability.Presence](watched)
			require.Equal(t, testCase.has, present, "the driver's presence")
		})
	}
}

// A dialled printer whose driver says it cannot reach the printer is shown as
// unreachable. The capability session the host keeps for the printer asks
// through the dial loop's wrapper.
func TestADialledPrinterThatIsUnreachableIsShownAsUnreachable(t *testing.T) {
	t.Parallel()

	session := &optionalSession{presence: make(chan struct{}), unreachable: true}
	watched, _ := watchSession(session)
	held := capability.Wrap(watched, capability.Resolution{})

	require.False(t, held.Connected(), "a dialled printer its driver cannot reach is shown as reachable")
	require.NotNil(t, held.PresenceChanged(), "the host got no channel for the driver's reachability changes")
}

// dialsOptional is a dialect for a printer the server dials. The sessions it
// returns have every optional interface. It opens no socket. The test covers
// what the host does with the session after Connect returns it.
type dialsOptional struct {
	baselineDriver

	identity connectorapi.Identity
	probe    connectorapi.Set
}

func (d dialsOptional) Connect(
	context.Context, connectorapi.Endpoint, connectorapi.Set,
) (connectorapi.Session, error) {
	return &optionalSession{
		filesSession: filesSession{countedSession{
			identity: d.identity,
			patches:  make(chan connectorapi.StatePatch),
		}},
		probe: d.probe,
	}, nil
}

// A printer the server dials is served the same way as one that dialled in.
// Its files can be listed, the probe its driver reports narrows what the API
// offers, and its link is counted.
func TestADialledPrinterKeepsItsFilesItsProbeAndItsLinkCounters(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	baseline := connectorapi.Set{
		connectorapi.VerbSendInfo:           {Flag: true},
		connectorapi.VerbSendFileInfo:       {Flag: true},
		connectorapi.VerbCreateFolder:       {Flag: true},
		connectorapi.VerbFileUpload:         {Flag: true},
		connectorapi.Capability(nozzleVerb): {Flag: true},
		connectorapi.Capability(bedVerb):    {Flag: true},
	}

	// The machine says it has everything except a heated bed.
	probe := baseline.Clone()
	delete(probe, connectorapi.Capability(bedVerb))

	identity := connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "DIAL-OPTIONAL"}

	connector := dialledConnector{
		ref:      "near",
		driver:   dialsOptional{baselineDriver: baselineDriver{baseline: baseline}, identity: identity, probe: probe},
		baseline: baseline,
		arrivals: plane.Arrivals("near", baseline, capability.NewResolver(profile.Registry{}, discard()), ""),
	}

	loops := newDials(discard(), []dialledConnector{connector}, fastLadder())
	loops.start(t.Context())

	t.Cleanup(loops.stop)

	loops.SyncEndpoints(t.Context(), []resource.Endpoint{{
		Name: "bench", Connector: "near", Address: "printer.local:7125", Enabled: true,
	}})

	owner := "user:" + config.DefaultPrincipal
	id := plane.idOf(identity)

	require.Eventually(t, func() bool {
		device, err := plane.Get(t.Context(), owner, id)

		return err == nil && device.Connected
	}, settle, 5*time.Millisecond, "the dialled printer never arrived")

	listing, err := plane.Files(t.Context(), owner, id, "usb", true)
	require.NoError(t, err, "the files of a dialled printer could not be listed")
	require.Equal(t, "usb", listing.Directory.Path)

	require.NoError(t, plane.CreateFolder(t.Context(), owner, id, "usb/models"),
		"a folder could not be made on a dialled printer")

	entry, ok := plane.entry(id)
	require.True(t, ok)
	require.NoError(t, entry.session.Send(t.Context(), connectorapi.Upload{Path: "usb/part.bgcode"}),
		"a file could not be sent to a dialled printer")

	device, err := plane.Get(t.Context(), owner, id)
	require.NoError(t, err)

	require.NotContains(t, device.Capabilities, bedVerb,
		"the dialled printer said it has no heated bed and the API offers a bed verb anyway")
	require.Contains(t, device.Capabilities, nozzleVerb)
	require.True(t, device.Link.Counted, "the link of a dialled printer whose driver counts is reported as not counted")
}
