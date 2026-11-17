package build

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
)

// The build variants. A variant decides which conditions survive, never which
// tag a condition implies: the tag names are the airlock's.
const (
	VariantFull     = "full"
	VariantStandard = "standard"
	VariantMinimal  = "minimal"
)

// The conditions this package evaluates. Each one is a key in the airlock, and
// a condition with no entry there is an error.
const (
	conditionObservability = "core.observability.enabled"
	conditionDiagnostics   = "core.diagnostics.enabled"
	// conditionNoDashboards is the one condition that takes code out: it holds
	// when core.dashboards.enabled is false, and its tag compiles the
	// dashboards out of a binary that would never serve them.
	conditionNoDashboards = "core.dashboards.disabled"
	// conditionSQLite implies no tag: internal/authz links SQLite into every
	// build untagged, for the reason internal/authz/sqlite.go gives, and
	// adapters/db/sqlite is compiled unconditionally beside it. The condition
	// is read so an operator who set it is not refused.
	conditionSQLite   = "persistence.sqlite"
	conditionPostgres = "persistence.postgres"
)

// The values the one persistence selector takes. There is no engine per store:
// the graph, the records and the queue share one database, so a file cannot
// put two of them on two engines.
const (
	engineSQLite   = "sqlite"
	enginePostgres = "postgres"
)

// Config is the settings that decide which optional features are compiled in.
// The resources block is read separately by install.Parse, and it is the only
// path by which a declared adapter influences the build.
//
// Every condition this tool reads is a key the running server reads as well.
// The web app, the dashboards and the metrics client sit under `core` because
// the server reads them there. One key means a build cannot be derived from a
// value the runtime ignores. `persistence` sits at the top level for the same
// reason: the server reads the engine there.
//
// The top-level blocks `mqtt`, `observability` and `fractal` are retired. They
// are decoded only so a file carrying one is refused by name.
type Config struct {
	Core struct {
		Frontend      Switch `yaml:"frontend"`
		Dashboards    Switch `yaml:"dashboards"`
		Diagnostics   Switch `yaml:"diagnostics"`
		Observability struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"observability"`
	} `yaml:"core"`
	// The retired keys, decoded only so that a file carrying one is refused by
	// name. A build that ignored them would skip the step the operator asked
	// for and print nothing.
	Server *struct {
		API *struct {
			Enabled  *bool `yaml:"enabled"`
			Frontend *bool `yaml:"frontend"`
		} `yaml:"api"`
	} `yaml:"server"`
	Documentation *struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"documentation"`
	MQTT          *yaml.Node `yaml:"mqtt"`
	Observability *yaml.Node `yaml:"observability"`
	Fractal       *yaml.Node `yaml:"fractal"`
	Persistence   struct {
		Type string `yaml:"type"`
	} `yaml:"persistence"`
}

// Switch is one `enabled:` key that defaults to on. The pointer separates an
// operator who wrote `enabled: false` from one who wrote no block. Only the
// first means off.
type Switch struct {
	Enabled *bool `yaml:"enabled"`
}

// On reports whether the step is built. An absent key is on.
func (s Switch) On() bool { return s.Enabled == nil || *s.Enabled }

// ErrRetiredKey reports a configuration that carries a key this tool does not
// read. The message names the key that replaced it.
var ErrRetiredKey = errors.New("build: this key is no longer read")

// ErrUnknownTag reports an extra tag this tree does not compile against. go
// build accepts any word as a tag, so a misspelt one would build a binary
// without the part it names.
var ErrUnknownTag = errors.New("build: this tree has no such build tag")

// Detect returns the optional tags a configuration implies, sorted and
// deduplicated. Name is what a message calls the config file.
//
// A declared instance contributes its type's tag when it is switched on and
// reached as a builtin. An instance that is switched off, reached as a service,
// or of a type this tree does not carry contributes nothing.
//
// An instance that will not load contributes nothing and does not stop the
// detection. One wrong block must not leave an operator with no binary.
func Detect(name string, content []byte, variant string, extra []string, cat Catalogue) ([]string, error) {
	if variant == "" {
		variant = VariantFull
	}

	switch variant {
	case VariantFull, VariantStandard, VariantMinimal:
	default:
		return nil, fmt.Errorf("unsupported build variant %q", variant)
	}

	if cat.IsZero() {
		return nil, fmt.Errorf("%w: no catalogue was loaded", ErrCatalogue)
	}

	cfg, err := parseConfig(name, content)
	if err != nil {
		return nil, err
	}

	tags := map[string]struct{}{}

	add := func(tag string) {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags[tag] = struct{}{}
		}
	}

	if err = blockTags(cfg, variant, cat, add); err != nil {
		return nil, err
	}

	declared, err := install.Parse(name, content)
	if err != nil {
		return nil, err
	}

	resourceTags(declared, cat, add)

	for _, tag := range extra {
		if !cat.Knows(tag) {
			return nil, fmt.Errorf("%w: %q. The build tags are %s",
				ErrUnknownTag, tag, strings.Join(cat.Tags(), " "))
		}

		add(tag)
	}

	out := make([]string, 0, len(tags))
	for tag := range tags {
		out = append(out, tag)
	}

	slices.Sort(out)

	return out, nil
}

// blockTags evaluates the top-level blocks. The conditions are here because
// they are control flow; the tags they imply are the airlock's.
func blockTags(cfg Config, variant string, cat Catalogue, add func(string)) error {
	minimal := variant == VariantMinimal

	conditions := map[string]bool{
		conditionObservability: cfg.Core.Observability.Enabled,
		conditionNoDashboards:  !cfg.Core.Dashboards.On(),
		conditionDiagnostics:   cfg.Core.Diagnostics.On(),
		conditionSQLite:        cfg.Persistence.Type == engineSQLite,
		conditionPostgres:      cfg.Persistence.Type == enginePostgres,
	}

	names := slices.Sorted(func(yield func(string) bool) {
		for name := range conditions {
			if !yield(name) {
				return
			}
		}
	})

	for _, name := range names {
		if !conditions[name] {
			continue
		}

		tag, err := cat.TagForCondition(name, minimal)
		if err != nil {
			return err
		}

		add(tag)
	}

	return nil
}

// resourceTags adds the tag of every declared instance that runs in this
// binary. It applies in every variant, including minimal. A resources entry is
// an instance the operator declared, and a build that dropped its type would
// refuse the configuration it was built from.
func resourceTags(declared install.Document, cat Catalogue, add func(string)) {
	for _, instance := range declared.Instances {
		if instance.State != install.StateRuntime || instance.Runtime != install.RuntimeBuiltin {
			continue
		}

		tag, held := cat.TagForType(instance.Ref.Kind().String(), instance.Type)
		if !held {
			continue
		}

		add(tag)
	}
}

// Features are the build steps a configuration switches on that no Go build tag
// carries: whether the web app is compiled and embedded. They are separate from
// Detect's tags because they decide which sub-builds run, not what the compiler
// is handed. The documentation site is not one: the binary links the
// documentation its build names, and `make docs` builds the site when asked.
type Features struct {
	FrontendEnabled bool
}

// DetectFeatures reads the build-step toggles out of a configuration. Name is
// what a message calls the file.
//
// An empty file is a checkout nobody has configured. It builds the mandatory
// parts and no optional step, as Detect names no optional tag for the same
// file. Any other file is read the way the server reads it: the app is built
// unless `core.frontend.enabled` switches it off.
func DetectFeatures(name string, content []byte) (Features, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return Features{}, nil
	}

	cfg, err := parseConfig(name, content)
	if err != nil {
		return Features{}, err
	}

	return Features{
		FrontendEnabled: cfg.Core.Frontend.On(),
	}, nil
}

// parseConfig decodes one configuration and refuses a retired key by name.
func parseConfig(name string, content []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", name, err)
	}

	if err := refuseRetiredKeys(name, cfg); err != nil {
		return Config{}, err
	}

	applyDefaults(&cfg)

	return cfg, nil
}

// refuseRetiredKeys stops a build whose configuration names a key this tool
// does not read, and says which key replaced it.
//
// Ignoring one would fail without a message: a file that says
// `server.api.frontend: true` would build no app and print nothing, and the
// operator would find out when the origin root answered 404.
func refuseRetiredKeys(name string, cfg Config) error {
	if cfg.Server != nil && cfg.Server.API != nil &&
		(cfg.Server.API.Enabled != nil || cfg.Server.API.Frontend != nil) {
		return fmt.Errorf("%w: %s names server.api, which the build stopped reading. "+
			"The web app is core.frontend.enabled", ErrRetiredKey, name)
	}

	if cfg.Documentation != nil && cfg.Documentation.Enabled != nil {
		return fmt.Errorf("%w: %s names documentation.enabled, which the build stopped reading. "+
			"The binary links the documentation its build names, and `make docs` builds the site",
			ErrRetiredKey, name)
	}

	for _, retired := range retiredBlocks {
		if retired.node(cfg) == nil {
			continue
		}

		return fmt.Errorf("%w: %s names the top-level %s block, which the build stopped reading. %s",
			ErrRetiredKey, name, retired.block, retired.moved)
	}

	return nil
}

// retiredBlocks are the retired top-level blocks, each with the sentence
// naming the key that replaced it. The core refuses the same three blocks with
// the same replacements; see internal/app/config/strict.go.
//
//nolint:gochecknoglobals // the inventory of retired blocks, read at parse time and never written.
var retiredBlocks = []struct {
	block string
	moved string
	node  func(Config) *yaml.Node
}{
	{
		block: "mqtt",
		moved: "This build serves no API over MQTT. To send the printers' state to a broker, " +
			"declare a resources.sink row of type mqtt",
		node: func(cfg Config) *yaml.Node { return cfg.MQTT },
	},
	{
		block: "observability",
		moved: "The metrics client is core.observability.enabled",
		node:  func(cfg Config) *yaml.Node { return cfg.Observability },
	},
	{
		block: "fractal",
		moved: "A link to a parent is a resource instance: resources.link.<name>.enabled. " +
			"It implies no build tag, because the link imports the standard library alone",
		node: func(cfg Config) *yaml.Node { return cfg.Fractal },
	},
}

// applyDefaults fills what a file left unset, so a tag is derived from the
// values the server will run with.
func applyDefaults(cfg *Config) {
	if cfg.Persistence.Type == "" {
		cfg.Persistence.Type = engineSQLite
	}
}

// SplitExtraTags splits a string like "postgres mqtt" into individual tags.
func SplitExtraTags(raw string) []string { return strings.Fields(strings.TrimSpace(raw)) }
