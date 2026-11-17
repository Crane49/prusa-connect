package resourcehost

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// A module the operator approved with check_absent_people may ask access.check
// about a person it is not serving. The subject must be one person, and a
// person this server holds. The graph is read for every question. Every
// question writes one audit line, and each instance has a rate limit.

// People is the list of people this server keeps.
type People interface {
	// Kept reports whether the server keeps a list of people. A deployment
	// with an OpenID Connect login keeps none, and neither does one in open
	// mode.
	Kept() bool
	// Holds answers whether the principal is a person the list holds. It
	// answers false for a principal that is not one person.
	Holds(ctx context.Context, principal string) (bool, error)
	// List answers every person the list holds, with the name a person reads.
	// It answers nobody while Kept is false.
	List(ctx context.Context) ([]accessapi.Person, error)
}

// PersonID returns the account id of a principal that names one person,
// user:<id>, and false for a set of people or anything else. Every check in
// this file that asks whether a subject is a person calls it.
func PersonID(principal string) (string, bool) {
	subject, err := authz.ParseSubject(principal)
	if err != nil || subject.Type() != authz.TypeUser || subject.IsUserset() {
		return "", false
	}

	return subject.ID(), true
}

// The bound on questions about people an instance is not serving: a token
// bucket that holds 600 questions and refills 10 a second. Every question on
// that path spends a token, and a refused question does too, so the load on
// the database and the growth of the log are limited by the refill rate.
const (
	absentBurst     = 600
	absentPerSecond = 10
)

// maxLoggedBytes caps each field an audit line carries, so no caller can make
// a line long.
const maxLoggedBytes = 256

// quietInterval is how often a warning that repeats is written again. The
// first of a run is written at once, and the next one after this interval says
// how many were left out in between.
const quietInterval = time.Minute

// quietLog holds the warnings that a module can make the host repeat, so each
// is written at most once an interval per instance.
type quietLog struct {
	mu   sync.Mutex
	held map[quietKey]*quietRun
}

// quietKey is one kind of warning about one instance.
type quietKey struct {
	instance ref.Ref
	what     string
}

// quietRun is when a warning was last written and how many were left out
// since.
type quietRun struct {
	at     time.Time
	missed int
}

// write reports whether this warning should be written, and how many like it
// were left out before it.
func (q *quietLog) write(key quietKey, now time.Time) (bool, int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.held == nil {
		q.held = map[quietKey]*quietRun{}
	}

	run, ok := q.held[key]
	if !ok {
		q.held[key] = &quietRun{at: now}

		return true, 0
	}

	if now.Sub(run.at) < quietInterval {
		run.missed++

		return false, 0
	}

	missed := run.missed
	run.at, run.missed = now, 0

	return true, missed
}

// absentBudgets holds each instance's bucket.
type absentBudgets struct {
	mu   sync.Mutex
	held map[ref.Ref]*absentBucket
}

// absentBucket is one instance's tokens and when they were last counted.
type absentBucket struct {
	tokens float64
	at     time.Time
}

// take spends one question for the instance, and reports whether it may go on.
func (b *absentBudgets) take(instance ref.Ref, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.held == nil {
		b.held = map[ref.Ref]*absentBucket{}
	}

	held, ok := b.held[instance]
	if !ok {
		held = &absentBucket{tokens: absentBurst, at: now}
		b.held[instance] = held
	}

	held.tokens = min(absentBurst, held.tokens+now.Sub(held.at).Seconds()*absentPerSecond)
	held.at = now

	if held.tokens < 1 {
		return false
	}

	held.tokens--

	return true
}

// absentAccess is what vouchedAccess needs to answer about a person. allowed is
// the permission, and a zero value is an instance without it.
type absentAccess struct {
	allowed bool
	people  People
	budget  *absentBudgets
	quiet   *quietLog
	log     *slog.Logger
}

// keeps reports whether this server keeps a list of people.
func (a absentAccess) keeps() bool { return a.people != nil && a.people.Kept() }

// checkServed answers a question about a person the instance is serving, or
// served since it started. The graph can keep a deleted person's grants, so a
// yes counts only for a person the list still holds.
func (v vouchedAccess) checkServed(
	ctx context.Context, params accessapi.CheckParams,
) (accessapi.Decision, error) {
	decision, err := v.inner.Check(ctx, params)
	if err != nil || !decision.Allowed || !v.absent.keeps() {
		return decision, err
	}

	if _, isPerson := PersonID(params.Subject); !isPerson {
		return decision, nil
	}

	held, err := v.absent.people.Holds(ctx, params.Subject)
	if err != nil {
		v.warn(ctx, "served lookup", "a module asked about a person it served, and the person could not be looked up",
			"subject", capped(params.Subject))

		return accessapi.Decision{}, fmt.Errorf("look up %s: %w", capped(params.Subject), err)
	}

	decision.Allowed = held

	return decision, nil
}

// checkAbsent answers one question about a person the instance is not serving.
//
// An instance without the permission is refused first. Every other question
// then spends a token, and a refused question does too, so the bucket bounds
// the audit lines. The refusals that need neither the graph nor the database
// come next. The graph is asked before the person is looked up, so an action
// outside the instance's list is ErrDenied for a removed person too.
func (v vouchedAccess) checkAbsent(
	ctx context.Context, params accessapi.CheckParams,
) (accessapi.Decision, error) {
	if !v.absent.allowed {
		return accessapi.Decision{}, fmt.Errorf("%w: %s has not served %s since it started",
			accessapi.ErrUnknownSubject, v.instance, capped(params.Subject))
	}

	if !v.absent.budget.take(v.instance, time.Now()) {
		v.warn(ctx, "bound", "a module passed its bound on questions about people it is not serving",
			"burst", absentBurst, "per_second", absentPerSecond)

		return accessapi.Decision{}, fmt.Errorf("%w: %s may ask %d a second",
			accessapi.ErrTooManyQuestions, v.instance, absentPerSecond)
	}

	if _, isPerson := PersonID(params.Subject); !isPerson {
		v.audit(ctx, params, "refused: not a person")

		return accessapi.Decision{}, fmt.Errorf("%w: %s is not a person, and %s has not served it",
			accessapi.ErrUnknownSubject, capped(params.Subject), v.instance)
	}

	if !v.absent.keeps() {
		v.audit(ctx, params, "refused: this server keeps no list of people")

		return accessapi.Decision{}, fmt.Errorf("%w: this server keeps no list of people",
			accessapi.ErrUnknownSubject)
	}

	decision, err := v.inner.Check(ctx, params)
	if err != nil {
		v.audit(ctx, params, "refused")

		return accessapi.Decision{}, err
	}

	held, err := v.absent.people.Holds(ctx, params.Subject)
	if err != nil {
		v.audit(ctx, params, "failed: the person could not be looked up")

		return accessapi.Decision{}, fmt.Errorf("look up %s: %w", capped(params.Subject), err)
	}

	decision.Allowed = decision.Allowed && held

	answer := "no"
	if decision.Allowed {
		answer = "yes"
	}

	v.audit(ctx, params, answer)

	return decision, nil
}

// warn writes a warning the module can make the host repeat, at most once a
// quietInterval per instance, with the count of the ones left out.
func (v vouchedAccess) warn(ctx context.Context, what, message string, attrs ...any) {
	write, missed := v.absent.quiet.write(quietKey{instance: v.instance, what: what}, time.Now())
	if !write {
		return
	}

	attrs = append(attrs, "resource", v.instance.String(), "left_out", missed)
	v.absent.log.WarnContext(ctx, message, attrs...)
}

// audit writes one line for one question about a person the instance is not
// serving. Every field is capped, because a caller inside this process can
// reach the host without the size limits the wire applies to a module.
func (v vouchedAccess) audit(ctx context.Context, params accessapi.CheckParams, answer string) {
	v.absent.log.InfoContext(ctx, "a module asked about a person it is not serving",
		"resource", v.instance.String(), "subject", capped(params.Subject), "action", capped(params.Action),
		"object", capped(params.ObjectType+":"+params.ObjectID), "answer", answer)
}

// capped cuts a value a module sent to maxLoggedBytes, on a rune boundary.
func capped(value string) string { return machinetext.Clip(value, maxLoggedBytes) }
