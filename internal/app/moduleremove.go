package app

// Taking one module away: the declaration out of the machine-owned overlay, and
// the files off the disk. It is the other half of the install in modulecli.go,
// and the API and the command both reach it here.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// removeModule takes one module's declaration out of the overlay and deletes
// what was installed for it.
func removeModule(
	ctx context.Context, out io.Writer, core config.Core, configPath, named string, withUnused bool,
) error {
	if strings.TrimSpace(named) == "" {
		return fmt.Errorf("%w: %s %s <module or kind:name>", ErrUsage, ModuleCommand, removeVerb)
	}

	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return err
	}

	found, err := moduleNamed(rows, strings.TrimSpace(named))
	if err != nil {
		return err
	}

	// What the install granted this module, worked out from the permissions the
	// declaration recorded. A module nobody approved recorded none, and the
	// removal then takes no grant away.
	plan := declaredGrants(found)

	var grants *moduleAuthority

	if len(plan.write) > 0 || withUnused {
		opened, closeGraph, openErr := openModuleGrants(ctx, core)
		if openErr != nil {
			return openErr
		}

		defer closeGraph()

		grants = opened
	}

	// No stopper. This command talks to a configuration file and not to a
	// running core, so there is no supervised instance here to stop. A core
	// that is running holds the module's artefact open, and the delete below
	// says so rather than half removing the module.
	done, err := removeWithUnused(ctx, moduleRemoval{
		core: core, configPath: configPath, found: found, grants: grants, plan: plan, withUnused: withUnused,
	})

	if done.root != "" {
		fmt.Fprintf(out, "removed %s and deleted %s\n", found.Ref, done.root)

		for _, grant := range done.withdrawn {
			fmt.Fprintf(out, "  withdrawn   %s\n", grant)
		}

		if done.left != nil {
			fmt.Fprintf(out, "  left behind %s, with the module's data. Installing the same module again "+
				"takes it back, and `%s %s %s` deletes it\n",
				databaseGranted(done.left.Grant()), ModuleCommand, purgeVerb, found.Ref)
		}

		for _, left := range plan.unplanned {
			fmt.Fprintf(out, "  left alone  the recorded permission %q, because %s\n", left.name, left.why)
		}

		for _, gone := range done.unusedRemoved {
			fmt.Fprintf(out, "removed %s, which was installed for it and nothing else requires\n", gone)
		}

		if len(done.unused) > 0 {
			fmt.Fprintf(out, "  unused      %s were installed for it and nothing else requires them. "+
				"`%s %s -with-unused %s` removes them with it\n",
				joinRefs(done.unused), ModuleCommand, removeVerb, named)
		}
	}

	if err != nil {
		return err
	}

	if err = recordLock(core, configPath); err != nil {
		return err
	}

	namespace, _ := found.ModuleNamespace()

	return runningCoreNotice(ctx, out, core, configPath, len(done.withdrawn) > 0,
		"The same removal through the API, DELETE /api/v1/modules/"+namespace+", takes effect at once.")
}

// declaredGrants is the grants the recorded permissions of a declaration
// justify: what a removal takes back, and what the demo writes into its own
// graph.
//
// A permission it cannot plan, one the graph does not hold or one no role
// reaches, is carried in the plan and reported. It is not refused: a removal
// refused here would leave the files, the declaration and every grant that
// could be planned, with nothing left to act on them.
func declaredGrants(declared resource.Declaration) moduleGrantPlan {
	plan, err := planModuleGrants(declared.Ref, declaredPermissions(declared))
	if err != nil {
		return moduleGrantPlan{unplanned: []unplannedPermission{{
			name: declared.Ref.String(),
			why:  "this server could not name it in the authorization graph: " + err.Error(),
		}}}
	}

	return plan
}

// declaredPermissions is the permissions one declaration recorded, as the names
// a manifest writes them under. A declaration with no process half recorded
// none: only a module that runs a process acts as itself.
func declaredPermissions(declared resource.Declaration) []string {
	if declared.Spec.Service == nil {
		return nil
	}

	return authz.ActionNames(declared.Spec.Service.Permissions)
}

// moduleRemoval is one uninstall: which declaration goes, and the two seams the
// caller decides.
type moduleRemoval struct {
	core       config.Core
	configPath string
	found      resource.Declaration
	// stop ends the running instance and waits for its process to exit, and
	// reports false when nothing was running under that name. Nil is a caller
	// with no supervision plane in this process, which is the command.
	stop func(ref.Ref) bool
	// remove deletes the module's directory. Nil is os.RemoveAll. It is a seam
	// so a test can fail the first attempt the way a held file does.
	remove func(string) error
	// pause is the wait between delete attempts. Zero is deletePause.
	pause time.Duration
	// grants takes the module's own grants out of the authorization graph. Nil
	// is a caller with no graph in this process, and the grants then stay
	// written.
	grants *moduleAuthority
	// plan is what those grants are, from the permissions the declaration
	// recorded. The caller builds it with declaredGrants, because it is also
	// what decides whether the graph had to be opened at all.
	plan moduleGrantPlan
	// withUnused also removes the modules an install added for this one that
	// nothing else requires once it is gone. Without it they are only named.
	withUnused bool
}

// The bound on waiting for the module's directory to go. Windows reports
// Access is denied on a file whose process has just exited and whose handle
// the system has not released yet, so the delete is retried for about two
// seconds and then reported.
const (
	deleteAttempts = 20
	deletePause    = 100 * time.Millisecond
)

// removeDeclared stops the instance, deletes what was installed for it, takes
// the declaration out of the overlay, takes the module's grants out of the
// authorization graph, and answers the directory it deleted with a line per
// grant it withdrew.
//
// The files go before the declaration. A delete that fails then leaves the
// module installed and the same call is what finishes it, where the other order
// leaves a deployment with no declaration, files nobody will collect, and a
// second attempt that reports the module was never there.
//
// The grants go last, and they go here rather than in either caller. The
// command and DELETE /api/v1/modules/<namespace> both arrive through this
// function, and a grant withdrawn on one path and left on the other would be a
// module removed over HTTP whose principal keeps the fleet for ever.
func removeDeclared(ctx context.Context, op moduleRemoval) (removed, error) {
	if op.found.Source == resource.SourceFile {
		return removed{}, handDeclaredError{
			instance: op.found.Ref, file: op.configPath, installsOff: !op.core.ModulesOn(),
		}
	}

	// Before anything is stopped or deleted, so a removal another module
	// requires changes nothing.
	unused, err := removalResolves(op.core, op.configPath, op.found)
	if err != nil {
		return removed{}, err
	}

	// The process first. A running module holds its own executable open, and
	// Windows refuses to delete a file anything holds.
	if op.stop != nil {
		op.stop(op.found.Ref)
	}

	root := filepath.Join(op.core.Modules, op.found.Ref.ID())
	if err = deleteModuleRoot(ctx, op, root); err != nil {
		return removed{}, err
	}

	// The release an update kept aside, when the update ended before it could
	// say whether the new one started. No process runs from it.
	if err = os.RemoveAll(previousRelease(op.core.Modules, op.found.Ref.ID())); err != nil {
		kept := filepath.ToSlash(previousRelease(op.core.Modules, op.found.Ref.ID()))
		held := httpfault.ModuleFilesHeld(kept,
			"the module's files are still on this server and its declaration was kept")

		return removed{}, held.Wrap(err)
	}

	left := leftOnRemoval(op.found)

	held, err := resource.RemoveModule(op.core.Overlay, op.found.Ref, left)
	if err != nil {
		return removed{}, err
	}

	if !held {
		return removed{}, fmt.Errorf("no declaration of %s is in %s", op.found.Ref, op.core.Overlay)
	}

	// After the declaration, so a removal that stopped halfway leaves the module
	// declared with the grants it was installed with rather than running and
	// reading nothing.
	withdrawn, err := op.grants.withdraw(ctx, op.plan)
	if err != nil {
		return removed{root: root, withdrawn: withdrawn, left: left, unused: refsOf(unused)},
			fmt.Errorf("the module is removed and some of its grants are still written. Take them away "+
				"by hand: %w", err)
	}

	return removed{root: root, withdrawn: withdrawn, left: left, unused: refsOf(unused)}, nil
}

// removeWithUnused removes one module and, when the operator asked, the
// modules an install added for it that nothing else requires any more, in
// the order the resolver gives. Without the ask they are only named.
func removeWithUnused(ctx context.Context, op moduleRemoval) (removed, error) {
	done, err := removeDeclared(ctx, op)
	if err != nil || !op.withUnused {
		return done, err
	}

	rows, err := resource.Installed(resource.Paths{File: op.configPath, Overlay: op.core.Overlay})
	if err != nil {
		return done, err
	}

	for _, instance := range done.unused {
		found, held := moduleOf(rows, instance)
		if !held {
			continue
		}

		next := op
		next.found, next.plan, next.withUnused = found, declaredGrants(found), false

		if _, err = removeDeclared(ctx, next); err != nil {
			return done, fmt.Errorf("remove %s, which was installed for %s: %w", instance, op.found.Ref, err)
		}

		done.unusedRemoved = append(done.unusedRemoved, instance)
	}

	done.unused = nil

	return done, nil
}

// moduleOf is the declaration of one instance among the installed modules.
func moduleOf(rows []resource.Declaration, instance ref.Ref) (resource.Declaration, bool) {
	for _, one := range rows {
		if one.Ref == instance {
			return one, true
		}
	}

	return resource.Declaration{}, false
}

// refsOf is the instances of some declarations.
func refsOf(declared []resource.Declaration) []ref.Ref {
	out := make([]ref.Ref, 0, len(declared))
	for _, one := range declared {
		out = append(out, one.Ref)
	}

	return out
}

// joinRefs writes instances for one line.
func joinRefs(instances []ref.Ref) string {
	names := make([]string, 0, len(instances))
	for _, instance := range instances {
		names = append(names, instance.String())
	}

	return strings.Join(names, ", ")
}

// removed is what one removal did: the directory it deleted, a line per grant
// it withdrew, and the module's own database it left behind.
type removed struct {
	root      string
	withdrawn []string
	left      *resource.LeftDatabase
	// unused is the modules an install added for this one that nothing
	// requires any more and that were left installed, and unusedRemoved the
	// ones removed with it.
	unused        []ref.Ref
	unusedRemoved []ref.Ref
}

// leftOnRemoval is the module's own database a removal leaves behind, and nil
// for a module granted none. The database and its data stay where they are,
// and the record is what the operator purges.
func leftOnRemoval(declared resource.Declaration) *resource.LeftDatabase {
	if declared.Spec.Service == nil || declared.Spec.Service.Database == nil {
		return nil
	}

	left := resource.LeftBehind(declared.Ref, declared.Spec.Type, declared.Spec.Service.Database,
		resource.LeftRemoved, time.Now())

	return &left
}

// deleteModuleRoot deletes the module's directory, trying again for a short
// while before it gives up.
//
// The retry is for one case: the process this uninstall has just stopped is
// gone and the system has not released its handle on the executable yet, which
// Windows reports as Access is denied. Waiting is what that needs, and a
// failure that outlasts the wait is answered rather than retried forever.
func deleteModuleRoot(ctx context.Context, op moduleRemoval, root string) error {
	remove := op.remove
	if remove == nil {
		remove = os.RemoveAll
	}

	pause := op.pause
	if pause <= 0 {
		pause = deletePause
	}

	var err error

	for attempt := range deleteAttempts {
		if err = remove(root); err == nil {
			return nil
		}

		if attempt == deleteAttempts-1 || !waitFor(ctx, pause) {
			break
		}
	}

	return httpfault.ModuleFilesHeld(filepath.ToSlash(root),
		"the module's files are still on this server and its declaration was kept").Wrap(err)
}

// waitFor sleeps, and reports false when the context ended first.
func waitFor(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
