// Command scaffold-driver writes a printer driver that compiles, registers
// and passes the published conformance suite. It writes go.mod, build.yaml,
// <name>.go, <name>_test.go, faults.go, README.md and CHANGELOG.md, plus
// kind.go when -kind names a device kind. With -storage it writes a storage
// provider and its tests instead.
//
// The go.mod has a replace directive that points at the sdk directory of a
// local checkout. The skeleton driver speaks JSON over HTTP.
//
// Run via: go run ./cmd/scaffold-driver -name acme -module example.com/prusa-driver-acme
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

func main() {
	var opts options

	flag.StringVar(&opts.name, "name", "",
		"the driver type name an operator writes in configuration, as a DNS-1123 label")
	flag.StringVar(&opts.module, "module", "",
		"the Go module path of the new driver, such as example.com/prusa-driver-acme")
	flag.StringVar(&opts.dir, "dir", "",
		"the directory to write into. Defaults to the last element of -module")
	flag.StringVar(&opts.kind, "kind", "",
		"a device kind this module declares, as a DNS-1123 label, for a machine that is not a "+
			"printer. Empty declares none and the module's devices are printers")
	flag.BoolVar(&opts.storage, "storage", false,
		"scaffold a storage provider instead of a printer dialect")
	flag.StringVar(&opts.sdk, "sdk", "",
		"the sdk directory of a prusa-connect checkout. Defaults to the nearest one above the "+
			"working directory")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "scaffold-driver: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	name    string
	module  string
	dir     string
	sdk     string
	kind    string
	storage bool
}

// errUsage marks a bad flag value.
var errUsage = errors.New("cannot scaffold a driver")

func run(opts options) error {
	plan, err := planOf(opts)
	if err != nil {
		return err
	}

	written, err := write(plan)
	if err != nil {
		return err
	}

	for _, file := range written {
		fmt.Fprintf(os.Stderr, "OK %s\n", file)
	}

	fmt.Fprintf(os.Stderr, "\nNext: cd %s && go test ./...\n", plan.Dir)

	return nil
}

// planOf validates the flags and fills in the defaults.
func planOf(opts options) (Plan, error) {
	if err := connectorapi.ValidateTypeName(connectorapi.TypeName(opts.name)); err != nil {
		return Plan{}, fmt.Errorf("%w: -name %q: %w", errUsage, opts.name, err)
	}

	if opts.module == "" {
		return Plan{}, fmt.Errorf("%w: -module is the Go module path of the new driver", errUsage)
	}

	// The host derives a namespace from this path and panics at registration
	// when it cannot. Checking here names the flag.
	namespace, err := extension.NamespaceOf(opts.module)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: -module %q: %w", errUsage, opts.module, err)
	}

	pkg, err := packageName(opts.name)
	if err != nil {
		return Plan{}, err
	}

	sdk, err := sdkPath(opts.sdk)
	if err != nil {
		return Plan{}, err
	}

	version, err := goVersion(sdk)
	if err != nil {
		return Plan{}, err
	}

	dir := opts.dir
	if dir == "" {
		dir = path.Base(opts.module)
	}

	if opts.kind != "" && !opts.storage {
		// The host refuses a kind that is not a label. Refusing here names
		// the flag.
		if err = connectorapi.ValidateTypeName(connectorapi.TypeName(opts.kind)); err != nil {
			return Plan{}, fmt.Errorf("%w: -kind %q: %w", errUsage, opts.kind, err)
		}
	}

	kind := opts.kind
	if opts.storage {
		// A storage provider declares no device kind. With -storage, -kind is
		// the store's display name.
		kind = displayNameOf(opts.kind, opts.name)
	}

	return Plan{
		Name:      opts.name,
		Module:    opts.module,
		Package:   pkg,
		Namespace: namespace.String(),
		Qualified: namespace.String() + extension.QualifiedSeparator + opts.name,
		Dir:       dir,
		SDKPath:   sdk,
		GoVersion: version,
		CorePath:  path.Dir(sdk),
		KindName:  kind,
		Storage:   opts.storage,
	}, nil
}

// displayNameOf is the default label of a scaffolded store: the -kind value,
// or the type name when -kind is empty.
func displayNameOf(given, name string) string {
	if given != "" {
		return given
	}

	return name
}

// packageName turns a driver name into a Go package name. A DNS-1123 label
// allows hyphens and a leading digit. A package name allows neither.
func packageName(name string) (string, error) {
	pkg := strings.ReplaceAll(name, "-", "")

	if pkg == "" || (pkg[0] >= '0' && pkg[0] <= '9') {
		return "", fmt.Errorf(
			"%w: -name %q leaves %q once hyphens are removed, which is not a Go package name. "+
				"Start the name with a letter", errUsage, name, pkg)
	}

	return pkg, nil
}
