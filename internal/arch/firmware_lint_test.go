package arch_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// firmwareLabelString is the name forbidigo resolves a call to
// connectorapi.FirmwareLabel.String into once analyze-types is on. The rule in
// .golangci.yml is compiled here and matched against it, so this gate fails on a
// pattern that no longer catches the call rather than on a line of yaml that
// changed shape.
const firmwareLabelString = firmwareLabelPackage + ".FirmwareLabel.String"

// firmwareTextReaders are the two packages connectorapi.FirmwareLabel names as
// entitled to the reported text: the resolver that selects a profile, and the
// registry it selects from. Every other package asks the resolved capability
// set.
var firmwareTextReaders = []string{
	"internal/printer/profile",
	"internal/printer/capability",
}

// nolintDirective matches a suppression comment and captures the linters it
// names, empty for the bare form that suppresses all of them.
var nolintDirective = regexp.MustCompile(`//\s*nolint(?::([^\s]*))?`)

// TestTheFirmwareLabelGateIsArmed holds the lint rule that forbids reading a
// firmware label's text, because a lint rule is the one gate in this repository
// that a single line of source can switch off.
//
// It is not a second copy of TestFirmwareTextIsNeverOrdered. That one refuses
// <, >, <= and >= on any String result and says what it does not reach: a sort
// written over strings.Compare, and any other use of the text that is not an
// operator. Both escapes were measured on this tree: adding
//
//	//nolint:forbidigo // ...
//	return a.Firmware.String() + b.Firmware.String()
//
// to internal/connector/xbuddy passes golangci-lint and passes the ordering gate
// too. What is left to hold is that the type-aware rule stays armed and stays
// unsuppressed, which is what this checks.
func TestTheFirmwareLabelGateIsArmed(t *testing.T) {
	t.Parallel()

	config := readLintConfig(t)

	require.Contains(t, config.Linters.Enable, "forbidigo",
		"forbidigo is the only rule that can name a method of one type, and it is not enabled")

	require.True(t, config.Linters.Settings.Forbidigo.AnalyzeTypes,
		"without analyze-types forbidigo matches source text, so the rule below catches every String method "+
			"and none of them")

	matching := 0

	for _, rule := range config.Linters.Settings.Forbidigo.Forbid {
		if regexp.MustCompile(rule.Pattern).MatchString(firmwareLabelString) {
			matching++
		}
	}

	requireScanned(t, "forbidigo patterns matching "+firmwareLabelString, matching, 1,
		"With none, reading a reported firmware version is forbidden nowhere.")

	assertNoExclusionReachesAnotherPackage(t, config)
}

// TestNothingSuppressesTheFirmwareLabelGate is the other half: a rule that is
// armed and suppressed at the one call site that wanted it is not armed.
func TestNothingSuppressesTheFirmwareLabelGate(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	for _, dir := range packagesThatCanHoldAFirmwareLabel(t) {
		relative := relativeDir(t, root, dir)
		if slices.Contains(firmwareTextReaders, relative) {
			continue
		}

		for _, line := range suppressionsIn(t, dir) {
			t.Errorf("%s suppresses forbidigo, and it is not one of the packages entitled to read a "+
				"reported firmware version (%s). Ask the resolved capability set instead of the text",
				line, strings.Join(firmwareTextReaders, ", "))
		}
	}
}

// assertNoExclusionReachesAnotherPackage checks the other way the rule can be
// turned off: an exclusion in the same file, widened until it covers a package
// that has no business reading the text.
func assertNoExclusionReachesAnotherPackage(t *testing.T, config lintConfig) {
	t.Helper()

	root := repoRoot(t)

	for _, rule := range config.Linters.Exclusions.Rules {
		if !slices.Contains(rule.Linters, "forbidigo") {
			continue
		}

		path := regexp.MustCompile(rule.Path)

		for _, dir := range packagesThatCanHoldAFirmwareLabel(t) {
			relative := relativeDir(t, root, dir)
			if slices.Contains(firmwareTextReaders, relative) || !path.MatchString(relative+"/") {
				continue
			}

			t.Errorf("the forbidigo exclusion for %q also covers %s, which is not one of the packages "+
				"entitled to read a reported firmware version", rule.Path, relative)
		}
	}
}

// suppressionsIn returns every nolint directive in one package's own files that
// switches forbidigo off, named by file and line.
func suppressionsIn(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var found []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)

		for number, line := range strings.Split(string(content), "\n") {
			if !suppressesForbidigo(line) {
				continue
			}

			found = append(found, filepath.ToSlash(path)+":"+strconv.Itoa(number+1))
		}
	}

	return found
}

// suppressesForbidigo reports whether one line carries a nolint directive that
// covers forbidigo, which the bare form does as surely as the named one.
func suppressesForbidigo(line string) bool {
	match := nolintDirective.FindStringSubmatch(line)
	if match == nil {
		return false
	}

	if match[1] == "" {
		return true
	}

	for name := range strings.SplitSeq(match[1], ",") {
		if strings.TrimSpace(name) == "forbidigo" {
			return true
		}
	}

	return false
}

func relativeDir(t *testing.T, root, dir string) string {
	t.Helper()

	relative, err := filepath.Rel(root, dir)
	require.NoError(t, err)

	return filepath.ToSlash(relative)
}

// lintConfig is what this gate needs from .golangci.yml and nothing more.
type lintConfig struct {
	Linters struct {
		Enable   []string `yaml:"enable"`
		Settings struct {
			Forbidigo struct {
				AnalyzeTypes bool         `yaml:"analyze-types"`
				Forbid       []forbidRule `yaml:"forbid"`
			} `yaml:"forbidigo"`
		} `yaml:"settings"`
		Exclusions struct {
			Rules []exclusionRule `yaml:"rules"`
		} `yaml:"exclusions"`
	} `yaml:"linters"`
}

type forbidRule struct {
	Pattern string `yaml:"pattern"`
	Msg     string `yaml:"msg"`
}

type exclusionRule struct {
	Path    string   `yaml:"path"`
	Text    string   `yaml:"text"`
	Linters []string `yaml:"linters"`
}

func readLintConfig(t *testing.T) lintConfig {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(repoRoot(t), ".golangci.yml"))
	require.NoError(t, err)

	var config lintConfig

	require.NoError(t, yaml.Unmarshal(content, &config))

	return config
}
