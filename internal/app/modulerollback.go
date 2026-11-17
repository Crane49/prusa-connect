package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// An update over HTTP is kept only when the new release starts. The release it
// replaced waits beside it until then, and a new release that fails to start,
// such as one whose migration of its own database failed, is taken back: the
// old files, the old declaration and the old grants return, and the old
// release runs again.

// settleUpdate keeps or takes back an update by how its new release started.
// A release that started deletes the one it replaced. One that failed is
// rolled back. One still on its way up when the wait ended keeps the old
// release on disk, and the next install of the module or its removal deletes
// it.
func (m *moduleInstaller) settleUpdate(
	ctx context.Context, who authz.Subject, module string, held moduleReport, started moduleStart,
) error {
	if held.previous == "" {
		return nil
	}

	switch started.state {
	case moduleStateFailed, moduleStateRetrying:
		return m.rollBack(ctx, who, module, held, started.reason)
	case moduleStateDisabled:
		// An update the operator did not approve starts nothing, so there is
		// nothing to judge it by, and the operator chose it.
		m.discardPrevious(ctx, held.previous)
	default:
		if started.running {
			m.discardPrevious(ctx, held.previous)
		}
	}

	return nil
}

// discardPrevious deletes the release an update replaced. A failure is
// logged: the update stands, and the next install of the module clears the
// directory again.
func (m *moduleInstaller) discardPrevious(ctx context.Context, previous string) {
	if err := os.RemoveAll(previous); err != nil {
		m.log.WarnContext(ctx, "the release an update replaced could not be deleted",
			"path", previous, "error", err.Error())
	}
}

// rollBack takes an update back: it stops the new release, puts the old files
// and the old declaration back, writes the old grants again and starts the old
// release. It answers the fault that says so.
func (m *moduleInstaller) rollBack(
	ctx context.Context, who authz.Subject, module string, held moduleReport, reason string,
) error {
	m.stopInstance(held.instance)

	kept := ""
	if held.replaced.Spec.Module != nil {
		kept = held.replaced.Spec.Module.Version
	}

	if err := restoreRelease(ctx, held.root, held.previous); err != nil {
		m.log.ErrorContext(ctx, "an update did not start and its old release could not be put back",
			"resource", held.instance.String(), "error", err.Error())

		return fmt.Errorf("the release %s did not start (%s), and the release it replaced could not be "+
			"put back: %w", held.version, reason, err)
	}

	if err := resource.RestoreModule(m.core.Overlay, held.replaced, held.leftBefore); err != nil {
		return fmt.Errorf("the release %s did not start (%s), and the declaration of %s could not be "+
			"written back: %w", held.version, reason, kept, err)
	}

	if err := m.restoreGrants(ctx, who, held); err != nil {
		m.log.ErrorContext(ctx, "an update was taken back and its grants could not be",
			"resource", held.instance.String(), "error", err.Error())
	}

	// The answer waits for the old release to serve again, so a caller that
	// reads the refusal finds the module up.
	restart := m.converge(ctx, "rollback")
	again := m.startOf(ctx, held.instance, held.replaced.Spec.Enabled, restart, m.settleFor(held))

	m.log.WarnContext(ctx, "an update did not start and was taken back",
		"resource", held.instance.String(), "version", held.version, "kept", kept, "reason", reason,
		"state", again.state)

	return httpfault.ModuleUpdateRolledBack(module, held.version, kept, reason)
}

// restoreRelease deletes the new release's files and moves the old ones back.
// The delete is retried, because the process just stopped may still hold its
// executable for a moment (see deleteModuleRoot).
func restoreRelease(ctx context.Context, root, previous string) error {
	var err error

	for attempt := range deleteAttempts {
		if err = os.RemoveAll(root); err == nil {
			break
		}

		if attempt == deleteAttempts-1 || !waitFor(ctx, deletePause) {
			return fmt.Errorf("delete the new release: %w", err)
		}
	}

	if err = os.Rename(previous, root); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("move the old release back: %w", err)
	}

	return nil
}

// restoreGrants writes the grants the old declaration justified and takes
// back the ones only the new release asked for. It is the install's own plan,
// run from the new declaration back to the old permissions.
func (m *moduleInstaller) restoreGrants(ctx context.Context, who authz.Subject, held moduleReport) error {
	opener := m.grantsOf(who)
	if opener == nil {
		return nil
	}

	asking := manifest.Manifest{Permissions: declaredPermissions(held.replaced)}
	if service := held.replaced.Spec.Service; service != nil {
		asking.CheckAbsentPeople = service.CheckAbsentPeople
	}

	work, err := grantWorkFor(ctx, moduleInstall{
		request: moduleRequest{approve: len(asking.Permissions) > 0 || asking.CheckAbsentPeople},
		grants:  opener,
	}, held.instance, asking, held.written)
	if err != nil {
		return err
	}

	defer work.close()

	_, err = work.move(ctx)

	return err
}
