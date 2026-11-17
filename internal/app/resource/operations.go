package resource

import (
	"context"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// memoryWindow is the longest time a history covers. A history also covers
// this time when the caller names no start.
const memoryWindow = time.Hour

// MemoryHistory is what the instance's process used since the instant,
// limited to the last hour. A caller who may not see the instance gets the
// answer for an instance that does not exist. An instance the plane keeps no
// memory history for answers empty lists.
func (s *Store) MemoryHistory(
	ctx context.Context, principal string, instance ref.Ref, since time.Time,
) (httpapi.ResourceMemoryHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	who, err := s.subject(principal)
	if err != nil {
		return httpapi.ResourceMemoryHistory{}, err
	}

	if err = s.mustSee(ctx, who, instance); err != nil {
		return httpapi.ResourceMemoryHistory{}, err
	}

	if err = s.mustDeclare(ctx, principal, instance, "memory"); err != nil {
		return httpapi.ResourceMemoryHistory{}, err
	}

	if floor := s.now().Add(-memoryWindow); since.Before(floor) {
		since = floor
	}

	history := httpapi.ResourceMemoryHistory{}

	if s.plane != nil {
		history, _ = s.plane.MemoryHistory(instance, since)
	}

	if history.Items == nil {
		history.Items = []httpapi.MemoryPoint{}
	}

	if history.Stops == nil {
		history.Stops = []httpapi.ResourceStop{}
	}

	return history, nil
}

// Log is the newest standard error of the instance. It needs
// configure_resource on the kind, because a module can print a setting it was
// given.
func (s *Store) Log(ctx context.Context, principal string, instance ref.Ref) (httpapi.ResourceLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	who, err := s.subject(principal)
	if err != nil {
		return httpapi.ResourceLog{}, err
	}

	if err = s.mustSee(ctx, who, instance); err != nil {
		return httpapi.ResourceLog{}, err
	}

	configures, err := s.writable(ctx, who, instance.Kind())
	if err != nil {
		return httpapi.ResourceLog{}, err
	}

	if !configures {
		return httpapi.ResourceLog{}, authz.Denied(authz.ActionConfigureResource, instance)
	}

	if err = s.mustDeclare(ctx, principal, instance, "log"); err != nil {
		return httpapi.ResourceLog{}, err
	}

	log := httpapi.ResourceLog{}

	if s.plane != nil {
		log, _ = s.plane.Log(instance)
	}

	if log.Lines == nil {
		log.Lines = []httpapi.ResourceLogLine{}
	}

	return log, nil
}

// mustSee refuses a caller who may not read the instance, with the answer for
// an instance that does not exist.
func (s *Store) mustSee(ctx context.Context, who authz.Subject, instance ref.Ref) error {
	seen, err := s.visible(ctx, who, instance)
	if err != nil {
		return err
	}

	if !seen {
		return httpfault.ResourceNotFound(instance.String())
	}

	return nil
}

// mustDeclare refuses an instance the configuration does not declare, as Get
// does. The caller holds s.mu.
func (s *Store) mustDeclare(ctx context.Context, principal string, instance ref.Ref, op string) error {
	doc, err := s.reread(ctx, principal, op)
	if err != nil {
		return err
	}

	if _, found := doc.Find(instance); !found {
		return httpfault.ResourceNotFound(instance.String())
	}

	return nil
}
