package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// graph is the authorization graph as the event stream reads it: one question
// per subscription and one atomic load per subscriber tick. It is declared here
// because this is where it is consumed, and *authz.Store satisfies it.
type graph interface {
	// Visible is the whole set a principal may observe, stamped with the
	// generation it was computed at.
	Visible(ctx context.Context, subject authz.Subject) (authz.Visibility, error)
	// VisibleDevicesFor is the device part of that answer for one action. The
	// audit narrowing uses it: the same read with a different mask.
	VisibleDevicesFor(
		ctx context.Context, subject authz.Subject, action authz.Action,
	) ([]authz.VisibleDevice, error)
	// Epoch is the current generation, bumped by any grant change.
	Epoch() uint64

	// Can answers one permission question. It decides whether a subscriber
	// sees the admission exchange of the connectors this process mounted.
	Can(ctx context.Context, subject authz.Subject, action authz.Action, object authz.Object) (bool, error)
}

// admissionOrigins is the connector instances this process mounted a
// registration table for.
//
// A connector is not a declared resource. It is configured under
// core.connectors and the graph holds no instance row for it, so the
// visibility answer cannot find one in the resource table. The names are bound
// here after the connectors are built, because the hub is made before them.
//
// Without it nobody could subscribe to the registration events, because their
// origin is the connector that holds the code.
type admissionOrigins struct {
	mu    sync.RWMutex
	names []string
}

// bind records the connector instances, in a fixed order so two readings of the
// visibility answer list them the same way.
func (a *admissionOrigins) bind(names []string) {
	if a == nil {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.names = slices.Sorted(slices.Values(names))
}

// all is the connector instances, as a copy.
func (a *admissionOrigins) all() []string {
	if a == nil {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	return slices.Clone(a.names)
}

// visibility is stream.Authorizer over the authorization graph. It lives in the
// composition root because internal/authz is a domain package and may not
// import a transport, and internal/transport/stream may not import a domain's
// policy. Neither side can hold this mapping, so the code that builds both
// does.
//
// It only renders the graph's answer. The epoch it reports is the one the
// graph stamped its answer with, never one read here. Visible reads the
// generation before the rows, so a write landing in between leaves the answer
// stamped older than the graph, and the reader re-authorizes once too often.
// A stamp read after the rows could be newer than the rows it describes, and
// a revocation would then never be noticed.
type visibility struct {
	graph graph
	// admission is the connectors whose registration exchange a caller who may
	// decide admissions watches. It is nil where nothing publishes one.
	admission *admissionOrigins
	// dashboardsOff is core.dashboards.enabled switched off, which grants no
	// dashboard origin. The zero value grants them, so the history reader,
	// which builds a visibility with the graph alone, sees what the stream
	// shows.
	dashboardsOff bool
	serverID      string
	serverAdmin   interface {
		Grants(context.Context, authz.Subject, authz.Object) ([]authz.Tuple, error)
	}
}

var _ stream.Authorizer = visibility{}

// VisibleOrigins renders every printer and every configured instance the
// principal may observe as the origin the hub routes on, each carrying the
// generation of the graph this principal's sight of it began at. That
// generation bounds a replay when the origin is handed to a reader mid-stream,
// so it is per origin and not one number for the whole answer.
func (v visibility) VisibleOrigins(ctx context.Context, principal string) (stream.Visibility, error) {
	subject, err := authz.ParseSubject(principal)
	if err != nil {
		return stream.Visibility{}, fmt.Errorf("read the subscriber's principal: %w", err)
	}

	answer, err := v.graph.Visible(ctx, subject)
	if err != nil {
		return stream.Visibility{}, fmt.Errorf("read what %s may observe: %w", principal, err)
	}

	// Audit sight is a second read of the same projection under a different
	// mask. It is read here and not at subscribe time, because the
	// re-authorization pass calls this function on every epoch move. So audit
	// sight is recomputed as often as sight of the origin is.
	audited, err := v.graph.VisibleDevicesFor(ctx, subject, auditAction())
	if err != nil {
		return stream.Visibility{}, fmt.Errorf("read what %s may audit: %w", principal, err)
	}

	audit := make(map[string]struct{}, len(audited))
	for _, printer := range audited {
		audit[printer.ID] = struct{}{}
	}

	origins := make([]stream.Grant, 0, len(answer.Devices)+len(answer.Resources))

	for _, printer := range answer.Devices {
		origin, originErr := event.NewOrigin(event.KindDevice, printer.ID)
		if originErr != nil {
			return stream.Visibility{}, fmt.Errorf("render device %q as an origin: %w", printer.ID, originErr)
		}

		_, mayAudit := audit[printer.ID]

		origins = append(origins, stream.Grant{Origin: origin, Since: printer.Since, Audit: mayAudit})

		// A camera's lifecycle and stored stills are on its own origin, seen as the device is.
		if printer.Kind == authz.DeviceKindCamera {
			camera, cameraErr := event.NewOrigin(event.Kind(ref.KindCamera), printer.ID)
			if cameraErr != nil {
				return stream.Visibility{}, fmt.Errorf("render camera %q as an origin: %w", printer.ID, cameraErr)
			}

			origins = append(origins, stream.Grant{Origin: camera, Since: printer.Since})
		}
	}

	for _, instance := range answer.Resources {
		origin, originErr := event.NewOrigin(event.Kind(instance.Ref.Kind()), instance.Ref.ID())
		if originErr != nil {
			return stream.Visibility{}, fmt.Errorf("render resource %s as an origin: %w", instance.Ref, originErr)
		}

		origins = append(origins, stream.Grant{Origin: origin, Since: instance.Since})
	}

	admission, err := v.admissionGrants(ctx, subject)
	if err != nil {
		return stream.Visibility{}, err
	}

	origins = append(origins, admission...)

	server, err := v.serverGrant(ctx, subject)
	if err != nil {
		return stream.Visibility{}, err
	}
	origins = append(origins, server...)

	if !v.dashboardsOff {
		boards, boardsErr := dashboardGrants(ctx, v.graph, subject)
		if boardsErr != nil {
			return stream.Visibility{}, boardsErr
		}

		origins = append(origins, boards...)
	}

	return stream.Visibility{Origins: origins, Epoch: answer.Epoch}, nil
}

func (v visibility) serverGrant(ctx context.Context, subject authz.Subject) ([]stream.Grant, error) {
	if v.serverID == "" {
		return nil, nil
	}

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	if err != nil {
		return nil, fmt.Errorf("name the server organization: %w", err)
	}
	if v.serverAdmin == nil {
		return nil, nil
	}

	if _, err = v.serverAdmin.Grants(ctx, subject, org); err != nil {
		raised, denied := errors.AsType[*fault.Fault](err)
		if denied && raised.Code() == authz.CodeAccessDenied() {
			return nil, nil
		}

		return nil, fmt.Errorf("read whether %s may administer the server: %w", subject, err)
	}

	origin, err := event.NewOrigin(event.Kind("server"), v.serverID)
	if err != nil {
		return nil, fmt.Errorf("render the server event origin: %w", err)
	}

	return []stream.Grant{{Origin: origin}}, nil
}

// admissionGrants is the connector origins this subscriber may watch.
//
// The question is asked once, on the driver resource kind. The registration
// surface asks the same one before it lists or decides a code
// (requireRegistrationAdmin). So only a caller who may decide who joins this
// fleet is told when a machine asks to join.
func (v visibility) admissionGrants(
	ctx context.Context, subject authz.Subject,
) ([]stream.Grant, error) {
	names := v.admission.all()

	if len(names) == 0 {
		return nil, nil
	}

	object, err := authz.NewObject(authz.TypeResourceKind, ref.KindDriver.String())
	if err != nil {
		return nil, fmt.Errorf("name the driver resource kind: %w", err)
	}

	allowed, err := v.graph.Can(ctx, subject, authz.ActionAdmitDevice, object)
	if err != nil {
		return nil, fmt.Errorf("read whether %s may decide an admission: %w", subject, err)
	}

	if !allowed {
		return nil, nil
	}

	out := make([]stream.Grant, 0, len(names))

	for _, name := range names {
		origin, originErr := event.NewOrigin(event.Kind(ref.KindDriver.String()), name)
		if originErr != nil {
			return nil, fmt.Errorf("render connector %q as an origin: %w", name, originErr)
		}

		out = append(out, stream.Grant{Origin: origin})
	}

	return out, nil
}

// Epoch is the graph's own generation, read on every subscriber's tick.
func (v visibility) Epoch() uint64 { return v.graph.Epoch() }
