package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The files this gate reads. The baseline is the surface the tree last
// accepted; the artefacts are what the generators wrote, and each of them is
// already gated against its Go table in both directions.
const (
	defaultBaseline  = "tools/vocabcompat/baseline.json"
	defaultArtefacts = "docs/vocabularies"
)

// Exit codes. A refusal is separated from a usage error so a runner can tell a
// broken invocation from a vocabulary that moved without saying so.
const (
	exitOK      = 0
	exitRefused = 1
	exitUsage   = 2
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("vocabcompat", flag.ContinueOnError)
	flags.SetOutput(stderr)

	root := flags.String("root", ".", "the repository root the other paths are relative to")
	baselinePath := flags.String("baseline", defaultBaseline, "the sets the tree last accepted")
	artefactDir := flags.String("artefacts", defaultArtefacts, "the generated vocabulary artefacts")
	versionPath := flags.String("version-file", defaultVersion, "the release this build is, which decides the freeze")

	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	command := "check"
	if rest := flags.Args(); len(rest) > 0 {
		command = rest[0]
	}

	paths := locations{
		baseline:  filepath.Join(*root, *baselinePath),
		artefacts: filepath.Join(*root, *artefactDir),
		release:   filepath.Join(*root, *versionPath),
	}

	switch command {
	case "check":
		return check(stdout, stderr, paths)
	case "accept":
		return accept(stdout, stderr, paths)
	default:
		fmt.Fprintf(stderr, "vocabcompat: %q is not a command. Use check or accept.\n", command)

		return exitUsage
	}
}

// locations is where the three inputs live.
type locations struct {
	baseline, artefacts, release string
}

// check compares every artefact against the baseline and requires the version
// each difference demands.
func check(stdout, stderr io.Writer, paths locations) int {
	was, now, release, err := load(paths)
	if err != nil {
		fmt.Fprintf(stderr, "vocabcompat: %v\n", err)

		return exitUsage
	}

	fmt.Fprintf(stdout, "vocabcompat: %d vocabularies, release %s, and the freeze floor is %s\n",
		len(now), release.text, freezeFloor)

	for _, name := range was.Unversioned {
		fmt.Fprintf(stdout, "  %s publishes no version. %s\n", name.Vocabulary, name.Why)
	}

	var refusals []string

	for _, name := range sortedNames(now) {
		refusals = append(refusals, judge(stdout, name, was.Vocabularies[name], now[name], release)...)
	}

	if len(refusals) > 0 {
		fmt.Fprintf(stdout, "\nrefused:\n  %s\n", strings.Join(refusals, "\n  "))
		fmt.Fprintln(stdout, "\nMove the version in the Go constant, regenerate, and run `make vocab-accept` "+
			"to move the baseline. See docs/decisions/20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md.")

		return exitRefused
	}

	fmt.Fprintln(stdout, "\naccepted. Every set that moved carries the version its movement demands.")

	return exitOK
}

// judge reports every reason one vocabulary is refused, and prints what moved.
func judge(out io.Writer, name string, was, now surface, release version) []string {
	found := differences(name, was, now)

	content, format, breaking := demanded(found)

	fmt.Fprintf(out, "\n%s: version %s to %s, format %s to %s, %d differences\n",
		name, was.Version, now.Version, was.Format, now.Format, len(found))

	for _, item := range found {
		fmt.Fprintf(out, "  [%s] %s\n", item.Kind, item.Detail)
	}

	var refusals []string

	refusals = append(refusals, holds(name, "version", was.Version, now.Version, content)...)
	refusals = append(refusals, holdsFormat(name, was.Format, now.Format, format)...)

	if breaking && release.frozen() {
		refusals = append(refusals, fmt.Sprintf(
			"%s: something left the published set, and VERSION reads %s, at or above %s. "+
				"A frozen set is added to and never subtracted from",
			name, release.text, freezeFloor))
	}

	for _, item := range found {
		switch {
		case item.Kind == "deprecation-withdrawn":
			refusals = append(refusals, fmt.Sprintf(
				"%s: the deprecation of %s is gone and the entry is still published. A set that "+
					"announced a removal does not take it back by regenerating. Restore the marker, or "+
					"run `make vocab-accept` to publish the reversal on purpose",
				name, item.Detail))
		case item.Retirement && release.frozen():
			refusals = append(refusals, retirementHolds(name, item, release)...)
		}
	}

	return refusals
}

// retirementHolds is what a frozen release asks of an entry that left a
// published set: an earlier release announced it, and the window since that
// release has passed. Below the freeze floor nothing calls this, because no
// release has published the set and there is no client to warn.
func retirementHolds(name string, item finding, release version) []string {
	if item.Since == "" {
		return []string{fmt.Sprintf(
			"%s: %s left the published set with no deprecation, and VERSION reads %s, at or above %s. "+
				"An entry leaves in two releases: one that publishes a deprecated marker naming the "+
				"release and the replacement, and a later one that removes it",
			name, item.Detail, release.text, freezeFloor)}
	}

	since, err := parseVersion(name+" "+item.Detail+" deprecated since", item.Since)
	if err != nil {
		return []string{err.Error()}
	}

	if release.compare(since) < 0 {
		return []string{fmt.Sprintf(
			"%s: %s was deprecated in %s, which is ahead of VERSION %s. The window is counted from "+
				"the release the marker shipped in",
			name, item.Detail, item.Since, release.text)}
	}

	if windowPassed(since, release) {
		return nil
	}

	return []string{fmt.Sprintf(
		"%s: %s was deprecated in %s and VERSION reads %s. A deprecated entry stays published for "+
			"%d minor releases, so it may be removed from %d.%d.0 onwards",
		name, item.Detail, item.Since, release.text,
		deprecationWindow, since.major, since.minor+deprecationWindow)}
}

// windowPassed reports whether enough of the application's minor releases have
// gone by since the deprecation shipped. A new major carries the whole window
// with it, because everything in it is a later release than the one that
// announced the removal.
func windowPassed(since, release version) bool {
	if release.major != since.major {
		return release.major > since.major
	}

	return release.minor >= since.minor+deprecationWindow
}

// holds checks one number against the bump its differences demand.
func holds(name, which, wasText, nowText string, demand int) []string {
	was, err := parseVersion(name+" "+which+" in the baseline", wasText)
	if err != nil {
		return []string{err.Error()}
	}

	now, err := parseVersion(name+" "+which+" in the artefact", nowText)
	if err != nil {
		return []string{err.Error()}
	}

	if now.compare(was) < 0 {
		return []string{fmt.Sprintf("%s: the %s went backwards, from %s to %s", name, which, wasText, nowText)}
	}

	if satisfied(was, now, demand) {
		return nil
	}

	return []string{fmt.Sprintf(
		"%s: the %s reads %s and the baseline reads %s, and what moved demands %s bump",
		name, which, nowText, wasText, bumpNames[demand])}
}

// holdsFormat is holds for the artefact's own shape version, which is two
// numbers rather than three.
func holdsFormat(name, wasText, nowText string, demand int) []string {
	was, err := parseFormat(name+" format in the baseline", wasText)
	if err != nil {
		return []string{err.Error()}
	}

	now, err := parseFormat(name+" format in the artefact", nowText)
	if err != nil {
		return []string{err.Error()}
	}

	if now.compare(was) < 0 {
		return []string{fmt.Sprintf("%s: the format went backwards, from %s to %s", name, wasText, nowText)}
	}

	if satisfied(was, now, demand) {
		return nil
	}

	return []string{fmt.Sprintf(
		"%s: the format reads %s and the baseline reads %s, and what moved demands %s bump",
		name, nowText, wasText, bumpNames[demand])}
}

// demanded folds the findings into the largest bump each number owes, and
// whether anything left the published surface.
func demanded(found []finding) (content, format int, breaking bool) {
	for _, item := range found {
		if item.Content > content {
			content = item.Content
		}

		if item.Format > format {
			format = item.Format
		}

		// A retirement is breaking and is judged by the deprecation that
		// announced it, so it is not what the blanket freeze refusal reads.
		breaking = breaking || (item.Breaking && !item.Retirement)
	}

	return content, format, breaking
}

// load reads the baseline, every artefact and the release, and refuses a tree
// where the two sides do not name the same vocabularies.
func load(paths locations) (baseline, map[string]surface, version, error) {
	was, err := readBaseline(paths.baseline)
	if err != nil {
		return baseline{}, nil, version{}, err
	}

	release, err := readRelease(paths.release)
	if err != nil {
		return baseline{}, nil, version{}, err
	}

	now, err := readArtefacts(paths.artefacts, was)
	if err != nil {
		return baseline{}, nil, version{}, err
	}

	for name := range was.Vocabularies {
		if _, held := now[name]; !held {
			return baseline{}, nil, version{}, fmt.Errorf(
				"the baseline names %q and %s holds no artefact for it. A published set does not "+
					"stop being published by losing its file", name, paths.artefacts)
		}
	}

	return was, now, release, nil
}

// readArtefacts reads every vocabulary artefact, keyed by the name inside it.
//
// A file the baseline does not name is refused rather than accepted, because a
// vocabulary that ships with no baseline is a version nothing holds, which is
// what ADR-20260827-qvk exists to stop.
func readArtefacts(dir string, was baseline) (map[string]surface, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	out := map[string]surface{}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		known, held := was.Vocabularies[strings.TrimSuffix(entry.Name(), ".json")]
		if !held {
			return nil, fmt.Errorf(
				"%s is not in the baseline. Every published vocabulary carries a version this gate "+
					"holds to its set.\n"+
					"Add a stanza for it to the baseline, naming each set's \"key\", then run "+
					"`make vocab-accept` to fill in the entries", path)
		}

		shape, name, readErr := readSurface(path, keysOf(known))
		if readErr != nil {
			return nil, readErr
		}

		if want := strings.TrimSuffix(entry.Name(), ".json"); name != want {
			return nil, fmt.Errorf("%s names itself %q, and the baseline knows it as %q", path, name, want)
		}

		out[name] = shape
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%s holds no vocabulary artefact, so this gate would pass over nothing", dir)
	}

	return out, nil
}

// keysOf is the identity fields the baseline gives each set.
func keysOf(known surface) map[string][]string {
	out := make(map[string][]string, len(known.Sets))
	for name, set := range known.Sets {
		out[name] = set.Key
	}

	return out
}

// sortedNames returns the vocabulary names in order.
func sortedNames(surfaces map[string]surface) []string {
	out := make([]string, 0, len(surfaces))
	for name := range surfaces {
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}

// accept rewrites the baseline from the artefacts. It is the deliberate half of
// the gate: a set that moved is accepted by a diff a reviewer reads next to the
// changelog entry it needs, never by a flag on the check.
func accept(stdout, stderr io.Writer, paths locations) int {
	was, now, _, err := load(paths)
	if err != nil {
		fmt.Fprintf(stderr, "vocabcompat: %v\n", err)

		return exitUsage
	}

	was.Vocabularies = now

	data, err := json.MarshalIndent(was, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "vocabcompat: render the baseline: %v\n", err)

		return exitUsage
	}

	if err = os.WriteFile(paths.baseline, append(data, '\n'), 0o600); err != nil {
		fmt.Fprintf(stderr, "vocabcompat: write %s: %v\n", paths.baseline, err)

		return exitUsage
	}

	fmt.Fprintf(stdout, "vocabcompat: %s now holds %d vocabularies\n", paths.baseline, len(now))

	return exitOK
}
