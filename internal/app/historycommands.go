package app

import (
	"context"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// The folded command record as the API reads it.
//
// It is the same reader the fact and sample queries go through. A provider
// never receives a principal and never resolves a permission, so the reader
// reduces a query to the origins its caller may read. One place does it, so
// every transport applies the same rule.
//
// A folded row needs a second narrowing. It carries what was asked of the
// machine, which anybody who may read the device may read. It also carries who
// authorized it, which takes view_audit. A fact query separates the two by
// class, because they live in different frames. A folded row separates them
// field by field.

var _ httpapi.CommandHistory = historyReader{}

// Commands answers one page of folded command rows the caller may read.
//
// The audit members are stripped per row and not per query, because the query
// may name several origins and audit sight is per origin. A caller holding it
// on printer A and not on printer B reads A's authorizations and not B's, out
// of one page. The fact query is stricter and fails closed across a plural
// query, because an audit fact is the audit record as a whole row. Here the
// audit content is three fields that can be left empty.
func (h historyReader) Commands(
	ctx context.Context, principal string, query httpapi.HistoryCommandQuery,
) (httpapi.HistoryCommandPage, error) {
	grants, err := h.permitted(ctx, principal, query.Origins)
	if err != nil {
		return httpapi.HistoryCommandPage{}, err
	}

	provider, err := h.feed.reader(verbCommands)
	if err != nil {
		return httpapi.HistoryCommandPage{}, err
	}

	// A caller who may read nothing reads no commands. An empty Refs means
	// every origin the provider holds. Only the start-up sweep may ask that.
	if len(grants) == 0 {
		return httpapi.HistoryCommandPage{Items: []httpapi.HistoryCommand{}}, nil
	}

	audit := make(map[event.Origin]bool, len(grants))
	refs := make([]event.Origin, 0, len(grants))

	for _, grant := range grants {
		refs = append(refs, grant.Origin)
		audit[grant.Origin] = grant.Audit
	}

	answer, err := provider.QueryCommands(ctx, historyapi.CommandQuery{
		Refs:     refs,
		IDs:      query.IDs,
		OpenOnly: query.Open,
		From:     query.From,
		To:       query.To,
		Limit:    query.Limit,
		Cursor:   query.Cursor,
	})
	if err != nil {
		return httpapi.HistoryCommandPage{}, historyQueryFault(err, "read the stored commands")
	}

	page := httpapi.HistoryCommandPage{
		Items:  make([]httpapi.HistoryCommand, 0, len(answer.Commands)),
		Cursor: answer.Cursor,
	}

	for _, one := range answer.Commands {
		// A row for an origin outside the permitted set is dropped, so a
		// provider that ignored Refs cannot leak one. The fact reader makes the
		// same check on the class.
		seen, permitted := audit[one.Ref]
		if !permitted {
			continue
		}

		page.Items = append(page.Items, renderCommand(one, seen))
	}

	return page, nil
}

// renderCommand puts one folded row on the wire, with the audit members present
// only for a caller who holds audit sight of its origin.
func renderCommand(one historyapi.Command, audit bool) httpapi.HistoryCommand {
	rendered := httpapi.HistoryCommand{
		ID:             one.ID,
		Origin:         one.Ref.String(),
		Verb:           one.Verb,
		Key:            one.Key,
		Requested:      one.Requested,
		Settled:        one.Settled,
		Status:         one.Status,
		OK:             one.OK,
		OutcomeUnknown: one.OutcomeUnknown,
		MachineReason:  one.MachineReason,
		FaultCode:      one.FaultCode,
		ElapsedMS:      one.ElapsedMS,
	}

	if audit {
		rendered.Authorized = one.Authorized
		rendered.AuthType = one.AuthType
		rendered.AuthID = one.AuthID
	}

	return rendered
}
