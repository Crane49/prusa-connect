// Package connectorapitest is the conformance suite for connectorapi.Driver
// implementations. The drivers in this repository and a community author's
// drivers pass the same suite, or neither ships.
//
// It proves the property the capability model rests on: a driver's resolved
// capability set never contains anything its Baseline did not, whatever the
// host handed it. A driver that unions its profile in, or that ignores the
// profile and reports its baseline unchanged, fails here rather than in the
// field.
//
// Both directions run the same cases. The suite reads connectorapi.Directions
// and exercises the halves the driver implements: it dials an outbound driver
// at Subject.Endpoint, and it mounts an inbound driver's routes on a loopback
// server and has Subject.Dial make one device dial them. A dialled-in device
// reaches the same three tiers, the same descriptor rules and the same Do as a
// dialled-out one.
//
// Call it from the driver's own _test.go. An outbound subject names Endpoint
// and Connectable; an inbound one names Dial, and the suite owns the server:
//
//	func TestContract(t *testing.T) {
//	    connectorapitest.RunDriverContract(t, func(t *testing.T) connectorapitest.Subject {
//	        return connectorapitest.Subject{
//	            Driver:      mydriver.New(),
//	            Endpoint:    ...,
//	            Connectable: true,
//	            SafeVerb:    "core.command.get_status",
//	        }
//	    })
//	}
//
// An outbound subject declares either Connectable or NoLoopback, never both and
// never neither, and every subject declares either SafeVerb or NoSafeVerb the
// same way. The declaration is required rather than inferred because the halves
// those flags gate are the whole intersective ceiling and the whole of C6: a
// subject that never wrote one would certify green on a run that asserted the
// baseline and nothing else. The suite says which halves it ran and what a
// skipped half did not demonstrate.
//
// NoLoopback describes the outbound half alone. An inbound driver has no such
// answer available: Host stands its routes up with no hardware in reach.
package connectorapitest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	// mountPrefix is where the suite mounts an inbound driver's routes. It is a
	// path no driver would choose for itself, so a driver that works only at the
	// server root fails here rather than at a host that mounts it elsewhere.
	mountPrefix = "/connectors/subject-under-test"

	// arrivalTimeout bounds the wait for a dialled-in device to be announced. A
	// driver that never announces must fail with the clause named, not hang the
	// test binary until the package timeout panics with no message.
	arrivalTimeout = 10 * time.Second
)

// stateCloseTimeout bounds the wait for the state channel to close. A driver
// that leaves it open must fail with the clause named, not hang the test binary
// until the package timeout panics with no clause and no message.
//
// It is a var so this suite's own negative tests, which deliberately run a
// driver that never closes the channel, do not each wait it out.
var stateCloseTimeout = 10 * time.Second

// DialDevice makes exactly one device dial the routes the suite mounted and
// identify itself, so that the driver admits it and announces a session.
//
// baseURL carries the prefix the suite mounted the driver under, which is not
// the driver's own root: a device that reaches the handler only when the prefix
// is absent is a driver that hard-coded where it was mounted.
//
// Returning an error fails the case. Anything the device holds open is the
// implementation's to close, through t.Cleanup.
type DialDevice func(t *testing.T, baseURL string) error

// Subject is a driver instance plus what the suite needs to exercise it.
type Subject struct {
	// Driver is the implementation under test.
	Driver connectorapi.Driver
	// Endpoint reaches a device the suite may connect to. It is read only when
	// the driver is outbound and Connectable is true.
	Endpoint connectorapi.Endpoint
	// Connectable is whether Connect can succeed in this test environment,
	// against hardware or against a loopback the driver exposes for its own
	// tests. It describes the outbound half.
	Connectable bool
	// NoLoopback declares that this driver cannot be dialled in a unit test at
	// all, so the outbound resolution and session halves are skipped. Setting
	// it is a statement that the driver has not demonstrated it intersects: the
	// ceiling assertions, and every assertion about Do, State and Close, are
	// the halves it turns off. A driver that can stand up a loopback sets
	// Connectable instead.
	//
	// It describes the outbound half only. An inbound driver's routes are an
	// http.Handler that the suite mounts itself, so there is nothing it could
	// be unable to reach.
	NoLoopback bool
	// Dial makes one device dial an inbound driver's mounted routes. It is
	// required for a driver that implements connectorapi.Inbound and refused
	// for one that does not.
	Dial DialDevice
	// SafeVerb names a capability the suite may actually invoke against the
	// device: one with no physical effect, such as a status query. The suite
	// sends it through Do and requires a Result back, which is what stops a
	// driver certifying with a Do nothing ever called.
	SafeVerb connectorapi.Capability
	// SafeArgs are the arguments to send with SafeVerb. The suite validates
	// them against the resolved set first, so they must satisfy it.
	SafeArgs map[string]string
	// NoSafeVerb declares that every verb this driver offers moves the machine,
	// so there is none the suite may send. The Do round-trip is then skipped
	// and only the refusal cases run.
	NoSafeVerb bool
}

// MakeSubject builds a fresh subject for a single subtest.
type MakeSubject func(t *testing.T) Subject

// fabricated is a key no real driver declares. The suite hands it to Connect
// inside a profile to prove the driver cannot be talked into offering it, and
// sends it through Do to prove the driver does not answer for a verb it has not
// got.
const fabricated = connectorapi.Capability("core.command.suite_fabricated_verb")

// RunDriverContract runs every conformance case against the subject, once for
// each direction the driver implements.
func RunDriverContract(t *testing.T, newSubject MakeSubject) {
	t.Helper()

	subject := newSubject(t)
	if err := subjectError(subject); err != nil {
		t.Fatal(err)
	}

	t.Run("baseline", func(t *testing.T) { runBaseline(t, newSubject) })

	for _, half := range halves(subject) {
		t.Run(half.name, func(t *testing.T) {
			t.Run("resolution", func(t *testing.T) { runResolution(t, newSubject, half) })
			t.Run("session", func(t *testing.T) { runSession(t, newSubject, half) })
		})
	}
}

// half is one direction the suite exercises, reduced to the only thing the
// cases need from it: given a profile, produce the session the host would hold.
// Everything downstream of that is identical for a device the host dialled and
// a device that dialled the host, which is an earlier design decision as an assertion.
type half struct {
	name    string
	skip    string
	session func(t *testing.T, s Subject, profile connectorapi.Set) connectorapi.Session
}

// halves returns the directions to run. A driver that implements both is run
// twice, because a dialect whose two halves resolve differently is a dialect
// whose printers behave differently depending on who opened the socket.
func halves(s Subject) []half {
	inbound, outbound, err := connectorapi.Directions(s.Driver)
	if err != nil {
		return nil
	}

	var out []half

	if outbound != nil {
		skip := ""
		if s.NoLoopback {
			skip = "Subject.NoLoopback is set: the outbound half did not run, so this driver has not " +
				"demonstrated that a session it dialled is bounded by its baseline, and nothing about " +
				"its Do, State or Close has been exercised"
		}

		out = append(out, half{name: "outbound", skip: skip, session: connectSession})
	}

	if inbound != nil {
		out = append(out, half{name: "inbound", session: dialledInSession})
	}

	return out
}

// subjectError reports why the suite cannot be run against a subject as
// declared, or nil when it can.
//
// Every pair is required rather than defaulted. Connectable defaulting to false
// reduced this suite to its baseline half on a field the driver author sets, and
// reported PASS: a driver that unions its profile into its baseline failed with
// Connectable true and passed with it false. An opt-out has to be written down
// to be taken.
func subjectError(s Subject) error {
	if s.Driver == nil {
		return errors.New("Subject.Driver is nil: there is nothing to run the contract against")
	}

	inbound, outbound, err := connectorapi.Directions(s.Driver)
	if err != nil {
		return fmt.Errorf("%w: a driver implements connectorapi.Outbound, connectorapi.Inbound "+
			"or both, and this one implements neither, so no device can ever reach it", err)
	}

	if declared := outboundDeclarationError(s, outbound != nil); declared != nil {
		return declared
	}

	if declared := inboundDeclarationError(s, inbound != nil); declared != nil {
		return declared
	}

	// The verb pair gates the Do assertions, which run for whichever halves are
	// exercised. A subject that turns every half off has nothing to declare.
	if !exercisesAnyHalf(s, inbound != nil, outbound != nil) {
		return nil
	}

	switch {
	case s.SafeVerb == "" && !s.NoSafeVerb:
		return errors.New("a subject the suite can reach must set Subject.SafeVerb, a capability the " +
			"suite may invoke against the device without moving it, so that Do is exercised at all; set " +
			"Subject.NoSafeVerb to declare that every verb this driver offers moves the machine")
	case s.SafeVerb != "" && s.NoSafeVerb:
		return errors.New("Subject.SafeVerb and Subject.NoSafeVerb are both set; " +
			"one names a verb the suite may send and the other says there is none")
	}

	return nil
}

func outboundDeclarationError(s Subject, isOutbound bool) error {
	if !isOutbound {
		if s.Connectable || s.NoLoopback {
			return errors.New("Subject.Connectable and Subject.NoLoopback describe the outbound half, " +
				"and this driver does not implement connectorapi.Outbound")
		}

		return nil
	}

	switch {
	case !s.Connectable && !s.NoLoopback:
		return errors.New("an outbound subject must set Subject.Connectable, so the suite can connect " +
			"and assert that the resolved set is bounded by the baseline, or Subject.NoLoopback, which " +
			"declares the driver cannot be dialled in a unit test and gives up every assertion " +
			"about the intersective ceiling, Do, State and Close")
	case s.Connectable && s.NoLoopback:
		return errors.New("Subject.Connectable and Subject.NoLoopback are both set; " +
			"one says the suite may connect and the other says it may not")
	}

	return nil
}

func inboundDeclarationError(s Subject, isInbound bool) error {
	switch {
	case isInbound && s.Dial == nil:
		return errors.New("an inbound subject must set Subject.Dial, which makes one device dial the " +
			"routes the suite mounts. There is no opt-out: an inbound driver's routes are an " +
			"http.Handler, so the suite reaches them with no hardware and no loopback of the driver's own")
	case !isInbound && s.Dial != nil:
		return errors.New("Subject.Dial makes a device dial in, and this driver does not implement " +
			"connectorapi.Inbound, so there is nothing mounted for it to reach")
	}

	return nil
}

func exercisesAnyHalf(s Subject, isInbound, isOutbound bool) bool {
	return isInbound || (isOutbound && s.Connectable)
}

// BaselineErrors reports every way a baseline breaks the contract. It is
// exported so that a driver can assert its own table without running a session,
// and so this suite's own tests can drive it with a broken baseline.
func BaselineErrors(baseline connectorapi.Set) []error {
	var problems []error

	if len(baseline) == 0 {
		problems = append(problems, errors.New("baseline is empty: the driver declares no capability at all"))
	}

	return append(problems, baseline.Problems()...)
}

// ResolutionErrors reports a resolved set that the baseline does not contain, or
// that breaks the descriptor rules in its own right.
//
// This is an earlier design decision's intersective ceiling, stated as an assertion: whatever
// the host offered, the answer is bounded by the driver's own code. The merged
// set is also run through the descriptor rules, because a merge can subtract a
// key another key depends on and a set nothing checks is one where a tier that
// lies looks like a tier that is right.
func ResolutionErrors(baseline, resolved connectorapi.Set) []error {
	problems := resolved.Problems()

	for _, key := range resolved.Keys() {
		declared, ok := baseline[key]
		if !ok {
			problems = append(problems, fmt.Errorf(
				"resolved set contains %s, which the baseline does not declare", key))

			continue
		}

		problems = append(problems, widenedErrors(key, declared, resolved[key])...)
	}

	problems = append(problems, droppedRequirementErrors(baseline, resolved)...)

	return problems
}

func runBaseline(t *testing.T, newSubject MakeSubject) {
	t.Helper()

	subject := newSubject(t)

	t.Run("satisfies the descriptor rules", func(t *testing.T) {
		for _, problem := range BaselineErrors(subject.Driver.Baseline()) {
			t.Error(problem)
		}
	})

	t.Run("is pure and unshared", func(t *testing.T) {
		first := subject.Driver.Baseline()
		digest := first.Digest()

		for key := range first {
			delete(first, key)
		}

		second := subject.Driver.Baseline()
		if second.Digest() != digest {
			t.Error("Baseline is not pure: a caller that mutated the returned set changed the next answer")
		}
	})
}

func runResolution(t *testing.T, newSubject MakeSubject, h half) {
	t.Helper()

	if h.skip != "" {
		t.Skip(h.skip)
	}

	subject := newSubject(t)
	baseline := subject.Driver.Baseline()

	t.Run("a profile cannot add a capability the code does not have", func(t *testing.T) {
		profile := baseline.Clone()
		profile[fabricated] = connectorapi.Value{Flag: true}

		resolved, _ := h.session(t, newSubject(t), profile).Capabilities()
		if resolved.Has(fabricated) {
			t.Errorf("%s reached the resolved set from a profile alone", fabricated)
		}

		for _, problem := range ResolutionErrors(baseline, resolved) {
			t.Error(problem)
		}
	})

	t.Run("a profile subtracts what it leaves out", func(t *testing.T) {
		dropped, ok := firstVerb(baseline)
		if !ok {
			t.Skip("the baseline declares no verb to drop")
		}

		profile := baseline.Clone()
		delete(profile, dropped)

		resolved, _ := h.session(t, newSubject(t), profile).Capabilities()
		if resolved.Has(dropped) {
			t.Errorf("%s survived a profile that does not declare it", dropped)
		}
	})

	t.Run("an empty profile leaves nothing", func(t *testing.T) {
		resolved, _ := h.session(t, newSubject(t), connectorapi.Set{}).Capabilities()
		if len(resolved) != 0 {
			t.Errorf("a profile declaring nothing resolved to %d capabilities", len(resolved))
		}
	})
}

func runSession(t *testing.T, newSubject MakeSubject, h half) {
	t.Helper()

	if h.skip != "" {
		t.Skip(h.skip)
	}

	subject := newSubject(t)
	session := h.session(t, subject, subject.Driver.Baseline().Clone())

	t.Run("capabilities do not move within an epoch", func(t *testing.T) {
		first, firstEpoch := session.Capabilities()
		second, secondEpoch := session.Capabilities()

		if firstEpoch != secondEpoch || first.Digest() != second.Digest() {
			t.Errorf("capabilities changed without the epoch moving: %d then %d", firstEpoch, secondEpoch)
		}
	})

	t.Run("the capability set handed out is not the one in force", func(t *testing.T) {
		checkCapabilitiesAreACopy(t, session)
	})

	t.Run("identity reports a model", func(t *testing.T) {
		if session.Identity().Model == "" {
			t.Error("identity carries no model, so no profile can ever be selected for this printer")
		}
	})

	t.Run("every declared verb validates against its own descriptor", func(t *testing.T) {
		checkVerbsValidate(t, session)
	})

	t.Run("a verb the printer does not have is not carried out", func(t *testing.T) {
		checkUndeclaredVerbIsRefused(t, session)
	})

	t.Run("a cancelled command is not reported as done", func(t *testing.T) {
		checkCancellationIsNotSuccess(t, subject, session)
	})

	t.Run("a safe verb round-trips through Do", func(t *testing.T) {
		checkSafeVerbRoundTrips(t, subject, session)
	})

	t.Run("a filesystem half refuses a path that leaves its storage", func(t *testing.T) {
		checkFilesystemConfinement(t, session)
	})

	t.Run("a probe removes capabilities and never adds one", func(t *testing.T) {
		checkProbe(t, subject, session)
	})

	t.Run("nothing reading the state channel does not stop a command", func(t *testing.T) {
		checkStateDoesNotBlockCommands(t, subject, session)
	})

	t.Run("the session counts the patches it drops", func(t *testing.T) {
		checkLinkCounters(t, session)
	})

	t.Run("a print this driver reports carries an id", func(t *testing.T) {
		checkJobsCarryAnID(t, session)
	})

	t.Run("close is idempotent and closes the state channel", func(t *testing.T) {
		checkCloseIsIdempotent(t, session)
	})
}

// escapingPaths are the four shapes a caller composes to leave the storage a
// path names. Three of them carry the segment at an edge, which is what
// substring matching on "/../" misses.
//
//nolint:gochecknoglobals // suite data, read-only.
var escapingPaths = []string{"..", "../etc/passwd", "usb/..", "usb/../../etc/passwd"}

// checkFilesystemConfinement holds a driver that reports files to the one rule
// the vocabulary cannot state: a path carrying a ".." segment never reaches the
// device.
//
// A driver with no filesystem half skips it, and a driver that grows one is
// checked from the first release that has it. The listing itself is not
// asserted: what is on a device is the device's business, and a suite that
// demanded a particular tree would only run against a fixture.
func checkFilesystemConfinement(t *testing.T, session connectorapi.Session) {
	t.Helper()

	files, ok := connectorapi.Files(session)
	if !ok {
		t.Skip("this driver has no filesystem half, so there is no path for it to confine")
	}

	ctx := t.Context()

	if _, err := files.Storages(ctx); err != nil {
		t.Errorf("Storages reported %v; a device that has said nothing reports none, not an error", err)
	}

	for _, path := range escapingPaths {
		listing, err := files.List(ctx, path)
		if err == nil {
			t.Errorf("List(%q) leaves the storage it names and was answered with %+v", path, listing)

			continue
		}

		// The code and not merely an error. A driver that sent the path and got
		// nothing back also returns an error, and the two must not read the
		// same: one is a refusal the caller caused and one is a device that
		// answered nothing.
		var refusal *fault.Fault
		if !errors.As(err, &refusal) || refusal.Code() != connectorapi.PathNotAllowed.Code() {
			t.Errorf("List(%q) reported %v; a path that leaves its storage is refused with %s, "+
				"which connectorapi.RefusePathEscape returns", path, err, connectorapi.PathNotAllowed.Code())
		}
	}
}

// checkCapabilitiesAreACopy proves the caller cannot edit the set the session
// acts on. connectorapi.Session promises the set is immutable for the life of
// the epoch, and a session that hands out its live map lets a caller write a
// capability into it from below the baseline, with a digest that then describes
// a set that is not in force.
func checkCapabilitiesAreACopy(t *testing.T, session connectorapi.Session) {
	t.Helper()

	first, _ := session.Capabilities()
	before := first.Digest()

	first[fabricated] = connectorapi.Value{Flag: true}

	second, _ := session.Capabilities()
	if second.Has(fabricated) {
		t.Errorf("%s was written into the set a caller was handed and came back on the next read: "+
			"Capabilities returns the live map", fabricated)
	}

	if second.Digest() != before {
		t.Error("the capability set changed because a caller mutated what it was handed")
	}
}

// checkVerbsValidate walks the resolved verbs through host-side validation. A
// verb that refuses its own empty command for any reason other than a required
// argument is a descriptor nothing can act on.
func checkVerbsValidate(t *testing.T, session connectorapi.Session) {
	t.Helper()

	caps, _ := session.Capabilities()

	for _, key := range caps.Keys() {
		if _, isArgument := key.Argument(); isArgument || !caps.Has(key) {
			continue
		}

		_, err := caps.Validate(connectorapi.Command{Verb: key})
		if err == nil || fault.HasCode(err, connectorapi.ArgumentMissing.Code()) {
			continue
		}

		t.Errorf("%s: a command with no arguments was refused for a reason other than a missing "+
			"required argument: %v", key, err)
	}
}

// checkUndeclaredVerbIsRefused sends a verb no descriptor carries. The host
// refuses it before a driver is reached, so this is the last line rather than
// the first: a Do that answers OK for a verb the printer does not have is a
// driver reporting an outcome that did not happen, and it is what a suite that
// never calls Do cannot tell apart from a driver that works.
func checkUndeclaredVerbIsRefused(t *testing.T, session connectorapi.Session) {
	t.Helper()

	caps, _ := session.Capabilities()
	if caps.Has(fabricated) {
		t.Fatalf("%s is in the resolved set, so it is not the undeclared verb this case needs", fabricated)
	}

	result, err := session.Do(t.Context(), connectorapi.Command{Verb: fabricated})
	if err == nil && result.OK {
		t.Errorf("Do reported %s carried out, and no descriptor declares it", fabricated)
	}
}

// checkCancellationIsNotSuccess sends a command whose context is already done. A
// driver that reports OK for it is reporting an outcome it cannot have observed.
func checkCancellationIsNotSuccess(t *testing.T, subject Subject, session connectorapi.Session) {
	t.Helper()

	if subject.NoSafeVerb {
		t.Skip("Subject.NoSafeVerb is set: there is no verb the suite may send, cancelled or not")
	}

	cmd, ok := safeCommand(t, subject, session)
	if !ok {
		return
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := session.Do(ctx, cmd)
	if err == nil && result.OK {
		t.Errorf("Do reported %s carried out on a context that was already cancelled", cmd.Verb)
	}
}

// checkSafeVerbRoundTrips is the one clause that sends a command the printer
// really has. Without it a driver whose Do accepts everything and answers OK
// passes the whole suite, because nothing ever called it.
func checkSafeVerbRoundTrips(t *testing.T, subject Subject, session connectorapi.Session) {
	t.Helper()

	if subject.NoSafeVerb {
		t.Skip("Subject.NoSafeVerb is set: every verb this driver offers moves the machine, so Do " +
			"was exercised with refusals only")
	}

	cmd, ok := safeCommand(t, subject, session)
	if !ok {
		return
	}

	result, err := session.Do(t.Context(), cmd)
	if err != nil {
		t.Fatalf("Do(%s): %v", cmd.Verb, err)
	}

	if !result.OK {
		t.Errorf("Do(%s) reported the command was not accepted, and it is the verb the subject "+
			"declared safe to send", cmd.Verb)
	}

	// A machine reason explains a refusal. One on a command that was carried
	// out leaves a host holding two answers, and the host that acts on the
	// reason is the one that reprints a file the printer said it already had.
	if result.OK && result.MachineReason != "" {
		t.Errorf("Do(%s) reported the command carried out and gave %q as the reason it was refused",
			cmd.Verb, result.MachineReason)
	}

	for _, ignored := range result.Ignored {
		if _, sent := cmd.Args[ignored]; !sent {
			t.Errorf("Do(%s) reports it ignored %q, which was not sent", cmd.Verb, ignored)
		}
	}

	for name := range result.Substituted {
		if _, sent := cmd.Args[name]; !sent {
			t.Errorf("Do(%s) reports it substituted %q, which was not sent", cmd.Verb, name)
		}
	}
}

// safeCommand builds the declared safe command and puts it through host-side
// validation, which is the only way a driver is ever reached.
func safeCommand(t *testing.T, subject Subject, session connectorapi.Session) (connectorapi.Command, bool) {
	t.Helper()

	caps, _ := session.Capabilities()

	if !caps.Has(subject.SafeVerb) {
		t.Errorf("Subject.SafeVerb is %s, which the resolved set does not declare", subject.SafeVerb)

		return connectorapi.Command{}, false
	}

	cmd, err := caps.Validate(connectorapi.Command{Verb: subject.SafeVerb, Args: subject.SafeArgs})
	if err != nil {
		t.Errorf("Subject.SafeVerb %s with Subject.SafeArgs %v does not satisfy the printer's own "+
			"descriptor: %v", subject.SafeVerb, subject.SafeArgs, err)

		return connectorapi.Command{}, false
	}

	return cmd, true
}

// checkCloseIsIdempotent closes twice and drains the state channel, because a
// session that leaves the channel open leaks every subscriber reading it.
//
// The drain is bounded. Draining an open channel with a bare range hangs the
// whole test binary until the package timeout panics, naming no clause: for a
// suite a community author runs against their own driver, "it hangs" is the
// worst diagnosis there is.
func checkCloseIsIdempotent(t *testing.T, session connectorapi.Session) {
	t.Helper()

	patches := session.State()
	if patches == nil {
		t.Fatal("State returned a nil channel, which a subscriber ranges over forever")
	}

	if err := session.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}

	if err := session.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	drained := make(chan struct{})

	go func() {
		defer close(drained)

		for range patches { // draining is the point; the loop ends when the channel closes.
		}
	}()

	timeout := time.NewTimer(stateCloseTimeout)
	defer timeout.Stop()

	select {
	case <-drained:
	case <-timeout.C:
		t.Errorf("the state channel was still open %v after Close returned. Close must close it, "+
			"or every subscriber ranging over it blocks for the life of the process", stateCloseTimeout)
	}
}

// connectSession is the outbound source: the host dials the endpoint
// configuration named and the driver answers with a session.
func connectSession(t *testing.T, subject Subject, profile connectorapi.Set) connectorapi.Session {
	t.Helper()

	outbound, ok := subject.Driver.(connectorapi.Outbound)
	if !ok {
		t.Fatal("the outbound half is running against a driver that does not implement connectorapi.Outbound")
	}

	session, err := outbound.Connect(t.Context(), subject.Endpoint, profile)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	return closeWithTest(t, session)
}

// dialledInSession is the inbound source: the suite mounts the driver's routes
// behind a prefix, a device dials them, and the session the driver announces to
// the host fake is the one every case downstream runs against.
//
// The prefix is deliberately not the server root. A driver that writes absolute
// paths into its own routes works at the root and breaks the moment a host
// mounts it anywhere else, and that is a failure a community author would hit
// in production rather than here.
func dialledInSession(t *testing.T, subject Subject, profile connectorapi.Set) connectorapi.Session {
	t.Helper()

	inbound, ok := subject.Driver.(connectorapi.Inbound)
	if !ok {
		t.Fatal("the inbound half is running against a driver that does not implement connectorapi.Inbound")
	}

	host := NewHost(profile)

	handler, err := inbound.Handler(host)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	if handler == nil {
		t.Fatal("Handler returned no routes, so a device has nothing to dial")
	}

	server := httptest.NewServer(http.StripPrefix(mountPrefix, handler))
	t.Cleanup(server.Close)

	if dialled := subject.Dial(t, server.URL+mountPrefix); dialled != nil {
		t.Fatalf("Subject.Dial: %v", dialled)
	}

	ctx, cancel := context.WithTimeout(t.Context(), arrivalTimeout)
	defer cancel()

	session, err := host.AwaitSession(ctx)
	if err != nil {
		t.Fatalf("a device dialled the mounted routes and no session was announced: %v", err)
	}

	if len(host.Admissions()) == 0 {
		t.Error("a session was announced for a device the driver never put through Arrivals.Admit, " +
			"so the host never decided whether it owns this printer")
	}

	return closeWithTest(t, session)
}

func closeWithTest(t *testing.T, session connectorapi.Session) connectorapi.Session {
	t.Helper()

	t.Cleanup(func() {
		if closeErr := session.Close(); closeErr != nil {
			t.Errorf("Close: %v", closeErr)
		}
	})

	return session
}

func firstVerb(set connectorapi.Set) (connectorapi.Capability, bool) {
	for _, key := range set.Keys() {
		if _, isArgument := key.Argument(); isArgument {
			continue
		}

		if set.Has(key) {
			return key, true
		}
	}

	return "", false
}

// widenedErrors reports a resolved value that permits more than the baseline
// did. Subtracting is the whole point; adding is the defect.
func widenedErrors(key connectorapi.Capability, declared, resolved connectorapi.Value) []error {
	var problems []error

	if resolved.Flag && !declared.Flag {
		problems = append(problems, fmt.Errorf("%s is on in the resolved set and off in the baseline", key))
	}

	if resolved.Kind != declared.Kind {
		problems = append(problems, fmt.Errorf("%s resolved to kind %v, declared as %v",
			key, resolved.Kind, declared.Kind))
	}

	for _, value := range resolved.Supported {
		if len(declared.Supported) > 0 && !slices.Contains(declared.Supported, value) {
			problems = append(problems, fmt.Errorf("%s resolved to accept %q, which the baseline does not",
				key, value))
		}
	}

	// Dropping Required widens: the caller may now leave out what the driver's
	// code demands, and the host will not stop it.
	if declared.Required && !resolved.Required {
		problems = append(problems, fmt.Errorf(
			"%s is required in the baseline and optional in the resolved set", key))
	}

	// Minting a default widens the same way from the other side: a value the
	// baseline never named is filled in and handed to the driver as though the
	// caller had asked for it.
	if resolved.Default != declared.Default && resolved.Default != "" {
		problems = append(problems, fmt.Errorf("%s resolved to default %q, which the baseline does not name",
			key, resolved.Default))
	}

	problems = append(problems, boundErrors(key, declared, resolved)...)

	return problems
}

// boundErrors reports a bound the resolved set loosened. A limit that rose or
// vanished, and a lower bound that fell or vanished, each let through a value
// the baseline refused.
func boundErrors(key connectorapi.Capability, declared, resolved connectorapi.Value) []error {
	var problems []error

	if resolved.HasLimit && declared.HasLimit && resolved.Limit > declared.Limit {
		problems = append(problems, fmt.Errorf("%s resolved to a limit of %v, above the baseline's %v",
			key, resolved.Limit, declared.Limit))
	}

	if !resolved.HasLimit && declared.HasLimit {
		problems = append(problems, fmt.Errorf("%s dropped the baseline's limit of %v", key, declared.Limit))
	}

	if resolved.HasMin && declared.HasMin && resolved.Min < declared.Min {
		problems = append(problems, fmt.Errorf("%s resolved to a lower bound of %v, below the baseline's %v",
			key, resolved.Min, declared.Min))
	}

	if !resolved.HasMin && declared.HasMin {
		problems = append(problems, fmt.Errorf("%s dropped the baseline's lower bound of %v",
			key, declared.Min))
	}

	return problems
}

// droppedRequirementErrors reports a verb the resolved set still offers whose
// required argument it no longer carries.
//
// Subtracting the argument key looks like narrowing and is not: the descriptor
// says a verb accepts exactly the arguments it carries keys for, so the caller
// may now invoke the verb with nothing at all, against code that demands a
// value. The narrowing answer is to subtract the verb.
func droppedRequirementErrors(baseline, resolved connectorapi.Set) []error {
	var problems []error

	for _, key := range baseline.Keys() {
		value := baseline[key]
		if !value.Flag || !value.Required {
			continue
		}

		if _, isArgument := key.Argument(); !isArgument {
			continue
		}

		if resolved.Has(key) || !resolved.Has(key.Verb()) {
			continue
		}

		problems = append(problems, fmt.Errorf(
			"the resolved set still offers %s but dropped its required argument %s, so the verb may now "+
				"be invoked with nothing against code that demands a value", key.Verb(), key))
	}

	return problems
}
