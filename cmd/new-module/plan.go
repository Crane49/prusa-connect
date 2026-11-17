// Turning the answers into the plan the templates are rendered from.

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// shape says whether a module has a UI, a process or both. The -kind flag
// sets it. It is not called kind in code because that means the resource kind.
type shape string

// The shapes, in menu order.
const (
	shapeUI      shape = "ui"
	shapeProcess shape = "process"
	shapeBoth    shape = "both"
)

//nolint:gochecknoglobals // a closed table, read and never written.
var shapes = []shape{shapeUI, shapeProcess, shapeBoth}

// shapeWords is each shape's name in the summary and its line in the menu.
//
//nolint:gochecknoglobals // a closed table, read and never written.
var shapeWords = map[shape]struct{ title, what string }{
	shapeUI:      {"UI only", "it draws on the app's screens, and runs nothing"},
	shapeProcess: {"process only", "a Go program the server runs, with nothing on screen"},
	shapeBoth:    {"UI + process", "screens, and the Go program they read from"},
}

func (s shape) title() string { return shapeWords[s].title }

// draws reports whether the module has a browser bundle.
func (s shape) draws() bool { return s == shapeUI || s == shapeBoth }

// runs reports whether the module has a process the host runs.
func (s shape) runs() bool { return s == shapeProcess || s == shapeBoth }

func shapeOffers() []string {
	offers := make([]string, 0, len(shapes))
	for _, s := range shapes {
		offers = append(offers, shapeWords[s].title+": "+shapeWords[s].what)
	}

	return offers
}

// shapeNumber is s's number in the kind menu, or 1 for an unknown shape.
func shapeNumber(s shape) int {
	if index := slices.Index(shapes, s); index >= 0 {
		return index + 1
	}

	return 1
}

func shapeNames() string {
	return string(shapeUI) + ", " + string(shapeProcess) + " or " + string(shapeBoth)
}

// repoDefault is the default repository address: github.com, then the gh
// login or "example". It needs a real host or it gives no namespace.
func repoDefault(login, name string) string {
	return "github.com/" + firstOf(login, "example") + "/" + name
}

// repoOf cleans a pasted repository address: it drops the scheme, a trailing
// slash and a .git suffix.
func repoOf(typed string) string {
	repo := strings.TrimSpace(typed)
	repo = strings.TrimPrefix(strings.TrimPrefix(repo, "https://"), "http://")

	return strings.TrimSuffix(strings.TrimSuffix(repo, "/"), ".git")
}

// planFor builds the plan the skeleton is rendered from. It refuses input
// the host would reject, before any file is written.
func planFor(request answers) (scaffold.Plan, error) {
	name := strings.TrimSpace(request.name)
	if name == "" {
		return scaffold.Plan{}, fmt.Errorf("%w: -name is the module's short name", errUsage)
	}

	if !slices.Contains(shapes, request.shape) {
		return scaffold.Plan{}, fmt.Errorf("%w: -kind is %s", errUsage, shapeNames())
	}

	kind := initKind
	if !request.shape.runs() {
		kind = assetKind
	}

	// The name is the instance name, the binary name and the page path. The
	// error from ref.New quotes raw bytes, so the rule is printed instead.
	if _, err := ref.New(ref.Kind(kind), name); err != nil {
		return scaffold.Plan{}, fmt.Errorf("\"%s\" cannot name an instance. "+
			"Use lower-case letters, digits and dashes, like nozzle-watch", printable(name))
	}

	if request.shape.draws() && len(request.contributions) == 0 {
		return scaffold.Plan{}, fmt.Errorf(
			"%w: a bundle that registers nothing draws nothing. -contributes takes %s",
			errUsage, strings.Join(slotNames(), ", "))
	}

	// The default is resolved here and never stored, so it follows the name
	// when the name changes on a later pass.
	repo := repoOf(request.repo)
	if repo == "" {
		repo = repoDefault(request.login, name)
	}

	namespace, err := extension.NamespaceOf(repo)
	if err != nil {
		return scaffold.Plan{}, fmt.Errorf("%s gives no namespace: %w", repo, err)
	}

	checkout, dir, err := placeFor(request, name)
	if err != nil {
		return scaffold.Plan{}, err
	}

	if err = scaffold.Vacant(dir); err != nil {
		return scaffold.Plan{}, err
	}

	goVersion, library, err := versionsFor(checkout)
	if err != nil {
		return scaffold.Plan{}, err
	}

	plan := scaffold.Plan{
		Name:       name,
		Title:      titleOf(request.title, name),
		Module:     repo,
		Namespace:  string(namespace),
		Kind:       kind,
		Version:    firstVersion,
		Dir:        dir,
		Checkout:   checkout,
		GoVersion:  goVersion,
		Library:    library,
		SDKVersion: publishedSDK,
		Packages:   publishedPackages,
		UI:         request.shape.draws(),
		Process:    request.shape.runs(),
		MemoryMiB:  request.memoryMiB,
	}

	if checkout != "" {
		rel, relErr := physicalRel(filepath.Join(dir, "ui"), filepath.FromSlash(checkout))
		if relErr != nil {
			return scaffold.Plan{}, fmt.Errorf("the checkout relative to the module: %w", relErr)
		}

		plan.UICheckout = rel
	}

	if plan.UI {
		fill(&plan, request.contributions)
	}

	return plan, nil
}

// physicalRel is the path from base to target with symlinks resolved, as
// pnpm walks it. Only base's longest existing ancestor is resolved.
func physicalRel(base, target string) (string, error) {
	target, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}

	ancestor := base

	var tail []string

	for {
		if _, statErr := os.Stat(ancestor); statErr == nil {
			break
		}

		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("no existing ancestor of %s", base)
		}

		tail = append([]string{filepath.Base(ancestor)}, tail...)
		ancestor = parent
	}

	ancestor, err = filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(filepath.Join(append([]string{ancestor}, tail...)...), target)
	if err != nil {
		return "", err
	}

	return filepath.ToSlash(rel), nil
}

// fill adds each contribution to the plan in table order. An empty id or
// title takes the module's name or title.
func fill(plan *scaffold.Plan, contributions map[manifest.SlotName]scaffold.Contribution) {
	for _, row := range manifest.Slots() {
		held, makes := contributions[row.Name]
		if !makes {
			continue
		}

		plan.Contributions = append(plan.Contributions, scaffold.Contribution{
			Slot:        row.Name,
			ID:          firstOf(held.ID, plan.Name),
			Title:       firstOf(held.Title, plan.Title),
			Icon:        held.Icon,
			Destructive: held.Destructive,
		})
	}
}

// firstOf returns given trimmed, or fallback when given is blank.
func firstOf(given, fallback string) string {
	if strings.TrimSpace(given) != "" {
		return strings.TrimSpace(given)
	}

	return fallback
}

// titleOf returns the given title, or the name with hyphens as spaces and
// each word capitalised.
func titleOf(given, name string) string {
	if strings.TrimSpace(given) != "" {
		return strings.TrimSpace(given)
	}

	words := strings.Split(strings.ReplaceAll(name, "-", " "), " ")
	for index, word := range words {
		if word == "" {
			continue
		}

		letters := []rune(word)
		letters[0] = unicode.ToUpper(letters[0])
		words[index] = string(letters)
	}

	return strings.Join(words, " ")
}

// placeFor resolves the checkout and the module's directory. The default is
// <name> beside the checkout, never inside it. A bad -checkout is an error.
func placeFor(request answers, name string) (string, string, error) {
	checkout, err := scaffold.FindCheckout(request.checkout, request.workDir)

	switch {
	case err == nil:
	case request.checkout == "" && errors.Is(err, scaffold.ErrNoCheckout):
		checkout = ""
	default:
		return "", "", fmt.Errorf("%w", err)
	}

	dir := strings.TrimSpace(request.dir)

	switch {
	case dir != "" && !filepath.IsAbs(dir):
		dir = filepath.Join(request.workDir, dir)
	case dir == "" && checkout != "":
		dir = filepath.Join(filepath.Dir(filepath.FromSlash(checkout)), name)
	case dir == "":
		dir = filepath.Join(request.workDir, name)
	}

	return checkout, filepath.Clean(dir), nil
}

// versionsFor returns the Go version and the component library range: the
// checkout's when there is one, the published ones otherwise.
func versionsFor(checkout string) (string, string, error) {
	if checkout == "" {
		library, err := scaffold.RangeOf(publishedPackages)
		if err != nil {
			return "", "", fmt.Errorf("%w", err)
		}

		return publishedGo, library, nil
	}

	goVersion, err := scaffold.GoVersion(checkout)
	if err != nil {
		return "", "", fmt.Errorf("%w", err)
	}

	library, err := scaffold.LibraryRange(checkout)
	if err != nil {
		return "", "", fmt.Errorf("%w", err)
	}

	return goVersion, library, nil
}

// defaultDir is the module's directory when -dir is empty, as shown prints
// it.
func defaultDir(request answers, name string) string {
	request.dir = ""

	_, dir, err := placeFor(request, name)
	if err != nil {
		return name
	}

	return shown(request.workDir, dir)
}
