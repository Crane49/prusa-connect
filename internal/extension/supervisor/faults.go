package supervisor

import (
	"errors"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Advisory HTTP statuses for the codes below. A supervised call that refuses
// before reaching the resource is the host being unavailable, not the resource
// being wrong, so the class is 5xx throughout.
const (
	statusUnavailable = 503
	statusTimeout     = 504
	statusInternal    = 500
)

// The codes a supervised call returns. Each describes something the host did to
// a call: refused it, dropped it, timed out on it or recovered from it.
//
// They sit in core.internal because sdk/fault exposes no resource domain.
var (
	notReady = fault.Register(fault.DomainInternal(), "RESOURCE_NOT_READY", statusUnavailable, fault.Document(
		"The resource is not serving requests",
		"Check the resource state and retry once it is ready.",
		fault.RetryAllowed,
		fault.ShowDetail,
		"resource", "state",
	))

	breakerOpen = fault.Register(fault.DomainInternal(), "RESOURCE_BREAKER_OPEN", statusUnavailable, fault.Document(
		"The resource is failing and its circuit breaker is open",
		"Retry after the breaker's cooldown, and check the resource for the underlying failure.",
		fault.RetryAllowed,
		fault.ShowDetail,
		"resource", "op",
	))

	queueFull = fault.Register(fault.DomainInternal(), "RESOURCE_QUEUE_FULL", statusUnavailable, fault.Document(
		"The resource is too slow and its queue is full, so the item was dropped",
		"Retry. A resource that drops continuously is too slow for its offered load.",
		fault.RetryAllowed,
		fault.ShowDetail,
		"resource", "op",
	))

	callTimeout = fault.Register(fault.DomainInternal(), "RESOURCE_TIMEOUT", statusTimeout, fault.Document(
		"The resource did not answer within its budget",
		"Retry. A resource that times out repeatedly is unreachable rather than slow.",
		fault.RetryAllowed,
		fault.ShowDetail,
		"resource", "op", "timeout_ms",
	))

	panicked = fault.Register(fault.DomainInternal(), "RESOURCE_PANIC", statusInternal, fault.Document(
		"The resource panicked and was taken out of service",
		fault.NoAction,
		fault.RetryNever,
		fault.RedactDetail,
		"resource", "op",
	))

	startLimitHit = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_START_LIMIT_HIT",
		statusUnavailable,
		fault.Document(
			"The resource failed to start too many times and is no longer being retried",
			"Fix the underlying failure, then restart the resource.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "attempts",
		),
	)
)

// CodeNotReady is returned by a call routed to an instance that is not serving.
func CodeNotReady() fault.Code { return notReady.Code() }

// CodeBreakerOpen is returned by a call refused because the breaker is open.
func CodeBreakerOpen() fault.Code { return breakerOpen.Code() }

// CodeQueueFull is returned when a bounded queue dropped the offered item.
func CodeQueueFull() fault.Code { return queueFull.Code() }

// CodeTimeout is returned when the host's per-call budget expired.
func CodeTimeout() fault.Code { return callTimeout.Code() }

// CodePanic is returned when the host recovered a panic from the resource.
func CodePanic() fault.Code { return panicked.Code() }

// CodeStartLimitHit is the result of a retry ladder that ran out.
func CodeStartLimitHit() fault.Code { return startLimitHit.Code() }

// errStreamAbandoned is what a streaming span is finished with when its caller
// never released it and the host took the bulkhead slot back.
var errStreamAbandoned = errors.New("supervisor: streaming span abandoned")
