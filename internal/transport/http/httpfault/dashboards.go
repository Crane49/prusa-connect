package httpfault

import (
	"net/http"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The param name the dashboard limit interpolates. A dashboard is named by
// resource, as dashboard:<id>, and its current revision by version, the keys
// every other refusal already uses for those facts.
const paramNameLimitDashboards = "limit_dashboards"

// The codes the dashboard routes answer with. A dashboard the caller may not
// see answers not-found and not denied, so a listing and a read never tell a
// person that a dashboard they may not see exists.
var (
	dashboardNotFound = fault.Register(
		fault.DomainResource(), "DASHBOARD_NOT_FOUND", http.StatusNotFound, fault.Document(
			"There is no dashboard with this id that you may see",
			"Open the list of dashboards and pick one from it. A deleted dashboard's id is never "+
				"given to another one, so an old link does not open a different dashboard.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource,
		))

	dashboardChanged = fault.Register(
		fault.DomainResource(), "DASHBOARD_CHANGED", http.StatusConflict, fault.Document(
			"Somebody saved this dashboard after you read it",
			"Read the dashboard again, make the change against the revision it answers, and send it "+
				"again. The server refuses the change rather than writing over the other person's.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNameResource, paramNameVersion,
		))

	dashboardsFull = fault.Register(
		fault.DomainResource(), "DASHBOARDS_FULL", http.StatusConflict, fault.Document(
			"This server already holds as many dashboards as it keeps",
			"Delete a dashboard nobody uses, then make the new one. The server keeps at most "+
				"limit_dashboards dashboards.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLimitDashboards,
		))

	dashboardInvalid = fault.Register(
		fault.DomainValidation(), "DASHBOARD_INVALID", http.StatusUnprocessableEntity, fault.Document(
			"The dashboard breaks one of the rules a dashboard is held to",
			"Correct the member named by location as the reason says, and send the dashboard again.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLocation, paramNameReason,
		))
)

// DashboardNotFound reports a dashboard that is not there, or that the caller
// may not see.
func DashboardNotFound(id int64) *fault.Fault {
	return dashboardNotFound.New("no such dashboard").With(
		fault.StringParam(paramNameResource, dashboardResource(id)),
	)
}

// DashboardChanged reports a change made against an older revision, naming
// the revision the dashboard is at now.
func DashboardChanged(id, revision int64) *fault.Fault {
	return dashboardChanged.New("the dashboard was saved after it was read").With(
		fault.StringParam(paramNameResource, dashboardResource(id)),
		fault.StringParam(paramNameVersion, strconv.FormatInt(revision, 10)),
	)
}

// dashboardResource is a dashboard as the resource param names it.
func dashboardResource(id int64) string { return "dashboard:" + strconv.FormatInt(id, 10) }

// DashboardsFull reports a new dashboard past the limit the server keeps.
func DashboardsFull(limit int) *fault.Fault {
	return dashboardsFull.New("the server keeps no more dashboards").With(
		fault.IntParam(paramNameLimitDashboards, int64(limit)),
	)
}

// DashboardInvalid reports a member that breaks a rule. location is the
// member, such as body.cards[2].w, and reason says which rule and its limit.
func DashboardInvalid(location, reason string) *fault.Fault {
	return dashboardInvalid.New(reason).With(
		fault.StringParam(paramNameLocation, location),
		fault.StringParam(paramNameReason, reason),
	)
}

// CodeDashboardNotFound names a dashboard that is not there or not visible.
func CodeDashboardNotFound() fault.Code { return dashboardNotFound.Code() }

// CodeDashboardChanged names a change made against an older revision.
func CodeDashboardChanged() fault.Code { return dashboardChanged.Code() }

// CodeDashboardsFull names a server that holds as many dashboards as it keeps.
func CodeDashboardsFull() fault.Code { return dashboardsFull.Code() }

// CodeDashboardInvalid names a dashboard that breaks a rule.
func CodeDashboardInvalid() fault.Code { return dashboardInvalid.Code() }
