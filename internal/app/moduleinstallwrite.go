package app

import (
	"context"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The half of an install that writes: the declaration, the settings, the
// database and the grants of a release whose manifest and files have already
// been checked. installDeclared reaches it once the modules the release
// requires are in place.

// resolvedInstall is what installDeclared has read and checked by the time
// the declaration is written.
type resolvedInstall struct {
	held       release
	namespace  string
	modulePath string
	instance   ref.Ref
	asked      install.ModuleAsks
	checked    int
	// constraint is what the operator asked for, recorded for the lock.
	constraint string
}

// installResolved writes the declaration of a release whose manifest and
// files have been checked, and its grants.
func installResolved(
	ctx context.Context, core config.Core, configPath string, install moduleInstall, read resolvedInstall,
) (moduleReport, error) {
	request, held, instance, asked, checked := install.request, read.held, read.instance, read.asked, read.checked
	namespace := extension.Namespace(read.namespace)

	decided, err := replacing(core, configPath, instance, string(namespace), request, asked, &held.manifest)
	if err != nil {
		return moduleReport{}, err
	}

	declared := decided.declared

	// An offline transaction installs a module new to this server with no
	// operator to choose its database, so it takes the default SQLite one.
	if install.candidateCore != "" && declared.Ref.IsZero() && held.manifest.Needs.Database &&
		request.database == nil {
		request.database = &databaseChoice{mode: string(wirerpc.DatabaseSQLite)}
	}

	// The grants are worked out before anything is written, so a manifest asking
	// for a permission that cannot be granted stops the install here rather than
	// halfway through it.
	work, err := grantWorkFor(ctx, install, instance, held.manifest, declared)
	if err != nil {
		return moduleReport{}, err
	}

	defer work.close()

	kept, err := keptSettingsAndDatabase(ctx, core, namespace.String(), held.manifest, request, declared, instance)
	if err != nil {
		return moduleReport{}, err
	}

	install.stopReplaced(declared, held.manifest.Version, instance)

	written, landed, err := writeDeclaration(core, held, string(namespace), instance, declaredAs{
		approve: request.approve, settings: kept.settings, asked: asked, channel: request.channel,
		database: kept.database, left: kept.left, keep: install.keeps(declared), memory: decided.memory,
		path: read.modulePath, address: request.address, digest: request.digest, pulled: request.pulled,
		constraint: read.constraint,
	})
	if err != nil {
		return moduleReport{}, err
	}

	// After the declaration, because a grant written for a module that is not
	// declared is a grant nobody will ever read back.
	moved, err := work.move(ctx)

	made := reportOf(instance, written, landed, held, checked, request.approve)
	made.replaced, made.written, made.leftBefore = declared, written, kept.leftBefore
	made.databaseLeft, made.databaseBack = databaseLeft(kept.left), len(kept.left.Take) > 0
	made.ports = openPorts(asked.Listen, listenAsksOf(held.manifest.Needs.Listen))
	made.requests = append(made.requests, made.ports...)
	slices.Sort(made.requests)
	made.granted = moved.granted
	made.withdrawn = moved.withdrawn
	made.apart = work.plan.apart

	if request.approve {
		made.approved, made.withheld = approvedOf(held.manifest, made.namespace, moved.gave)
		made.added = decided.added
	}

	// The module is installed and declared at this point, and some grants may
	// have landed before one failed. The report says which, so the operator
	// knows what to correct by hand, and the error still fails the command.
	if err != nil {
		return made, err
	}

	return made, nil
}
