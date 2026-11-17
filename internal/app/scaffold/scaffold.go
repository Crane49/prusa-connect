// Package scaffold writes a new module from a skeleton, so an author starts
// from something that builds.
//
// It writes one of three kinds, each the shape of an example with the names
// substituted. UI + process is examples/spools: a process serving one route,
// the fragment that describes it, the manifest that declares both halves, and
// a browser bundle. UI only is examples/nozzle-watch. Process only is the
// process and the manifest with no ui block. Everything a module needs to be
// installed with `module add` is there, and nothing else.
//
// # Why the templates sit here
//
// The embed directive reads files inside the package that embeds them, and
// `cmd/` holds only commands. The templates sit beside the code that renders
// them. cmd/scaffold-driver keeps its own templates for the same reason.
package scaffold

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// templates holds the files a scaffolded module is rendered from. They end in
// .tmpl because a .go file with a template action in it does not compile and a
// .json one does not parse.
//
//go:embed templates/*.tmpl templates/slots/*.tmpl
var templates embed.FS

// The modes the scaffold writes under. A skeleton is source an author edits,
// so only the author may read and write the files.
const (
	dirPerm  = 0o750
	filePerm = 0o600
)

// The failures a scaffold reports. Callers inspect them with errors.Is.
var (
	// ErrNotEmpty reports a target directory that already holds something.
	// Nothing is overwritten: half a module mixed with half a skeleton can
	// still compile, and the author would not see the damage.
	ErrNotEmpty = errors.New("already holds something. Pick a new folder, or an empty one")
	// ErrNoCheckout reports a directory that is not a prusa-connect
	// checkout holding the sdk and the component library, and a search that
	// found none above the working directory.
	ErrNoCheckout = errors.New("scaffold: no prusa-connect checkout")
)

// Vacant refuses a dir that exists and is not an empty folder. A caller checks
// this first for a message it can show. Write checks it again, so a direct
// call to Write cannot overwrite a module.
func Vacant(dir string) error {
	entries, err := os.ReadDir(dir)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("%s cannot hold the module: %w", dir, err)
	case len(entries) > 0:
		return fmt.Errorf("%s %w", dir, ErrNotEmpty)
	default:
		return nil
	}
}

// coreModule is the module a scaffolded module compiles the sdk out of.
const coreModule = "github.com/prusa3d/prusa-connect"

// sdkModule is the module path of the sdk, which is the one Go dependency a
// module has.
const sdkModule = coreModule + "/sdk"

// Plan is one scaffold, fully resolved. Nothing below it reads a flag or the
// filesystem to decide what to write.
type Plan struct {
	// Name is the module's short name: the instance name, the binary's name,
	// and the page's path.
	Name string
	// Title is what a person reads: the sidebar entry and the page heading.
	Title string
	// Module is the Go module path of the new module, which is what the
	// namespace is derived from.
	Module string
	// Namespace is what that path renders as on a host. The author never
	// writes it; it is here so the skeleton's comments can name the addresses
	// their module will answer on.
	Namespace string
	// Kind is the resource kind the one instance is declared under.
	Kind string
	// Version is the module's first release.
	Version string
	// Dir is where the files are written.
	Dir string
	// Checkout is the prusa-connect checkout the skeleton builds
	// against, with forward slashes, which is the only separator go.mod and a
	// pnpm link read on every platform. It is empty for a module written
	// outside a checkout, which asks the registries for the sdk and the
	// packages instead.
	Checkout string
	// UICheckout is Checkout relative to the module's ui/ folder, with forward
	// slashes. pnpm-workspace.yaml maps the three packages to it, so nothing a
	// module commits names a path on its author's disk. Empty with Checkout.
	UICheckout string
	// GoVersion is the language version the go.mod declares, taken from the
	// sdk so a module never declares less than the contract it compiles
	// against.
	GoVersion string
	// Library is the range of @prusa/connect-ui the manifest asks for,
	// computed from the library in the checkout.
	Library string
	// SDKVersion is the sdk release go.mod requires when there is no checkout
	// to replace it with.
	SDKVersion string
	// Packages is the version of @prusa/connect and
	// @prusa/connect-ui that ui/package.json asks for when there is no
	// checkout to link them from.
	Packages string
	// UI is a module with a browser bundle, and Process one with a process the
	// host runs. A module has one or both. With no process it is the asset
	// kind, which the host verifies against a digest, serves, and never runs,
	// and nothing under Go is written for it. With no bundle nothing under ui/
	// is written and the manifest has no ui block.
	UI      bool
	Process bool
	// MemoryMiB is the memory the process recommends in needs.memory_mib. Zero
	// writes the line commented out.
	MemoryMiB int
	// Contributions are what this module's bundle registers, one per slot of
	// the table the author picked. A slot with no Contribution here makes
	// nothing there: no entry in the manifest's `contributes` block, no
	// component, and no registration in the entry.
	Contributions []Contribution
}

// Contributes is the one contribution the plan makes to slot, and nil when it
// makes none there. The per-slot templates read it for their own condition.
func (p Plan) Contributes(slot manifest.SlotName) *Contribution {
	for i := range p.Contributions {
		if p.Contributions[i].Slot == slot {
			return &p.Contributions[i]
		}
	}

	return nil
}

// Contribution is one entry in the manifest's `contributes` block together
// with the one `host.register` call and the one component or run function
// that match it. All three are rendered from this, so a registration the
// manifest did not declare cannot be written.
type Contribution struct {
	// Slot is the row of the slot table this contribution fills.
	Slot manifest.SlotName
	// ID is the page's route, or the section's or the action's id: one label
	// with no slash.
	ID string
	// Title is what a person reads: the sidebar entry, the section heading,
	// the settings tab, or the action's button.
	Title string
	// Icon is the icon a page or an action may show, empty for the library's
	// default. A section carries none.
	Icon manifest.Icon
	// Destructive marks a FormAction contribution drawn as a destructive
	// item. The host asks no confirmation before it runs; the module
	// confirms it if it needs to. It means nothing for a page or a section.
	Destructive bool
}

// row is this contribution's line of the slot table. A Contribution is never
// given a Slot the table does not carry, so the second return needs no check
// here.
func (c Contribution) row() manifest.Slot {
	row, _ := manifest.SlotNamed(c.Slot)

	return row
}

// Key is the list under ui.contributes this contribution is declared under.
func (c Contribution) Key() string { return c.Slot.Key() }

// Offer is the slot table's own hint text for what this contribution adds,
// the same sentence `make new-module` shows beside the choice.
func (c Contribution) Offer() string { return c.row().Offer }

// Register is the function on host.register this contribution is registered
// with.
func (c Contribution) Register() string { return c.Slot.Register() }

// Form is the shape of this contribution's manifest entry.
func (c Contribution) Form() manifest.Form { return c.row().Form }

// FormName is "page", "section", "action" or "card", so a template picks the
// manifest entry's shape by name instead of comparing manifest.Form's ints.
func (c Contribution) FormName() string {
	switch c.Form() {
	case manifest.FormPage:
		return "page"
	case manifest.FormAction:
		return "action"
	case manifest.FormSection:
		return "section"
	case manifest.FormCard:
		return "card"
	default:
		return "section"
	}
}

// File is where the component is written, relative to the module's Dir; for
// a FormAction contribution it is the run function instead, since an action
// has no component of its own.
func (c Contribution) File() string {
	ext := ".svelte"
	if c.Form() == manifest.FormAction {
		ext = ".ts"
	}

	return path.Join("ui", "src", string(c.Slot)+ext)
}

// Import is the identifier ui/src/index.ts imports this contribution's file
// under: the slot name in PascalCase.
func (c Contribution) Import() string {
	parts := strings.Split(string(c.Slot), "-")
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}

	return strings.Join(parts, "")
}

// ImportPath is the module specifier index.ts imports Import from. A
// component keeps its .svelte extension, which is what tells the bundler's
// Svelte plugin to compile it; a run function's .ts is dropped, the bare
// specifier every other TypeScript import in the bundle uses.
func (c Contribution) ImportPath() string {
	base := path.Base(c.File())
	if c.Form() == manifest.FormAction {
		return strings.TrimSuffix(base, ".ts")
	}

	return base
}

// slotContext is what a per-slot template renders from: the plan, so
// `{{.Name}}` still reaches the module's own name, alongside the one
// contribution the template is writing a file for, as `{{.Contribution}}`.
// Contribution is a named field because it shares a field name, Title, with
// Plan: embedding both would make `{{.Title}}` an ambiguous selector that
// fails only when a template reads it.
type slotContext struct {
	Plan

	Contribution Contribution
}

// Release is the directory, relative to Dir, the build writes the release
// into. It is the bundle's output when there is a bundle, because the manifest
// and the binary go beside the entry there.
func (p Plan) Release() string {
	if p.UI {
		return "ui/dist"
	}

	return "dist"
}

// Files maps each template to the file it renders to, relative to Dir.
//
// Only what the plan asks for is in it. A module with no process has no
// main.go, one with no bundle has no ui/, and a slot the plan does not fill
// has no component.
func (p Plan) Files() map[string]string {
	files := map[string]string{
		"templates/module.yaml.tmpl":            "module.yaml",
		"templates/README.md.tmpl":              "README.md",
		"templates/CHANGELOG.md.tmpl":           "CHANGELOG.md",
		"templates/gitignore.tmpl":              ".gitignore",
		"templates/vscode-extensions.json.tmpl": filepath.Join(".vscode", "extensions.json"),
	}

	// install.sh builds against the checkout and installs into the data of its
	// server, so a module with no checkout has none.
	if p.Checkout != "" {
		files["templates/install.sh.tmpl"] = "install.sh"
	}

	if p.UI {
		source := filepath.Join("ui", "src")

		files["templates/ui-package.json.tmpl"] = filepath.Join("ui", "package.json")
		files["templates/ui-pnpm-workspace.yaml.tmpl"] = filepath.Join("ui", "pnpm-workspace.yaml")

		// Outside a checkout the three packages come from the registry .npmrc
		// names. Inside one, pnpm-workspace.yaml's overrides link them from
		// disk.
		if p.Checkout == "" {
			files["templates/ui-npmrc.tmpl"] = filepath.Join("ui", ".npmrc")
		}
		files["templates/ui-svelte.config.tmpl"] = filepath.Join("ui", "svelte.config.js")
		files["templates/ui-eslint.config.js.tmpl"] = filepath.Join("ui", "eslint.config.js")
		files["templates/ui-prettierignore.tmpl"] = filepath.Join("ui", ".prettierignore")
		files["templates/ui-prettierrc.js.tmpl"] = filepath.Join("ui", ".prettierrc.js")
		files["templates/ui-tsconfig.json.tmpl"] = filepath.Join("ui", "tsconfig.json")
		files["templates/ui-vite.config.ts.tmpl"] = filepath.Join("ui", "vite.config.ts")
		files["templates/ui-index.ts.tmpl"] = filepath.Join(source, "index.ts")
		files["templates/ui-host.ts.tmpl"] = filepath.Join(source, "host.ts")
		files["templates/ui-styles.css.tmpl"] = filepath.Join(source, "styles.css")
		files["templates/ui-locales-en.json.tmpl"] = filepath.Join(source, "locales", "en.json")

		for _, c := range p.Contributions {
			name := path.Base(c.File())
			files["templates/slots/"+name+".tmpl"] = filepath.Join(source, name)
		}
	}

	if p.Process {
		files["templates/go.mod.tmpl"] = "go.mod"
		files["templates/main.go.tmpl"] = "main.go"
		files["templates/main_test.go.tmpl"] = "main_test.go"
		files["templates/faults.go.tmpl"] = "faults.go"
		files["templates/openapi.json.tmpl"] = "openapi.json"
	}

	return files
}

// Write refuses a plan.Dir that is not empty, then renders every template
// into it and returns the paths it wrote, sorted.
//
// Every file is rendered before any is written, so a template that will not
// render writes nothing.
func Write(plan Plan) ([]string, error) {
	if err := Vacant(plan.Dir); err != nil {
		return nil, err
	}

	rendered := make(map[string][]byte, len(plan.Files()))

	for source, target := range plan.Files() {
		full := filepath.Join(plan.Dir, target)

		body, err := Render(templates, source, dataFor(plan, source))
		if err != nil {
			return nil, err
		}

		rendered[full] = body
	}

	written := make([]string, 0, len(rendered))

	for path, body := range rendered {
		if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
			return nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}

		if err := os.WriteFile(path, body, filePerm); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}

		written = append(written, path)
	}

	slices.Sort(written)

	return written, nil
}

// dataFor is what source renders from: the plan on its own, or the plan
// paired with the one contribution a slot template writes a file for.
func dataFor(plan Plan, source string) any {
	if !strings.HasPrefix(source, "templates/slots/") {
		return plan
	}

	name := strings.TrimSuffix(path.Base(source), ".tmpl")

	for _, c := range plan.Contributions {
		if path.Base(c.File()) == name {
			return slotContext{Plan: plan, Contribution: c}
		}
	}

	return plan
}

// Render fills one template out of an embedded set and returns the file it
// wrote.
//
// It is exported because cmd/scaffold-driver renders its own templates with
// it, so the two tools share one renderer.
//
// Nothing here formats Go source. go/format pulls the parser and the printer
// into whatever links it, and the shipping binary has a size budget. The
// templates are written formatted, and
// TestAScaffoldedModuleIsAlreadyFormatted checks them. cmd/scaffold-driver is
// a developer tool and formats its own output.
func Render(templates fs.FS, source string, data any) ([]byte, error) {
	parsed, err := template.New(path.Base(source)).
		Funcs(template.FuncMap{"shell": ShellWord}).
		ParseFS(templates, source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}

	var out bytes.Buffer
	if err = parsed.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", source, err)
	}

	return out.Bytes(), nil
}

// ShellWord is word as a POSIX shell reads it back unchanged. Templates call
// it as `shell`, for a value such as a checkout path that may hold a space.
func ShellWord(word string) string {
	if word != "" && !strings.ContainsAny(word, " \t\n'\"\\$`*?[]#~&;|<>(){}!") {
		return word
	}

	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// FindCheckout resolves the checkout a skeleton builds against. A relative
// given path is joined to from. The path is checked: a link to a directory
// with no library in it would fail at the author's first install with a
// message about a package and not about the path. With none given it is the
// first checkout at or above from.
func FindCheckout(given, from string) (string, error) {
	if given != "" {
		if !filepath.IsAbs(given) {
			given = filepath.Join(from, given)
		}

		if err := checkCheckout(given); err != nil {
			return "", err
		}

		return filepath.ToSlash(filepath.Clean(given)), nil
	}

	dir := from

	for {
		if checkCheckout(dir) == nil {
			return filepath.ToSlash(dir), nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w at or above %s", ErrNoCheckout, from)
		}

		dir = parent
	}
}

// checkCheckout reports whether dir is a prusa-connect checkout holding
// the two things a skeleton needs: the sdk module and the component library.
func checkCheckout(dir string) error {
	body, err := os.ReadFile(filepath.Join(dir, "sdk", "go.mod"))
	if err != nil {
		return fmt.Errorf("%w: %s holds no sdk/go.mod: %w", ErrNoCheckout, dir, err)
	}

	if !bytes.Contains(body, []byte("module "+sdkModule)) {
		return fmt.Errorf("%w: %s/sdk is not %s", ErrNoCheckout, dir, sdkModule)
	}

	if _, err = os.Stat(filepath.Join(dir, "packages", "svelte", "package.json")); err != nil {
		return fmt.Errorf("%w: %s holds no packages/svelte: %w", ErrNoCheckout, dir, err)
	}

	return nil
}

// GoVersion reads the language version the checkout's sdk declares.
func GoVersion(checkout string) (string, error) {
	body, err := os.ReadFile(filepath.Join(checkout, "sdk", "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read the sdk go.mod: %w", err)
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		field, version, found := strings.Cut(strings.TrimSpace(line), " ")
		if found && field == "go" {
			return strings.TrimSpace(version), nil
		}
	}

	return "", fmt.Errorf("%w: %s/sdk/go.mod declares no go version", ErrNoCheckout, checkout)
}

// LibraryRange is the range of the component library a new bundle asks for,
// from the version in the checkout.
//
// It is computed because the app refuses a bundle whose range does not hold
// the app's own library version. A range copied from an example goes wrong
// when the library's major version moves.
func LibraryRange(checkout string) (string, error) {
	body, err := os.ReadFile(filepath.Join(checkout, "packages", "svelte", "package.json"))
	if err != nil {
		return "", fmt.Errorf("read the component library's package.json: %w", err)
	}

	var packaged struct {
		Version string `json:"version"`
	}

	if err = json.Unmarshal(body, &packaged); err != nil {
		return "", fmt.Errorf("read the component library's version: %w", err)
	}

	return RangeOf(packaged.Version)
}

// RangeOf is version up to the next major.
func RangeOf(version string) (string, error) {
	major, _, _, err := extension.Version(version).Parse()
	if err != nil {
		return "", fmt.Errorf("the component library's version is %q: %w", version, err)
	}

	return ">=" + version + " <" + strconv.Itoa(major+1) + ".0.0", nil
}
