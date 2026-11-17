package app

import (
	"fmt"
	"io"
	"slices"
	"text/tabwriter"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
)

// listModules prints the modules this deployment declares, from the record on
// disk.
//
// It reads the files rather than the API. The listing at /api/v1/resources
// takes a credential, and this command holds none: the core reads no
// environment variable (internal/arch/container_environment_test.go), so a
// token would be a flag somebody puts in their shell history. What the record
// says and what a running core is serving differ only until that core restarts,
// and the source column says which file each row came from.
func listModules(out io.Writer, core config.Core, configPath string) error {
	rows, err := resource.Installed(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return err
	}

	if len(rows) == 0 {
		// The usual hint would send the operator to a command that is refused.
		if !core.ModulesOn() {
			fmt.Fprintln(out, "no module is declared. core.modules_enabled is false, so `module add` is refused.")

			return listLeft(out, core)
		}

		fmt.Fprintln(out, "no module is declared. `module add` installs one.")

		return listLeft(out, core)
	}

	// ENABLED is the flag the declaration holds, so without this line a
	// module the key keeps off would look switched on.
	if !core.ModulesOn() && slices.ContainsFunc(rows, resource.Declaration.Installed) {
		fmt.Fprintln(out, "core.modules_enabled is false: a module with SOURCE overlay does not start.")
	}

	table := tabwriter.NewWriter(out, 0, 0, columnGap, ' ', 0)

	fmt.Fprintln(table, "INSTANCE\tTYPE\tRUNTIME\tENABLED\tUI\tSOURCE\tPATH")

	for _, row := range rows {
		fmt.Fprintf(table, "%s\t%s\t%s\t%t\t%t\t%s\t%s\n",
			row.Ref, row.Spec.Type, row.Spec.Runtime, row.Spec.Enabled,
			drawsAUI(row), row.Source, modulePathOf(row))
	}

	if err = table.Flush(); err != nil {
		return err
	}

	return listLeft(out, core)
}

// drawsAUI reports whether a declaration carries a browser half. It reads the
// declaration and not the bundle's manifest: what the bundle contributes is
// what the running core publishes, and this command reads no bundle.
func drawsAUI(declared resource.Declaration) bool {
	if declared.Spec.Asset != nil {
		return true
	}

	return declared.Spec.Service != nil && declared.Spec.Service.Assets != nil
}

// modulePathOf is where a declaration's files are, which is the directory an
// operator looks in when they want to know what is installed.
func modulePathOf(declared resource.Declaration) string {
	if declared.Spec.Service != nil {
		return declared.Spec.Service.Path
	}

	if declared.Spec.Asset != nil {
		return declared.Spec.Asset.Path
	}

	return ""
}
