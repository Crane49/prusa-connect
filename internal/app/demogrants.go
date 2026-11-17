package app

import (
	"context"
	"fmt"
	"io"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// demoModuleGrants makes the modules' grants in the demo's graph what the
// declarations record, and prints a line for each grant it wrote or took back.
// The declarations are every module the configuration file and its overlay
// declare together.
//
// The demo keeps its graph in its own directory, whatever the operator's file
// says (demoOwned). `module add -approve` run against that same file writes
// the module's grants into the graph the file names, which is a different
// database, and `module remove` takes them back from there. Without this the
// install reports a grant the demo never reads, and a removal leaves the demo's
// grant standing for the next module installed under the same name.
//
// Both directions use the plan and the principal the install uses. A grant a
// declared module records and the graph lacks is written. A grant on the
// organization for a module's principal that no declaration records is taken
// back: the principal of a module nothing declares, or a permission its
// declaration dropped. The declaration is the source of truth here, so a module
// grant an administrator withdraws over the API in a running demo is written
// again at the next start. A grant for any other subject is never touched, and
// neither is one for a declared instance that is not a module, such as a
// configured webhook, whose grants an administrator writes by hand.
func demoModuleGrants(ctx context.Context, out io.Writer, path string) error {
	core, err := config.Load(path)
	if err != nil {
		return err
	}

	rows, err := resource.Installed(resource.Paths{File: path, Overlay: core.Overlay})
	if err != nil {
		return err
	}

	var recorded moduleGrantPlan

	// The value is whether the instance is a module. Only a module's principal
	// is reconciled; another declared instance keeps whatever it holds.
	declared := make(map[string]bool, len(rows))

	for _, row := range rows {
		_, module := row.ModuleNamespace()
		declared[row.Ref.ID()] = module

		plan := declaredGrants(row)
		recorded.write = append(recorded.write, plan.write...)

		for _, left := range plan.unplanned {
			fmt.Fprintf(out, "Left alone the recorded permission %q of %s, because %s\n", left.name, row.Ref, left.why)
		}
	}

	authority, closeGraph, err := openModuleGrants(ctx, core)
	if err != nil {
		return err
	}

	defer closeGraph()

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	if err != nil {
		return fmt.Errorf("name the organization in the authorization graph: %w", err)
	}

	tuples, err := authority.graph.Grants(ctx, authority.who, org)
	if err != nil {
		return fmt.Errorf("read the grants on %s in the demo's graph: %w", org, err)
	}

	var held moduleGrantPlan

	for _, tuple := range tuples {
		subject := tuple.Subject()
		if subject.Type() != authz.TypeSink || subject.IsUserset() {
			continue
		}

		if module, isDeclared := declared[subject.ID()]; isDeclared && !module {
			continue
		}

		held.write = append(held.write, moduleGrant{tuple: tuple})
	}

	granted, _, err := authority.write(ctx, recorded.without(held))
	for _, line := range granted {
		fmt.Fprintf(out, "Granted %s\n", line)
	}

	if err != nil {
		return err
	}

	withdrawn, err := authority.withdraw(ctx, held.without(recorded))
	for _, line := range withdrawn {
		fmt.Fprintf(out, "Withdrew %s\n", line)
	}

	return err
}
