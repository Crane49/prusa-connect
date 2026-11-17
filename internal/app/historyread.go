package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// historyReader is the stored past as the API reads it: the configured provider
// behind the same visibility the event stream subscribes through.
//
// It lives in the composition root, as visibility does. A history provider
// never receives a principal and never resolves a permission; sdk/historyapi
// states that on both query types. So something has to reduce a query to the
// origins its caller may read. internal/authz may not import a transport and a
// history adapter may not import a policy, so the code that builds both does
// it.
type historyReader struct {
	// feed resolves which serving instance answers a verb, per query. It is not
	// a provider captured at boot. Several instances may store the same
	// writes, and a read picks one by capability. Where two claim the same
	// verb the read is refused.
	feed historyReaders
	// seen is the same value the hub authorizes a subscription with, so the
	// stream and the stored past apply one rule.
	seen visibility
}

// historyReaders is the resolution the read surface needs. It is an interface
// so a test of the query path can run against one provider without a
// supervision plane.
type historyReaders interface {
	reader(verb string) (historyapi.Provider, error)
}

var _ httpapi.HistoryStore = historyReader{}

// readableClasses is what a fact query may answer with when the caller holds no
// audit sight, and auditedClasses is the same set with the audit class in it.
//
// They are written out and not derived. "Every class except audit", computed
// from a list somewhere else, would widen when that list grows. Telemetry is
// in neither: it reaches AppendSamples and is never a fact.
var (
	readableClasses = []event.Class{event.ClassState, event.ClassCommand}
	auditedClasses  = []event.Class{event.ClassState, event.ClassCommand, event.ClassAudit}
)

// Facts answers one page of stored events the caller may read.
//
// An event a caller may not see on the stream must be unreadable from history.
// So the class restriction here is the same predicate the stream applies in
// Subscription.deliver. Without it a query naming
// com.prusa.connect.device.command.authorized by type would show who sent
// which command to any caller holding read on the printer, because a fact
// query reduces by origin alone and the fact path stores every audit frame.
//
// It fails closed across a plural query. Classes is one list and Refs is many
// origins, so the audit class is included only when the caller holds audit
// sight of every origin the query names. A caller holding it on printer A and
// not on printer B reads no audit facts from a query naming both, and queries
// A alone to read A's. The reader does not split the query itself, because
// the provider mints one cursor per query and two pages cannot share one.
func (h historyReader) Facts(
	ctx context.Context, principal string, query httpapi.HistoryFactQuery,
) (httpapi.HistoryFactPage, error) {
	grants, err := h.permitted(ctx, principal, query.Origins)
	if err != nil {
		return httpapi.HistoryFactPage{}, err
	}

	provider, err := h.feed.reader(verbFacts)
	if err != nil {
		return httpapi.HistoryFactPage{}, err
	}

	refs := make([]event.Origin, 0, len(grants))

	// Fail closed on the empty set as well: a caller who may read nothing may
	// not read audit.
	classes := readableClasses
	if len(grants) > 0 {
		classes = auditedClasses
	}

	for _, grant := range grants {
		refs = append(refs, grant.Origin)

		if !grant.Audit {
			classes = readableClasses
		}
	}

	// What the caller asked for narrows what their grants carry and never
	// widens it. A class they named and may not read drops out here.
	asked := intersectClasses(classes, query.Classes)
	if len(asked) == 0 {
		return httpapi.HistoryFactPage{Items: []httpapi.HistoryFact{}}, nil
	}

	classes = asked

	answer, err := provider.QueryFacts(ctx, historyapi.FactQuery{
		Refs:    refs,
		Types:   query.Types,
		Classes: classes,
		From:    query.From,
		To:      query.To,
		Limit:   query.Limit,
		Cursor:  query.Cursor,
	})
	if err != nil {
		return httpapi.HistoryFactPage{}, historyQueryFault(err, "read the stored events")
	}

	page := httpapi.HistoryFactPage{
		Items:      make([]httpapi.HistoryFact, 0, len(answer.Facts)),
		Cursor:     answer.Cursor,
		Incomplete: answer.Incomplete,
	}

	for _, one := range answer.Facts {
		// A row outside the permitted classes is dropped, so a provider that
		// ignored the restriction cannot leak one. A row carrying no class at
		// all is dropped too.
		if !slices.Contains(classes, one.Class) {
			continue
		}

		page.Items = append(page.Items, httpapi.HistoryFact{
			At:       one.At,
			Origin:   one.Ref.String(),
			Type:     one.Type,
			Sequence: one.Sequence,
			Epoch:    one.Epoch,
			Payload:  one.Payload,
		})
	}

	return page, nil
}

// intersectClasses reduces what the caller may read to what they asked for. An
// empty ask is the whole permitted set. An ask that overlaps it nowhere gives
// an empty page: a caller who asked only for audit rows on a device they hold
// no audit sight of gets no rows, and not the device's state rows.
func intersectClasses(permitted, asked []event.Class) []event.Class {
	if len(asked) == 0 {
		return permitted
	}

	out := make([]event.Class, 0, len(asked))

	for _, one := range asked {
		if slices.Contains(permitted, one) {
			out = append(out, one)
		}
	}

	return out
}

// Samples answers one page of stored measurements the caller may read.
func (h historyReader) Samples(
	ctx context.Context, principal string, query httpapi.HistorySampleQuery,
) (httpapi.HistorySamplePage, error) {
	grants, err := h.permitted(ctx, principal, query.Origins)
	if err != nil {
		return httpapi.HistorySamplePage{}, err
	}

	provider, err := h.feed.reader(verbSamples)
	if err != nil {
		return httpapi.HistorySamplePage{}, err
	}

	refs := make([]event.Origin, 0, len(grants))
	for _, grant := range grants {
		refs = append(refs, grant.Origin)
	}

	answer, err := provider.QuerySamples(ctx, historyapi.SampleQuery{
		Refs:    refs,
		Metrics: query.Metrics,
		From:    query.From,
		To:      query.To,
		Limit:   query.Limit,
		Cursor:  query.Cursor,
	})
	if err != nil {
		return httpapi.HistorySamplePage{}, historyQueryFault(err, "read the stored measurements")
	}

	page := httpapi.HistorySamplePage{
		Items:      make([]httpapi.HistorySample, 0, len(answer.Samples)),
		Cursor:     answer.Cursor,
		Incomplete: answer.Incomplete,
	}

	// A reading is a number or a word. The rendering carries the one it is and
	// leaves the other member out. Sending both, with a zero in the member the
	// reading does not hold, would tell a client that /state was 0 at every
	// instant it was IDLE.
	//
	// A row of a kind this build cannot render carries neither member, so it
	// is not turned into a measurement nobody reported.
	for _, one := range answer.Samples {
		item := httpapi.HistorySample{At: one.At, Origin: one.Ref.String(), Metric: one.Metric}

		switch one.Kind {
		case historyapi.SampleText:
			text := one.Text
			item.Text = &text
		case historyapi.SampleNumber:
			value := one.Value
			item.Value = &value
		}

		page.Items = append(page.Items, item)
	}

	return page, nil
}

// permitted reduces a query to the origins this principal may read, and refuses
// one that named an origin they may not.
//
// The set is not a filter applied afterwards. It reaches the provider as the
// query's Refs, which the reference store puts inside the statement ahead of
// the limit, so a page counts only rows the caller may read. A listing that
// fetched and then filtered would leak through its own paging.
//
// A caller who named nothing is given what they may read. A caller who named
// an origin they may not read is refused whole, and the refusal names none of
// them. The event stream applies the same rule.
//
// It answers grants and not origins, because each grant also says whether the
// caller holds audit sight. The stream reads the same answer, so the live
// path and this one apply one rule.
func (h historyReader) permitted(
	ctx context.Context, principal string, asked []event.Origin,
) ([]stream.Grant, error) {
	answer, err := h.seen.VisibleOrigins(ctx, principal)
	if err != nil {
		return nil, fmt.Errorf("read what %s may observe: %w", principal, err)
	}

	// The generation each sight began at bounds a replay on the event stream.
	// It does not apply to a query over the past, which reads what is stored.
	if len(asked) == 0 {
		return answer.Origins, nil
	}

	held := make([]stream.Grant, 0, len(asked))

	for _, one := range asked {
		found := slices.IndexFunc(answer.Origins, func(grant stream.Grant) bool {
			return grant.Origin == one
		})
		if found < 0 {
			return nil, fault.New(authz.CodeAccessDenied(),
				"this request named an origin the caller may not observe").With(
				fault.StringParam("object", "history"),
				fault.StringParam("action", "read"),
			)
		}

		held = append(held, answer.Origins[found])
	}

	return held, nil
}

// historyQueryFault renders what a provider refused.
//
// ErrUnsupported is a capability the configured provider does not carry. The
// caller gets the same answer as from a deployment with no provider at all:
// the feature is absent. The contract allows a provider that stores facts and
// refuses samples, and a 422 there would report a well-formed request as a
// malformed one.
//
// ErrQueryRefused is the contract's own refusal and reaches a caller as a 422.
// The provider serves this part of the contract and will not answer this
// window or shape truthfully: a cursor it did not mint, a bucket it did not
// compute, or a window it has expired.
//
// A fault reaches the caller as itself. The supervisor raises one when it
// refuses the call before the provider sees it: the provider is not serving, or
// its breaker is open. Both are a 503 the caller may retry.
//
// Anything else is this server failing. The detail goes to the log and not to
// the response.
func historyQueryFault(err error, doing string) error {
	var refusal *historyapi.QueryRefusedError

	switch {
	case errors.Is(err, historyapi.ErrUnsupported):
		return httpfault.HistoryNotConfigured()
	case errors.As(err, &refusal):
		return httpfault.HistoryQueryRefused(refusal.Reason, refusal.Coarsest.String())
	case errors.Is(err, historyapi.ErrQueryRefused):
		return httpfault.HistoryQueryRefused(
			"the provider will not answer a query in this shape", err.Error())
	}

	if raised, ok := errors.AsType[*fault.Fault](err); ok {
		return raised
	}

	return httpfault.Unexpected(doing).Wrap(err)
}
