//go:build !nodashboards

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/dashboard"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/dashboards"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The level a caller holds on the dashboard kind, below the first of the
// three visibilities: they see no dashboard at all.
const noDashboardLevel = -1

// dashboardGraph is what the dashboards ask of the authorization graph.
type dashboardGraph interface {
	Can(ctx context.Context, subject authz.Subject, action authz.Action, object authz.Object) (bool, error)
	Require(ctx context.Context, subject authz.Subject, action authz.Action, object authz.Object) error
}

// dashboardPublisher is where a change of a dashboard is announced.
type dashboardPublisher interface {
	Publish(pub stream.Publication) (event.Position, error)
}

// dashboardStore is the dashboards the home page draws. The rows are in the
// host database. The organization roles on the dashboard kind decide who sees
// which: view_resource sees those visible to everyone, operate_resource those
// visible to operators, configure_resource every one. Nothing is kept in
// memory between requests.
type dashboardStore struct {
	graph dashboardGraph
	rows  *dashboards.Store
	hub   dashboardPublisher
	log   *slog.Logger
}

var _ httpapi.DashboardStore = (*dashboardStore)(nil)

// dashboardsBuilt says whether this build carries the dashboards, so the
// first-run wizard offers core.dashboards.enabled only where it does
// something.
const dashboardsBuilt = true

// servedDashboards is the dashboards this server serves: the store over the
// host database, or none when core.dashboards.enabled is off. The second answer
// is true when they are off, and the start says so in one line.
func servedDashboards(
	ctx context.Context, core config.Core, host hostStore, hub dashboardPublisher, logger *slog.Logger,
) (httpapi.DashboardStore, bool, error) {
	if !core.Dashboards.On() {
		logger.InfoContext(ctx, "dashboards are off", "key", "core.dashboards.enabled")

		return nil, true, nil
	}

	store, err := newDashboardStore(host, hub, logger)
	if err != nil {
		return nil, false, err
	}

	return store, false, nil
}

// newDashboardStore binds the dashboards to the host database.
func newDashboardStore(host hostStore, hub dashboardPublisher, logger *slog.Logger) (*dashboardStore, error) {
	rows, err := dashboards.New(host.db, host.engine, nil)
	if err != nil {
		return nil, fmt.Errorf("open the dashboards: %w", err)
	}

	return &dashboardStore{graph: host.graph, rows: rows, hub: hub, log: logger}, nil
}

// ListDashboards answers every dashboard the caller sees. A caller who sees
// none is answered an empty listing, never a refusal.
func (s *dashboardStore) ListDashboards(ctx context.Context, principal string) (httpapi.DashboardListing, error) {
	listing := httpapi.DashboardListing{Items: []httpapi.DashboardRow{}}

	_, level, err := s.levelOf(ctx, principal)
	if err != nil || level == noDashboardLevel {
		return listing, err
	}

	rows, held, err := s.rows.List(ctx, level)
	if err != nil {
		return httpapi.DashboardListing{}, s.internal(ctx, "list the dashboards", err)
	}

	for _, row := range rows {
		listing.Items = append(listing.Items, httpapi.DashboardRow{
			ID: row.ID, Name: row.Name,
			Visibility: string(row.Visibility), IsDefault: row.IsDefault, Cards: row.Cards,
			Changed: row.Changed, ChangedBy: row.ChangedBy,
		})
	}

	listing.Total = len(listing.Items)
	listing.DefaultID = held

	return listing, nil
}

// GetDashboard answers one dashboard the caller sees.
func (s *dashboardStore) GetDashboard(ctx context.Context, principal string, id int64) (httpapi.Dashboard, error) {
	_, level, err := s.levelOf(ctx, principal)
	if err != nil {
		return httpapi.Dashboard{}, err
	}

	if level == noDashboardLevel {
		return httpapi.Dashboard{}, httpfault.DashboardNotFound(id)
	}

	board, err := s.rows.Get(ctx, id, level)
	if err != nil {
		return httpapi.Dashboard{}, s.refusal(ctx, id, err)
	}

	return wireDashboard(board)
}

// CreateDashboard writes a new dashboard. It takes operate_resource, and the
// visibility it is made with must be one the caller sees.
func (s *dashboardStore) CreateDashboard(
	ctx context.Context, principal string, draft httpapi.DashboardDraft,
) (httpapi.Dashboard, error) {
	who, level, err := s.mayWrite(ctx, principal)
	if err != nil {
		return httpapi.Dashboard{}, err
	}

	made, err := domainDraft(draft)
	if err != nil {
		return httpapi.Dashboard{}, err
	}

	board, err := dashboard.New(made)
	if err != nil {
		return httpapi.Dashboard{}, s.refusal(ctx, 0, err)
	}

	if err = s.mayShow(ctx, who, level, board.Visibility); err != nil {
		return httpapi.Dashboard{}, err
	}

	stored, err := s.rows.Create(ctx, board, principal)
	if err != nil {
		return httpapi.Dashboard{}, s.refusal(ctx, 0, err)
	}

	s.announce(ctx, stored.ID, stored.Visibility, "created")

	return wireDashboard(stored)
}

// ReplaceDashboard writes a draft over one dashboard the caller sees, against
// the revision the draft names.
func (s *dashboardStore) ReplaceDashboard(
	ctx context.Context, principal string, id int64, draft httpapi.DashboardDraft,
) (httpapi.Dashboard, error) {
	who, level, err := s.mayWrite(ctx, principal)
	if err != nil {
		return httpapi.Dashboard{}, err
	}

	if draft.Revision == nil {
		return httpapi.Dashboard{}, httpfault.DashboardInvalid("body.revision",
			"is required for a change: send the revision the dashboard was read at")
	}

	change, err := domainDraft(draft)
	if err != nil {
		return httpapi.Dashboard{}, err
	}

	base, err := s.rows.Get(ctx, id, level)
	if err != nil {
		return httpapi.Dashboard{}, s.refusal(ctx, id, err)
	}

	board, err := dashboard.Apply(base, change)
	if err != nil {
		return httpapi.Dashboard{}, s.refusal(ctx, id, err)
	}

	if err = s.mayShow(ctx, who, level, board.Visibility); err != nil {
		return httpapi.Dashboard{}, err
	}

	stored, err := s.rows.Replace(ctx, board, *draft.Revision, level, principal)
	if err != nil {
		return httpapi.Dashboard{}, s.refusal(ctx, id, err)
	}

	// The announcement goes to whoever saw it before or sees it now, so a
	// screen drawing a dashboard narrowed past its reader reads it again and
	// is told it is gone.
	s.announce(ctx, stored.ID, broader(base.Visibility, stored.Visibility), "changed")

	return wireDashboard(stored)
}

// DeleteDashboard removes one dashboard the caller sees.
func (s *dashboardStore) DeleteDashboard(ctx context.Context, principal string, id int64) error {
	_, level, err := s.mayWrite(ctx, principal)
	if err != nil {
		return err
	}

	// Who saw it is read by the statement that removed it, so the
	// announcement reaches them even when somebody narrowed it a moment ago.
	saw, err := s.rows.Delete(ctx, id, level)
	if err != nil {
		return s.refusal(ctx, id, err)
	}

	s.announce(ctx, id, saw, "deleted")

	return nil
}

// SetDefaultDashboard makes one dashboard the one the home page draws, or
// none. It takes configure_resource, because it changes what every person
// signing in sees first.
func (s *dashboardStore) SetDefaultDashboard(ctx context.Context, principal string, id *int64) (*int64, error) {
	who, level, err := s.levelOf(ctx, principal)
	if err != nil {
		return nil, err
	}

	if level < dashboard.VisibleToAdmins.Level() {
		return nil, s.refuse(ctx, who, authz.ActionConfigureResource)
	}

	var at int64
	if id != nil {
		at = *id
	}

	was, err := s.rows.SetDefault(ctx, id, principal)
	if err != nil {
		return nil, s.refusal(ctx, at, err)
	}

	// Both the dashboard that stopped being the default and the one that
	// became it are visible to everyone, and both moved a revision.
	if was != nil && (id == nil || *was != *id) {
		s.announce(ctx, *was, dashboard.VisibleToEveryone, "changed")
	}

	if id != nil && (was == nil || *was != *id) {
		s.announce(ctx, *id, dashboard.VisibleToEveryone, "changed")
	}

	return id, nil
}

// levelOf is the most a caller sees: noDashboardLevel for nothing, then the
// level of each visibility in turn.
func (s *dashboardStore) levelOf(ctx context.Context, principal string) (authz.Subject, int, error) {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return authz.Subject{}, noDashboardLevel, s.internal(ctx, "read the caller's principal", err)
	}

	level, err := dashboardLevel(ctx, s.graph.Can, who)
	if err != nil {
		return authz.Subject{}, noDashboardLevel, s.internal(ctx, "read what the caller may do with dashboards", err)
	}

	return who, level, nil
}

// dashboardLevel is the most a subject sees of the dashboards: noDashboardLevel
// for none, then the level of each visibility in turn, as the actions on the
// kind reach it. The store and the event stream both ask it, so a screen is
// told about exactly the dashboards it may read.
func dashboardLevel(
	ctx context.Context,
	can func(context.Context, authz.Subject, authz.Action, authz.Object) (bool, error),
	who authz.Subject,
) (int, error) {
	object, err := authz.NewObject(authz.TypeResourceKind, ref.KindDashboard.String())
	if err != nil {
		return noDashboardLevel, fmt.Errorf("name the dashboard resource kind: %w", err)
	}

	level := noDashboardLevel

	for at, action := range []authz.Action{
		authz.ActionViewResource, authz.ActionOperateResource, authz.ActionConfigureResource,
	} {
		allowed, canErr := can(ctx, who, action, object)
		if canErr != nil {
			return noDashboardLevel, fmt.Errorf("read whether %s sees dashboards: %w", who, canErr)
		}

		if !allowed {
			break
		}

		level = at
	}

	return level, nil
}

// mayWrite is the caller and their level when they may make, change or
// delete a dashboard, which takes operate_resource on the kind.
func (s *dashboardStore) mayWrite(ctx context.Context, principal string) (authz.Subject, int, error) {
	who, level, err := s.levelOf(ctx, principal)
	if err != nil {
		return authz.Subject{}, noDashboardLevel, err
	}

	if level < dashboard.VisibleToOperators.Level() {
		return authz.Subject{}, noDashboardLevel, s.refuse(ctx, who, authz.ActionOperateResource)
	}

	return who, level, nil
}

// mayShow refuses a visibility past what the caller sees: a dashboard nobody
// who made it could open again.
func (s *dashboardStore) mayShow(
	ctx context.Context,
	who authz.Subject,
	level int,
	visible dashboard.Visibility,
) error {
	if visible.Level() <= level {
		return nil
	}

	return s.refuse(ctx, who, authz.ActionConfigureResource)
}

// refuse is the refusal for a caller the level already found short of the
// action. It is never nil: a graph that grants the action on the second
// question had a grant arrive between the two, and the call was decided on the
// first answer, so it is refused the way Require refuses.
func (s *dashboardStore) refuse(ctx context.Context, who authz.Subject, action authz.Action) error {
	object, err := authz.NewObject(authz.TypeResourceKind, ref.KindDashboard.String())
	if err != nil {
		return s.internal(ctx, "name the dashboard kind", err)
	}

	if err = s.graph.Require(ctx, who, action, object); err != nil {
		return err
	}

	return authz.DeniedObject(action, object)
}

// refusal is the fault a dashboard error is answered with.
func (s *dashboardStore) refusal(ctx context.Context, id int64, err error) error {
	var (
		invalid *dashboard.InvalidError
		changed *dashboard.ChangedError
	)

	switch {
	case errors.As(err, &invalid):
		return httpfault.DashboardInvalid(invalid.Location, invalid.Reason)
	case errors.As(err, &changed):
		return httpfault.DashboardChanged(id, changed.Revision)
	case errors.Is(err, dashboard.ErrNotFound):
		return httpfault.DashboardNotFound(id)
	case errors.Is(err, dashboard.ErrFull):
		return httpfault.DashboardsFull(dashboard.MaxDashboards)
	}

	return s.internal(ctx, "write the dashboard", err)
}

// internal logs a failure the caller cannot act on and answers the redacted
// fault.
func (s *dashboardStore) internal(ctx context.Context, what string, err error) error {
	s.log.ErrorContext(ctx, "dashboard store failure", "what", what, "error", err.Error())

	return httpfault.Unexpected(what)
}

// dashboardChange is the data of com.prusa.connect.dashboard.changed.
type dashboardChange struct {
	ID     int64  `json:"id"`
	Action string `json:"action"`
}

// announce publishes a change on the origin of the visibility, which is the
// group of readers who see it. A failed publication is logged and does not
// undo the write: the row is the truth, and a wall screen reads it again on
// its own every ten minutes.
func (s *dashboardStore) announce(ctx context.Context, id int64, visible dashboard.Visibility, action string) {
	origin, err := dashboardOrigin(visible)
	if err == nil {
		data, _ := json.Marshal(dashboardChange{ID: id, Action: action}) //nolint:errchkjson // two plain fields.

		_, err = s.hub.Publish(stream.Publication{
			Origin: origin,
			Type:   event.TypeDashboardChanged,
			Class:  event.ClassState,
			Key:    fmt.Sprintf("%s/%d", event.TypeDashboardChanged, id),
			Data:   data,
		})
	}

	if err != nil {
		s.log.WarnContext(ctx, "a dashboard change was not announced", "dashboard", id, "error", err.Error())
	}
}

// dashboardOrigin is the origin of the readers of one visibility:
// dashboard:everyone, dashboard:operators or dashboard:admins.
func dashboardOrigin(visible dashboard.Visibility) (event.Origin, error) {
	return event.NewOrigin(event.Kind(ref.KindDashboard.String()), string(visible))
}

// broader is the visibility more people hold.
func broader(a, b dashboard.Visibility) dashboard.Visibility {
	if a.Level() < b.Level() {
		return a
	}

	return b
}

// dashboardGrants is the dashboard origins a subscriber is told about: one
// for each visibility the graph lets them see.
func dashboardGrants(ctx context.Context, sight graph, who authz.Subject) ([]stream.Grant, error) {
	level, err := dashboardLevel(ctx, sight.Can, who)
	if err != nil {
		return nil, err
	}

	out := make([]stream.Grant, 0, level+1)

	for at := range level + 1 {
		origin, originErr := dashboardOrigin(dashboard.VisibilityOf(at))
		if originErr != nil {
			return nil, originErr
		}

		out = append(out, stream.Grant{Origin: origin})
	}

	return out, nil
}

// domainDraft is a draft as the domain reads it. A member a client cannot
// write as JSON is refused here, before any rule runs.
func domainDraft(draft httpapi.DashboardDraft) (dashboard.Draft, error) {
	out := dashboard.Draft{Name: draft.Name, Columns: draft.Columns}

	if draft.Visibility != nil {
		visible := dashboard.Visibility(*draft.Visibility)
		out.Visibility = &visible
	}

	if draft.Density != nil {
		density := dashboard.Density(*draft.Density)
		out.Density = &density
	}

	if draft.Cards != nil {
		cards := make([]dashboard.Card, 0, len(*draft.Cards))

		for i, card := range *draft.Cards {
			config, err := json.Marshal(card.Config)
			if err != nil {
				return dashboard.Draft{}, httpfault.DashboardInvalid(
					fmt.Sprintf("body.cards[%d].config", i), "is not a JSON object")
			}

			cards = append(cards, dashboard.Card{
				ID: card.ID, Type: card.Type, X: card.X, Y: card.Y, W: card.W, H: card.H,
				Title: card.Title, Config: config,
			})
		}

		out.Cards = &cards
	}

	return out, nil
}

// wireDashboard is a stored dashboard as the API answers it.
func wireDashboard(board dashboard.Dashboard) (httpapi.Dashboard, error) {
	cards := make([]httpapi.DashboardCard, 0, len(board.Cards))

	for _, card := range board.Cards {
		config := map[string]any{}
		decoder := json.NewDecoder(bytes.NewReader(card.Config))
		decoder.UseNumber() // a number leaves as it was written, not rounded through float64.

		if err := decoder.Decode(&config); err != nil {
			return httpapi.Dashboard{}, httpfault.Unexpected("read the settings of a card")
		}

		cards = append(cards, httpapi.DashboardCard{
			ID: card.ID, Type: card.Type, X: card.X, Y: card.Y, W: card.W, H: card.H,
			Title: card.Title, Config: config,
		})
	}

	return httpapi.Dashboard{
		ID: board.ID, Name: board.Name,
		Visibility: string(board.Visibility), IsDefault: board.IsDefault,
		Columns: board.Columns, Density: string(board.Density), Cards: cards,
		Revision: board.Revision, Created: board.Created, CreatedBy: board.CreatedBy,
		Changed: board.Changed, ChangedBy: board.ChangedBy,
	}, nil
}
