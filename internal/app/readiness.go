package app

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/discovery"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// readinessInterval is how often the verdict is checked for a change to log.
// It is a poll and not a callback because the supervisor's status hook cannot
// read the store. A reconcile holds the store's lock across Host.stop, which
// waits on the goroutine the hook runs on, so a hook that took that lock would
// deadlock all three. The declared inventory the verdict needs is behind the
// same lock.
//
// The endpoint does not poll. It computes the verdict on the request goroutine,
// which may take the store's lock like any other read.
const readinessInterval = time.Second

// readiness is one verdict: the process is ready while every required
// instance is serving. An optional instance that will not start never makes
// it unready.
//
// It is served at GET /api/v1/readiness and logged whenever it changes. The
// endpoint is inside the sealed API surface because it is part of the contract:
// a load balancer routes on its status code and an operator reads its body, and
// everything this server answers is published in its OpenAPI document.
type readiness struct {
	store *resource.Store
	host  *resourcehost.Host
	// app is the state of the web app, one of the three answers httpapi names.
	// It is computed once, at startup, because it cannot change while the
	// process runs: the build either carries the app or does not, and
	// core.frontend.enabled is read from the file at load.
	app string
	// announcer is what this server tells the local network about itself. It is
	// asked at request time, because a responder this machine refused says so
	// only after the listeners are bound. A nil announcer reads as off.
	announcer *discovery.Announcer
}

var _ httpapi.Readiness = readiness{}

// verdict is the aggregate and what is blocking it.
type verdict struct {
	// Ready is false while a required instance is not serving.
	Ready bool
	// Blocked names the required instances that are not serving, in the
	// document's order.
	Blocked []string
	// Serving counts the instances calls are being routed to.
	Serving int
	// Declared counts every instance the merged configuration declares.
	Declared int
}

// Equal reports whether two verdicts say the same thing, so the watcher logs
// only a change.
func (v verdict) Equal(other verdict) bool {
	return v.Ready == other.Ready &&
		v.Serving == other.Serving &&
		v.Declared == other.Declared &&
		slices.Equal(v.Blocked, other.Blocked)
}

// verdict reads the declared configuration and the supervision plane's view of
// each instance. It reads both at call time and caches nothing, because a
// cached readiness answer describes a moment that has passed.
func (r readiness) verdict() verdict {
	return r.verdictOf(r.store.Document())
}

// verdictOf is the aggregate over one reading of the configuration. The
// document is passed in and not read again, so an answer that carries both the
// counts and the instances behind them describes one moment. With two reads, a
// reconcile landing between them would produce a report whose declared count
// and instance list disagree.
func (r readiness) verdictOf(document resource.Document) verdict {
	current := verdict{Ready: true, Declared: len(document.Instances)}
	trial := trialExtensionSet()
	seen := make(map[string]bool, len(document.Instances))

	for _, declared := range document.Instances {
		seen[declared.Ref.String()] = true
		serving := false

		if observed, ok := r.host.Status(declared.Ref); ok {
			serving = observed.Serving
		}

		if serving {
			current.Serving++

			continue
		}

		if declared.Spec.Required || trial[declared.Ref.String()] {
			current.Ready = false
			current.Blocked = append(current.Blocked, declared.Ref.String())
		}
	}
	for required := range trial {
		if !seen[required] {
			current.Ready = false
			current.Blocked = append(current.Blocked, required)
		}
	}
	if len(trial) > 0 {
		slices.Sort(current.Blocked)
	}

	return current
}

func trialExtensionSet() map[string]bool {
	required := make(map[string]bool)
	for name := range strings.SplitSeq(os.Getenv(upgradeTrialExtensionsEnvironment), ",") {
		if name != "" {
			required[name] = true
		}
	}

	return required
}

func trialRequired(declared resource.Declaration) bool {
	return declared.Spec.Required || trialExtensionSet()[declared.Ref.String()]
}

// state renders one instance for a log line, falling back to what the
// declaration alone says when the plane is not supervising it.
func (r readiness) state(declared resource.Declaration) string {
	if observed, ok := r.host.Status(declared.Ref); ok {
		return observed.State
	}

	if !declared.Spec.Enabled || r.store.Held(declared) {
		return condition.StateDisabled().String()
	}

	return condition.StatePending().String()
}

// Ready is the verdict as the API surface serves it. It is computed per request
// and not read from the watcher's last poll, so a load balancer never routes
// on a stale verdict.
func (r readiness) Ready(ctx context.Context, principal string) httpapi.ReadinessReport {
	document := r.store.Document()
	current := r.verdictOf(document)

	// Both lists are allocated and not left nil. A nil list encodes as
	// `"blocked": null`, and a client iterating the field would have to
	// special-case it.
	report := httpapi.ReadinessReport{
		App:       r.app,
		Discovery: r.announcer.State(),
		Ready:     current.Ready,
		Serving:   current.Serving,
		Declared:  current.Declared,
		Blocked:   []string{},
		Instances: []httpapi.ReadinessInstance{},
	}

	// The verdict and the counts describe the process, and a probe needs no
	// more. Everything below this line names a resource, so it is filtered to
	// what the caller may see, as /api/v1/resources is.
	visible := r.visibleTo(ctx, principal)
	if visible == nil {
		return report
	}

	for _, name := range current.Blocked {
		if _, ok := visible[name]; ok {
			report.Blocked = append(report.Blocked, name)
		}
	}

	for _, declared := range document.Instances {
		if _, ok := visible[declared.Ref.String()]; !ok {
			continue
		}

		serving := false
		if observed, ok := r.host.Status(declared.Ref); ok {
			serving = observed.Serving
		}

		report.Instances = append(report.Instances, httpapi.ReadinessInstance{
			Resource: declared.Ref.String(),
			State:    r.state(declared),
			Required: trialRequired(declared),
			Serving:  serving,
		})
	}

	return report
}

// visibleTo is the set of instances this caller may see, keyed the way the
// report names them. It returns nil for a caller who is not identified, whose
// grants cannot be resolved, or whose subject the graph cannot key on, so every
// one of those answers the same report a probe gets.
//
// It goes through the resource store's own list and does not ask the graph
// directly, so this endpoint names the same instances GET /api/v1/resources
// returns.
func (r readiness) visibleTo(ctx context.Context, principal string) map[string]struct{} {
	if principal == "" {
		return nil
	}

	listing, err := r.store.List(ctx, principal, "")
	if err != nil {
		return nil
	}

	visible := make(map[string]struct{}, len(listing.Items))
	for _, item := range listing.Items {
		visible[item.Kind+":"+item.Name] = struct{}{}
	}

	return visible
}

// watch logs the verdict whenever it changes, and returns when ctx ends. It
// tells an operator a required instance went away even when nobody polled the
// endpoint.
func (r readiness) watch(ctx context.Context, logger *slog.Logger) error {
	ticker := time.NewTicker(readinessInterval)
	defer ticker.Stop()

	var last verdict

	first := true

	for {
		current := r.verdict()

		if first || !current.Equal(last) {
			r.log(ctx, logger, current)

			last, first = current, false
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r readiness) log(ctx context.Context, logger *slog.Logger, current verdict) {
	attrs := []any{
		"ready", current.Ready,
		"serving", current.Serving,
		"declared", current.Declared,
		"instances", r.states(),
	}

	if current.Ready {
		logger.InfoContext(ctx, "readiness", attrs...)

		return
	}

	logger.WarnContext(ctx, "readiness", append(attrs, "blocked", current.Blocked)...)
}

// states renders every declared instance and where it is, so the line that says
// the process is not ready also says which resource to look at.
func (r readiness) states() []string {
	document := r.store.Document()
	out := make([]string, 0, len(document.Instances))

	for _, declared := range document.Instances {
		required := "optional"
		if declared.Spec.Required {
			required = "required"
		}

		out = append(out, declared.Ref.String()+"="+r.state(declared)+"/"+required)
	}

	return out
}
