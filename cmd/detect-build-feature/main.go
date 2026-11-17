// Command detect-build-feature prints whether a configuration turns on the
// web app, a build step with no Go build tag. It reads core.frontend.enabled.
//
// It prints only "true" or "false", because `make build` compares the output
// in a shell test.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/prusa3d/prusa-connect/internal/app/config/build"
)

const (
	exitOK      = 0
	exitFailure = 1
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr *os.File) int {
	flags := flag.NewFlagSet("detect-build-feature", flag.ContinueOnError)
	flags.SetOutput(stderr)

	configPath := flags.String("config", "config/config.yaml", "the operator's configuration file")
	field := flags.String("field", "", "the build step to report: frontend")

	if err := flags.Parse(args); err != nil {
		return exitFailure
	}

	enabled, err := detect(*configPath, *field)
	if err != nil {
		fmt.Fprintln(stderr, "detect-build-feature: "+err.Error())

		return exitFailure
	}

	if enabled {
		_, _ = stdout.WriteString("true")

		return exitOK
	}

	_, _ = stdout.WriteString("false")

	return exitOK
}

// detect reads the configuration and reports one field. A missing
// configuration file counts as an empty one.
func detect(configPath, field string) (bool, error) {
	if field == "" {
		return false, errors.New("--field is required: frontend")
	}

	content, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", configPath, err)
	}

	features, err := build.DetectFeatures(configPath, content)
	if err != nil {
		return false, err
	}

	switch field {
	case "frontend":
		return features.FrontendEnabled, nil
	default:
		return false, fmt.Errorf("unknown build step %q: frontend", field)
	}
}
