// Command extract-printer-profile writes the capability profile of one printer
// model to a file. `make build-printer` runs it before it compiles the printer
// build, which embeds that file in place of the profiles of every model.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

const (
	exitOK      = 0
	exitFailure = 1
)

// errUnknownModel reports a model name that no profile answers to.
var errUnknownModel = errors.New("no profile answers to this model")

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("extract-printer-profile", flag.ContinueOnError)
	flags.SetOutput(stderr)

	model := flags.String("model", "", "the printer model, as PRINTER_MODEL names it")
	out := flags.String("out", "internal/printer/profile/printermodel/profile.json", "the file to write")

	if err := flags.Parse(args); err != nil {
		return exitFailure
	}

	written, err := extract(*model, *out)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "extract-printer-profile: "+err.Error())

		return exitFailure
	}

	_, _ = fmt.Fprintf(stdout, "OK    the profile of %s is in %s\n", written, *out)

	return exitOK
}

// extract writes the part of the built-in registry the model uses and returns
// the model's name as the registry spells it.
func extract(model, out string) (string, error) {
	registry, err := profile.Builtin()
	if err != nil {
		return "", fmt.Errorf("read the built-in profiles: %w", err)
	}

	name, ok := registry.Canonical(model)
	if !ok {
		return "", fmt.Errorf("%w: %q. The models are %s",
			errUnknownModel, model, strings.Join(registry.Models(), ", "))
	}

	one := only(registry, name)
	one.GeneratedBy = "make build-printer PRINTER_MODEL=" + name

	data, err := json.MarshalIndent(one, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode the profile: %w", err)
	}

	if writeErr := os.WriteFile(out, append(data, '\n'), 0o600); writeErr != nil {
		return "", fmt.Errorf("write %s: %w", out, writeErr)
	}

	return name, nil
}

// only returns the part of the registry one model uses: its profiles, the alias
// rows that name it, and the bound rows narrowed to it. The printer build
// embeds this part alone, because a printer serves only its own model.
func only(registry profile.Registry, model string) profile.Registry {
	out := profile.Registry{Schema: registry.Schema}

	for _, candidate := range registry.Profiles {
		if strings.EqualFold(candidate.Model, model) {
			out.Profiles = append(out.Profiles, candidate)
		}
	}

	for _, alias := range registry.Aliases {
		if strings.EqualFold(alias.Model, model) {
			out.Aliases = append(out.Aliases, alias)
		}
	}

	for _, bound := range registry.Bounds {
		if slices.ContainsFunc(bound.Models, func(named string) bool { return strings.EqualFold(named, model) }) {
			bound.Models = []string{model}
			out.Bounds = append(out.Bounds, bound)
		}
	}

	return out
}
