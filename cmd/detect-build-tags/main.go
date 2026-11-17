// Command detect-build-tags prints the optional Go build tags a configuration
// implies, for `make build` to pass to the compiler.
//
// The mapping from configuration to tags is data: adapters/tags.json. A
// configuration file never names a build tag itself.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config/build"
)

const (
	exitOK      = 0
	exitFailure = 1
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr *os.File) int {
	flags := flag.NewFlagSet("detect-build-tags", flag.ContinueOnError)
	flags.SetOutput(stderr)

	configPath := flags.String("config", "config/config.yaml", "the operator's configuration file")
	adapters := flags.String("adapters", build.DefaultCataloguePath, "the type and condition to build tag map")
	variant := flags.String("variant", build.VariantFull, "build variant: full, standard, minimal")
	extra := flags.String("extra", "", "additional space-separated build tags")

	if err := flags.Parse(args); err != nil {
		return exitFailure
	}

	tags, err := detect(*configPath, *adapters, *variant, build.SplitExtraTags(*extra))
	if err != nil {
		fmt.Fprintln(stderr, "detect-build-tags: "+err.Error())

		return exitFailure
	}

	_, _ = stdout.WriteString(strings.Join(tags, " "))

	return exitOK
}

// detect reads both inputs and derives the tags. A missing configuration file
// counts as an empty one. A missing tag map is an error.
func detect(configPath, adapters, variant string, extra []string) ([]string, error) {
	content, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}

	catalogue, err := build.LoadCatalogue(adapters)
	if err != nil {
		return nil, err
	}

	return build.Detect(configPath, content, variant, extra, catalogue)
}
