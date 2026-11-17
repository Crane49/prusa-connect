package resourcehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// errNoService reports an instance whose runtime is service and which carries
// no service block. The loader refuses that declaration, so reaching it here is
// a defect in this host and not a mistake in the operator's file.
var errNoService = errors.New("resourcehost: the instance carries no service block")

// errChildEnded reports a child that ended when the host had not asked it to.
// The instance's Reachable condition carries it. The last lines the child
// wrote are in the log.
var errChildEnded = errors.New("resourcehost: the child process ended and the host did not ask it to")

// stopShare is the fraction of the supervisor's stop budget the host waits for
// a child to stop on its own. The rest of the budget is for killing and
// reaping it, so the process is gone before the supervisor reports the
// teardown as stuck.
const stopShare = 2

// hardLimitMarginPercent is how far above the budget the hard limit sits. The
// watch then sees a module go over its budget and stops it with that reason
// before the hard limit makes the module fail on its own.
const hardLimitMarginPercent = 25

// serviceSpec is the declaration of an instance whose runtime is service: the
// type whose artefact is launched, where that artefact is, and the namespace
// its surface is served under. It is separate from spec because a registered
// type is built from registry.Spec, and a service type is registered in no
// build of this binary.
type serviceSpec struct {
	typeName  string
	namespace string
	service   install.Service
	// module is what the module's own manifest said about itself when it was
	// installed, and nil for an instance that is not a module. It is what the
	// running child is held to; see manifestOf.
	module *install.ModuleEntry
	// config is the operator's own settings block for this instance, as it was
	// written. It is merged over the defaults the module declared and sent to
	// the process on plugin.configure.
	config json.RawMessage
	// budget is the memory the process may use, in bytes.
	budget int64
}

// serviceOf is the service half of a declaration, and nil for every instance
// reached any other way.
func serviceOf(declared resource.Declaration) *serviceSpec {
	if declared.Spec.Runtime != install.RuntimeService || declared.Spec.Service == nil {
		return nil
	}

	namespace, _ := declared.ModuleNamespace()
	budget, _ := resource.BudgetOf(declared.Spec)

	return &serviceSpec{
		typeName:  declared.Spec.Type,
		namespace: namespace,
		service:   *declared.Spec.Service,
		module:    declared.Spec.Module,
		config:    declared.Spec.Config,
		budget:    budget.Bytes,
	}
}

// manifestOf is what the install recorded about the module, in the shape the
// launch holds the running child to: the name off the type the artefact was
// installed under, the release off the module block, and the wire major off
// the service block.
//
// A release directory is staged and thrown away, and a module that runs a
// process leaves no manifest on disk, so the declaration is where the record of
// what was installed lives. An instance with no module block was not installed
// from a manifest, and nothing is checked for it.
func manifestOf(declared *serviceSpec) *manifest.Manifest {
	if declared.module == nil {
		return nil
	}

	return &manifest.Manifest{
		Name:          artefactName(declared.typeName),
		Version:       declared.module.Version,
		ContractMajor: declared.service.ContractMajor,
	}
}

// buildService launches one instance out of process and returns the target
// whose Close stops it. The child's memory is held to the declared budget by
// watch from the moment it serves, and a child that stops answering is killed
// by a hangWatch under hang.
//
// The three refusals before anything runs each carry their own code, because
// each names a different action: install the artefact, reinstall it, or run a
// host that serves the contract the extension was built against.
//
// mount is the path the instance's routes are served under, the namespace
// group or the root prefix the operator named, and empty for a service instance
// that is not a module. It is where the child's OpenAPI fragment, when it ships
// one, is rebased to.
func buildService(
	startCtx context.Context,
	handle supervisor.Handle,
	instance ref.Ref,
	declared *serviceSpec,
	stop time.Duration,
	serves wirerpc.Methods,
	mount string,
	grants *wirerpc.Grants,
	watch *memoryWatch,
	reaper *procgroup.Reaper,
	status func() supervisor.Status,
	hang hangPolicy,
	live *liveChildren,
) (any, error) {
	if declared == nil {
		return nil, fmt.Errorf("%w: %s", errNoService, instance)
	}

	if err := servesContract(instance, declared.service.ContractMajor); err != nil {
		return nil, err
	}

	// The settings are merged before anything is executed. The operator wrote
	// the block and the install recorded the keys the module reads, so a
	// mistake in it is reported without starting a process.
	merged, err := moduleSettings(declared, declared.config)
	if err != nil {
		return nil, settingsFailure(instance, err)
	}

	path, findErr := wirehost.Find(declared.service.Path, artefactName(declared.typeName))
	if err = findErr; err != nil {
		return nil, pluginMissing.New("no artefact to launch").
			With(
				fault.StringParam("resource", instance.String()),
				fault.StringParam("path", declared.service.Path),
			).
			Wrap(err)
	}

	ask := stop / stopShare

	// Methods is the host's side of the session. It holds the module's
	// key/value methods. It is built per child from the namespace the
	// operator's declaration produced, so the store this module reaches is
	// decided before its process exists. Nothing the module says changes it.
	child, cancel, err := launch(startCtx, wirehost.Options{
		Instance: instance.String(),
		Path:     path,
		Digest:   wirehost.Digest(declared.service.Checksum),
		Manifest: manifestOf(declared),
		Stop:     ask,
		Methods:  serves,
		Logger:   slog.New(newTail(watch.log, handle.Logger.Handler())),
		Env:      childEnv(os.Environ(), declared.budget, runtime.GOOS == "windows"),
		Reaper:   reaper,
	})
	if err != nil {
		return nil, launchFailure(instance, path, err)
	}

	live.add(child)

	if err = raiseOOMScore(child.Pid()); err != nil {
		handle.Logger.WarnContext(startCtx, "the kernel may stop the server before this module when memory runs out",
			"resource", instance.String(), "error", err.Error())
	}

	// Before any other call, so a module never serves a request under settings
	// it has not been given.
	configured, err := configure(startCtx, instance, child, merged, grants)
	if err != nil {
		_ = child.Close()
		cancel()

		return nil, err
	}

	info := child.Info()
	handle.Logger.InfoContext(startCtx, "the child process is serving",
		"resource", instance.String(),
		"path", path,
		"plugin", info.Name,
		"plugin_version", string(info.Version),
		"contract", string(info.Contract),
		"wire_minor", child.Minor(),
		"mount", mount)

	document, err := moduleDocument(startCtx, handle.Logger, instance, child, mount)
	if err != nil {
		_ = child.Close()
		cancel()

		return nil, err
	}

	// The browser half, when the declaration carries one. It is opened after
	// the child answered, so a module whose process will not start never
	// serves files under a namespace whose routes answer nothing.
	var bundle *assetBundle

	if declared.service.Assets != nil {
		if bundle, err = openBundle(
			startCtx, handle, instance, declared.namespace, *declared.service.Assets,
		); err != nil {
			_ = child.Close()
			cancel()

			return nil, err
		}
	}

	target := &serviceChild{
		child: child, cancel: cancel, ask: ask, document: document, bundle: bundle, database: configured.Database,
		stopped: make(chan struct{}), log: handle.Logger, instance: instance,
	}

	go target.watch(handle.Status)

	// The hard limit is applied from the watch's budget, not the declaration
	// this start began with, so an edit made during the start is the one in
	// force.
	// An edit applies the limit long after the start's context ended.
	limitCtx := context.WithoutCancel(startCtx)
	token := watch.attach(func(bytes int64) {
		if limitErr := child.LimitMemory(bytes + bytes*hardLimitMarginPercent/100); limitErr != nil {
			handle.Logger.WarnContext(limitCtx, "the module's memory budget could not be set as a hard limit",
				"resource", instance.String(), "bytes", bytes, "error", limitErr.Error())
		}
	})

	go func() {
		defer watch.detach(token)

		watch.watch(child.Exited(), target.stopped, child.Group().Memory, target.killFor,
			func(stop supervisor.Stop) {
				handle.Logger.WarnContext(limitCtx, "the module went over its memory budget and keeps running",
					"resource", instance.String(), "used_bytes", stop.Used, "budget_bytes", stop.Budget)
			})
	}()

	hung := &hangWatch{
		policy: hang,
		ping: func(ctx context.Context) error {
			if _, pingErr := child.Conn().Call(ctx, wirerpc.MethodPing, nil); pingErr != nil {
				return fmt.Errorf("ping: %w", pingErr)
			}

			return nil
		},
		status: status, kill: target.killFor, log: handle.Logger,
	}
	go hung.run(limitCtx, child.Exited(), target.stopped)

	return target, nil
}

// moduleDocument is the child's OpenAPI fragment rebased under the module's
// mount, and nil for a child that serves no routes or is not a module.
//
// A child with routes and no fragment never reaches here: the launch refused
// it with wirehost.ErrRoutesUndescribed, so a nil fragment on a module is a
// module that serves no HTTP surface and has nothing to mount.
func moduleDocument(
	ctx context.Context, log *slog.Logger, instance ref.Ref, child *wirehost.Child, mount string,
) ([]byte, error) {
	fragment := child.OpenAPI()
	if mount == "" || fragment == nil {
		return nil, nil
	}

	document, err := fragment.Rebase(mount)
	if err != nil {
		return nil, fmt.Errorf("rebase the module's OpenAPI fragment under %s: %w", mount, err)
	}

	log.InfoContext(ctx, "the module's OpenAPI document is served",
		"resource", instance.String(), "path", httpapi.ModuleRootOpenAPIPath(mount), "paths", len(fragment.Paths()))

	return document, nil
}

// attempt is what one launch came back with, on the channel the start budget is
// raced against.
type attempt struct {
	child *wirehost.Child
	err   error
}

// launch runs the child under a context of its own and bounds the launch by the
// supervisor's start budget.
//
// The child has to outlive startCtx, which the supervisor cancels as soon as
// the start returns, so the process is given a context this instance's Close
// cancels instead. The budget still bounds the launch: wirehost.handshake reads
// the child's first line with no context at all, so a child that starts and
// then writes nothing would block the supervision loop until the process was
// killed. Cancelling the child's context kills it.
//
// No goroutine started here outlives the return. A watcher goroutine that
// cancelled on startCtx.Done would still be running when the supervisor
// cancels the budget after the start, and would kill the child it had just
// started.
func launch(startCtx context.Context, opts wirehost.Options) (*wirehost.Child, context.CancelFunc, error) {
	childCtx, cancel := context.WithCancel(context.WithoutCancel(startCtx))
	launched := make(chan attempt, 1)

	go func() {
		child, err := wirehost.Launch(childCtx, opts)
		launched <- attempt{child: child, err: err}
	}()

	select {
	case done := <-launched:
		if done.err != nil {
			cancel()

			return nil, nil, done.err
		}

		return done.child, cancel, nil
	case <-startCtx.Done():
		cancel()

		// The process is being killed and the launch is returning. Waiting
		// for it means no child is left behind.
		if done := <-launched; done.child != nil {
			_ = done.child.Close()
		}

		return nil, nil, fmt.Errorf("launch %s within the start budget: %w", opts.Path, startCtx.Err())
	}
}

// launchFailure gives a launch refusal the code that says what to do about it.
// Anything else is returned as it came back, which the supervisor treats as
// worth retrying.
func launchFailure(instance ref.Ref, path string, err error) error {
	switch {
	case errors.Is(err, wirehost.ErrDigestMismatch), errors.Is(err, wirehost.ErrDigestFormat):
		return pluginChecksum.New("the artefact does not match its recorded checksum").
			With(
				fault.StringParam("resource", instance.String()),
				fault.StringParam("path", path),
			).
			Wrap(err)
	case errors.Is(err, extension.ErrIncompatibleContract):
		return pluginContract.New("the child answered with a contract this host does not serve").
			With(
				fault.StringParam("resource", instance.String()),
				fault.StringParam("serves", string(wirerpc.Contract)),
			).
			Wrap(err)
	case errors.Is(err, wirehost.ErrManifestDisagrees):
		return manifestDisagrees.New("the child is not the module the install recorded").
			With(
				fault.StringParam("resource", instance.String()),
				fault.StringParam("path", path),
			).
			Wrap(err)
	case errors.Is(err, wirehost.ErrRoutesUndescribed):
		return pluginUndocumented.New("the module serves routes and ships no OpenAPI fragment").
			With(
				fault.StringParam("resource", instance.String()),
				fault.StringParam("path", path),
			).
			Wrap(err)
	}

	return err
}

// servesContract compares the declared contract major with the one this host
// serves, before any process is started. The operator wrote the major in the
// file, so a mismatch is reported without executing anything.
func servesContract(instance ref.Ref, declared int) error {
	major, _, _, err := wirerpc.Contract.Parse()
	if err != nil {
		return fmt.Errorf("read this host's plugin contract: %w", err)
	}

	if declared == major {
		return nil
	}

	return pluginContract.New("the instance declares a contract major this host does not serve").
		With(
			fault.StringParam("resource", instance.String()),
			fault.IntParam("declared", int64(declared)),
			fault.StringParam("serves", string(wirerpc.Contract)),
		).
		Wrap(extension.ErrIncompatibleContract)
}

// artefactName is the file name half of an installed extension: the type's own
// name, with the publisher namespace taken off. The artefact belongs to the
// type and not to the instance, so two instances of one type launch one file.
func artefactName(typeName string) string {
	cut := strings.LastIndex(typeName, extension.QualifiedSeparator)
	if cut < 0 {
		return typeName
	}

	return typeName[cut+1:]
}

// liveChildren is every module process the host launched and has not yet
// reaped. The supervisor cannot reach a target once it is closing, so a
// shutdown that is stuck finds the processes to kill here.
type liveChildren struct {
	mu  sync.Mutex
	set map[*wirehost.Child]struct{}
}

// add records child until it has been reaped.
func (l *liveChildren) add(child *wirehost.Child) {
	l.mu.Lock()
	if l.set == nil {
		l.set = map[*wirehost.Child]struct{}{}
	}
	l.set[child] = struct{}{}
	l.mu.Unlock()

	go func() {
		<-child.Exited()

		l.mu.Lock()
		delete(l.set, child)
		l.mu.Unlock()
	}()
}

// kill kills the group of every recorded child and returns what failed.
func (l *liveChildren) kill() []error {
	l.mu.Lock()
	children := make([]*wirehost.Child, 0, len(l.set))
	for child := range l.set {
		children = append(children, child)
	}
	l.mu.Unlock()

	var failed []error

	for _, child := range children {
		if err := child.Kill(); err != nil {
			failed = append(failed, err)
		}
	}

	return failed
}

// serviceChild is one instance running out of process: the child, and the
// cancel that ends the context bounding its process.
type serviceChild struct {
	child  *wirehost.Child
	cancel context.CancelFunc

	// ask is how long the child is given to stop on its own, the same budget
	// wirehost.Options.Stop got. Close arms the kill on it, so the wait for a
	// child that will not go is bounded here as well as inside the wire host.
	ask time.Duration

	// document is the module's OpenAPI fragment rebased under its mount, and
	// nil for a child that shipped none. It is set at launch and read by
	// the mount, so a restart that changes it changes what is served.
	document []byte

	// bundle is the browser half of a module that ships both, verified and
	// opened at launch, and nil for a module that draws nothing. It is served
	// under the same namespace as the routes, so one module is one instance.
	bundle *assetBundle

	// database is the schema the module's own database holds, as the module
	// reported it after migrating it in plugin.configure, and nil for a
	// module that reported none.
	database *wirerpc.DatabaseState

	// closing separates a session the host ended from one that ended on its
	// own. The supervisor fences a status report on the incarnation that
	// received the reporter, and a deliberate stop does not move to a new
	// incarnation, so without this a shutdown reports its own child as a
	// runtime failure.
	closing atomic.Bool
	// stopped is closed with closing set, and ends the memory watch.
	stopped chan struct{}

	// reason is why the host killed the child, set before the kill, and nil
	// for a child that ended on its own.
	reason atomic.Pointer[supervisor.Stop]

	log      *slog.Logger
	instance ref.Ref
}

// killFor kills the child and its group for a reason the status reports.
// When two watches kill one child, the first reason is the one reported.
func (s *serviceChild) killFor(stop supervisor.Stop) {
	s.reason.CompareAndSwap(nil, &stop)

	if err := s.child.Kill(); err != nil {
		s.log.Warn("the module could not be killed",
			"resource", s.instance.String(), "result", stop.Result.String(), "error", err.Error())
	}
}

// watch reports the session ending, so a child that dies is retried with the
// same backoff as an in-process resource.
//
// The report waits for the reap, so it carries the exit code or the signal.
// The wait is bounded: a grandchild that left the group and holds the child's
// standard error keeps the reap from finishing, and the stop is then reported
// without them.
func (s *serviceChild) watch(status supervisor.Reporter) {
	<-s.child.Conn().Done()

	if s.closing.Load() {
		return
	}

	select {
	case <-s.child.Exited():
	case <-time.After(s.ask):
	}

	if s.closing.Load() {
		return
	}

	if reason := s.reason.Load(); reason != nil {
		status.Recoverable(reason)

		return
	}

	stop := s.child.ExitStop()
	if stop.Result == "" {
		status.Recoverable(errChildEnded)

		return
	}

	stop.At = time.Now()
	status.Recoverable(fmt.Errorf("%w: %w", errChildEnded, &stop))
}

// Close asks the child to stop, and cancels the context that kills it if it
// does not go.
func (s *serviceChild) Close() error {
	if s.closing.CompareAndSwap(false, true) {
		close(s.stopped)
	}

	defer s.cancel()

	// The kill is armed before the ask, not deferred behind it. A child that
	// has stopped reading its pipe blocks the shutdown call inside the
	// operating system, and a cancel that waits for that call to return never
	// runs at all.
	armed := time.AfterFunc(s.ask, s.cancel)
	defer armed.Stop()

	if s.bundle != nil {
		_ = s.bundle.Close()
	}

	if err := s.child.Close(); err != nil {
		return fmt.Errorf("stop the child: %w", err)
	}

	return nil
}

// Call makes one control call on the session this child is serving, which is
// how the module's HTTP surface is reached: the host resolves the caller and
// decides the permission, and only then does a request cross this boundary.
//
// Only serviceChild has this method, so a module reachable over HTTP always
// runs in a process of its own. No in-process target satisfies moduleCaller.
func (s *serviceChild) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	answered, err := s.child.Conn().Call(ctx, wirerpc.Method(method), params)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}

	return answered, nil
}
