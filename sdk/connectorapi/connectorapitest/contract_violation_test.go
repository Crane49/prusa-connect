package connectorapitest

import (
	"context"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A conformance suite that has never been watched failing is a suite that
// proves nothing. Each case here is a driver that breaks one clause, run
// through the whole of RunDriverContract, with the run required to fail and to
// name the clause.
//
// The run happens in a child copy of this test binary. A subtest that fails
// fails its parent, so there is no way to assert "this run failed" in-process,
// and the driver whose state channel never closes has to be watched not hanging
// the binary, which is the other half of what is being proved.
const (
	violatorEnv     = "CONNECTORAPITEST_VIOLATOR"
	violatorTest    = "TestTheSuiteAgainstAViolatingDriver"
	childRunTimeout = 2 * time.Minute

	// deadlockReport is how the runtime ends a process whose every goroutine is
	// blocked.
	deadlockReport = "all goroutines are asleep"
)

// violators are drivers that each break exactly one clause, with the words the
// suite must say when it catches them.
var violators = map[string]struct {
	subject func() Subject
	names   string
}{
	"unions-the-profile": {
		subject: func() Subject {
			return Subject{Driver: unionDriver{}, Connectable: true, SafeVerb: verbHome}
		},
		names: "which the baseline does not declare",
	},
	"hands-out-the-live-map": {
		subject: func() Subject {
			return Subject{Driver: liveMapDriver{}, Connectable: true, SafeVerb: verbHome}
		},
		names: "Capabilities returns the live map",
	},
	"says-yes-to-everything": {
		subject: func() Subject {
			return Subject{Driver: alwaysOKDriver{}, Connectable: true, SafeVerb: verbHome}
		},
		names: "carried out, and no descriptor declares it",
	},
	"explains-a-command-it-accepted": {
		subject: func() Subject {
			return Subject{Driver: explainingDriver{}, Connectable: true, SafeVerb: verbHome}
		},
		names: "as the reason it was refused",
	},
	"never-closes-the-state-channel": {
		subject: func() Subject {
			return Subject{Driver: openChannelDriver{}, Connectable: true, SafeVerb: verbHome}
		},
		names: "the state channel was still open",
	},
	"drops-a-required-argument": {
		subject: func() Subject {
			return Subject{Driver: dropRequiredDriver{}, Connectable: true, NoSafeVerb: true}
		},
		names: "dropped its required argument",
	},
	"mints-a-default": {
		subject: func() Subject {
			return Subject{Driver: mintDefaultDriver{}, Connectable: true, NoSafeVerb: true}
		},
		names: "which the baseline does not name",
	},
	"declares-a-default-its-own-limit-refuses": {
		subject: func() Subject {
			return Subject{Driver: badDefaultDriver{}, NoLoopback: true}
		},
		names: "which its own descriptor refuses",
	},
	"declares-a-required-argument-with-a-default": {
		subject: func() Subject {
			return Subject{Driver: requiredWithDefaultDriver{}, NoLoopback: true}
		},
		names: "can never be missing",
	},
	"unions-the-profile-on-a-device-that-dialled-in": {
		subject: func() Subject {
			return Subject{Driver: inboundUnionDriver{}, Dial: dialOneDevice, SafeVerb: verbHome}
		},
		names: "which the baseline does not declare",
	},
	"announces-a-device-the-host-never-admitted": {
		subject: func() Subject {
			return Subject{Driver: unadmittedDriver{}, Dial: dialOneDevice, SafeVerb: verbHome}
		},
		names: "never put through Arrivals.Admit",
	},
	"routes-from-the-server-root-instead-of-its-own": {
		subject: func() Subject {
			return Subject{Driver: rootedDriver{}, Dial: dialOneDevice, SafeVerb: verbHome}
		},
		names: "Subject.Dial",
	},
}

func TestTheSuiteFailsEveryViolatingDriver(t *testing.T) {
	t.Parallel()

	if os.Getenv(violatorEnv) != "" {
		t.Skip("this is the child run; it exists to be failed by the parent's case")
	}

	for name, tc := range violators {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			output, err := runChild(t, name)
			if err == nil {
				t.Fatalf("the suite passed a driver that %s:\n%s", name, output)
			}

			if !strings.Contains(output, tc.names) {
				t.Errorf("the suite failed the driver but never said %q, so the author is not told "+
					"which clause broke:\n%s", tc.names, output)
			}
		})
	}
}

// The same drivers with their one defect repaired must pass, or the case above
// proves only that the suite dislikes something.
func TestTheSuitePassesEveryRepairedDriver(t *testing.T) {
	t.Parallel()

	if os.Getenv(violatorEnv) != "" {
		t.Skip("this is the child run")
	}

	output, err := runChild(t, "repaired")
	if err != nil {
		t.Fatalf("the suite failed a driver whose only defect was repaired: %v\n%s", err, output)
	}
}

// TestTheSuiteAgainstAViolatingDriver is the child. It runs the whole contract
// against the driver its parent named and is expected to fail.
func TestTheSuiteAgainstAViolatingDriver(t *testing.T) {
	name := os.Getenv(violatorEnv)
	if name == "" {
		t.Skip("no violator named: this case only runs as a child of TestTheSuiteFailsEveryViolatingDriver")
	}

	// The driver that never closes its state channel must be watched failing
	// with the clause named rather than hanging, and there is no reason to wait
	// the production timeout out to see it.
	stateCloseTimeout = time.Second

	if name == "repaired" {
		RunDriverContract(t, func(_ *testing.T) Subject {
			return Subject{Driver: fakeDriver{}, Connectable: true, SafeVerb: verbHome}
		})

		return
	}

	tc, ok := violators[name]
	if !ok {
		t.Fatalf("no violator named %q", name)
	}

	RunDriverContract(t, func(_ *testing.T) Subject { return tc.subject() })
}

func runChild(t *testing.T, name string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), childRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+violatorTest, "-test.v")
	cmd.Env = append(os.Environ(), violatorEnv+"="+name)

	output, err := cmd.CombinedOutput()

	// A child that hangs also exits non-zero in the end, which the cases above
	// would read as the suite failing the fixture. It is the suite hanging on
	// it, which the bounded waits in the suite are there to prevent, and it has
	// to say so. It ends one of two ways: killed at the deadline, or, on a
	// machine idle enough that no timer is pending, by the runtime's deadlock
	// detector. Which one depends on the machine.
	if ctx.Err() != nil {
		t.Fatalf("the suite hung on the %s fixture until it was killed after %s, instead of "+
			"failing it:\n%s", name, childRunTimeout, output)
	}

	if strings.Contains(string(output), deadlockReport) {
		t.Fatalf("the suite hung on the %s fixture until the runtime reported a deadlock, "+
			"instead of failing it:\n%s", name, output)
	}

	return string(output), err
}

// baseSession is a conforming session the violators embed and each break in one
// place.
type baseSession struct {
	caps    connectorapi.Set
	patches chan connectorapi.StatePatch
	closed  bool
}

func newBaseSession(caps connectorapi.Set) *baseSession {
	return &baseSession{caps: caps, patches: make(chan connectorapi.StatePatch)}
}

func (s *baseSession) Identity() connectorapi.Identity {
	return connectorapi.Identity{Vendor: "test", Model: "FAKE", Firmware: connectorapi.NewFirmwareLabel("1.0.0")}
}

func (s *baseSession) Capabilities() (connectorapi.Set, uint64) { return s.caps.Clone(), 1 }

func (s *baseSession) Do(ctx context.Context, cmd connectorapi.Command) (connectorapi.Result, error) {
	if err := ctx.Err(); err != nil {
		return connectorapi.Result{}, err
	}

	if !s.caps.Has(cmd.Verb) {
		return connectorapi.Result{}, connectorapi.CapabilityNotSupported.New("capability not supported")
	}

	return connectorapi.Result{OK: true}, nil
}

func (s *baseSession) State() <-chan connectorapi.StatePatch { return s.patches }

func (s *baseSession) Close() error {
	if !s.closed {
		s.closed = true

		close(s.patches)
	}

	return nil
}

// unionDriver folds the profile into its baseline, which is the one thing
// an earlier design decision forbids.
type unionDriver struct{}

func (unionDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d unionDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	caps := d.Baseline().Clone()
	maps.Copy(caps, profile)

	return newBaseSession(caps), nil
}

// liveMapDriver hands its own map to the caller.
type liveMapDriver struct{}

func (liveMapDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d liveMapDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	return &liveMapSession{baseSession: newBaseSession(d.Baseline().Intersect(profile))}, nil
}

type liveMapSession struct{ *baseSession }

func (s *liveMapSession) Capabilities() (connectorapi.Set, uint64) { return s.caps, 1 }

// alwaysOKDriver reports every command carried out, including one no descriptor
// declares.
type alwaysOKDriver struct{}

func (alwaysOKDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d alwaysOKDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	return &alwaysOKSession{baseSession: newBaseSession(d.Baseline().Intersect(profile))}, nil
}

type alwaysOKSession struct{ *baseSession }

func (s *alwaysOKSession) Do(_ context.Context, _ connectorapi.Command) (connectorapi.Result, error) {
	return connectorapi.Result{OK: true}, nil
}

// explainingDriver answers a command it carried out with the word a dialect
// uses for a refusal, so a host reading the reason acts on a refusal that never
// happened.
type explainingDriver struct{}

func (explainingDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d explainingDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	return &explainingSession{baseSession: newBaseSession(d.Baseline().Intersect(profile))}, nil
}

type explainingSession struct{ *baseSession }

func (s *explainingSession) Do(
	ctx context.Context, cmd connectorapi.Command,
) (connectorapi.Result, error) {
	result, err := s.baseSession.Do(ctx, cmd)
	if err != nil {
		return connectorapi.Result{}, err
	}

	result.MachineReason = "FILE_EXISTS"

	return result, nil
}

// openChannelDriver closes without closing its state channel. A suite that does
// not time out on this hangs with no message and no clause naming the driver.
type openChannelDriver struct{}

func (openChannelDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d openChannelDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	return &openChannelSession{baseSession: newBaseSession(d.Baseline().Intersect(profile))}, nil
}

type openChannelSession struct{ *baseSession }

func (s *openChannelSession) Close() error { return nil }

// requiredBaseline demands an argument, which the two drivers below each widen
// away in their own direction.
func requiredBaseline() connectorapi.Set {
	return connectorapi.Set{
		verbHome:  {Flag: true},
		verbPrint: {Flag: true},
		argPath:   {Flag: true, Kind: connectorapi.KindString, Required: true},
	}
}

// dropRequiredDriver keeps the verb and drops the argument its code demands, so
// the caller may now invoke it with nothing.
type dropRequiredDriver struct{}

func (dropRequiredDriver) Baseline() connectorapi.Set { return requiredBaseline() }

func (d dropRequiredDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	caps := d.Baseline().Intersect(profile)
	caps[verbPrint] = connectorapi.Value{Flag: true}
	delete(caps, argPath)

	return newBaseSession(caps), nil
}

// mintDefaultDriver invents a value the baseline never named and has the host
// fill it in for every caller that stays silent.
type mintDefaultDriver struct{}

func (mintDefaultDriver) Baseline() connectorapi.Set { return requiredBaseline() }

func (d mintDefaultDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	caps := d.Baseline().Intersect(profile)
	caps[verbPrint] = connectorapi.Value{Flag: true}
	caps[argPath] = connectorapi.Value{
		Flag: true, Kind: connectorapi.KindString, Required: true, Default: "minted.gcode",
	}

	return newBaseSession(caps), nil
}

// badDefaultDriver ships a baseline whose default its own limit refuses. The
// host fills that default in without the caller ever naming it, so an unchecked
// default is a value that reaches a driver by declaration alone.
type badDefaultDriver struct{}

func (badDefaultDriver) Baseline() connectorapi.Set {
	return connectorapi.Set{
		verbPrint: {Flag: true},
		argPath: {
			Flag: true, Kind: connectorapi.KindInteger,
			Default: "99999", Limit: 10, HasLimit: true,
		},
	}
}

func (badDefaultDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, _ connectorapi.Set,
) (connectorapi.Session, error) {
	return nil, context.Canceled
}

// requiredWithDefaultDriver declares an argument required and gives it a
// default, so the default is always filled in and the requirement can never be
// enforced against anyone.
type requiredWithDefaultDriver struct{}

func (requiredWithDefaultDriver) Baseline() connectorapi.Set {
	return connectorapi.Set{
		verbPrint: {Flag: true},
		argPath: {
			Flag: true, Kind: connectorapi.KindString,
			Required: true, Default: "a.gcode",
		},
	}
}

func (requiredWithDefaultDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, _ connectorapi.Set,
) (connectorapi.Session, error) {
	return nil, context.Canceled
}

// inboundUnionDriver announces a session that reports its whole baseline
// whatever profile the host admitted it with. It is the outbound union defect
// reached through the other door, and the inbound half must catch it there too.
type inboundUnionDriver struct{}

func (inboundUnionDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d inboundUnionDriver) Handler(arrivals connectorapi.Arrivals) (http.Handler, error) {
	baseline := d.Baseline()

	return inboundHandler(arrivals, baseline, func(_ connectorapi.Set) connectorapi.Session {
		widened := baseline.Clone()
		widened["core.command.beep"] = connectorapi.Value{Flag: true}

		return newBaseSession(widened)
	}), nil
}

// unadmittedDriver hands the host a session for a device the host was never
// asked about, which is an inbound connector serving a printer nobody approved.
type unadmittedDriver struct{}

func (unadmittedDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d unadmittedDriver) Handler(arrivals connectorapi.Arrivals) (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /connect", func(w http.ResponseWriter, r *http.Request) {
		session := newBaseSession(d.Baseline())

		if err := arrivals.Arrived(r.Context(), session); err != nil {
			_ = session.Close()
			http.Error(w, "not taken", http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	})

	return mux, nil
}

// rootedDriver writes the prefix a host happened to mount it under into its own
// routes, so it works at the server root and nowhere else.
type rootedDriver struct{}

func (rootedDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d rootedDriver) Handler(arrivals connectorapi.Arrivals) (http.Handler, error) {
	inner := inboundHandler(arrivals, d.Baseline(), func(caps connectorapi.Set) connectorapi.Session {
		return newBaseSession(caps)
	})

	mux := http.NewServeMux()
	mux.Handle("/connectors/subject-under-test/", inner)

	return mux, nil
}
