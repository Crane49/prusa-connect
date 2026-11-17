package main

import (
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

const (
	frontMatter = "---\ntitle: Extensions\n" +
		"description: Every extension point this build serves, what is registered on each, " +
		"the config keys it accepts, the events it emits and where it publishes.\n" +
		"audience: extend\n" +
		"rewrite:\n  describes: current\n  implementedBy:\n    - sdk/extension\n---\n\n"

	preamble = "An extension registers itself from an `init` function that a distribution build reaches through a " +
		"blank import. The host derives the namespace from the registering module, so a name is " +
		"`namespace/name` and two authors cannot land on one name by choosing it. Configuration may write the " +
		"bare name while only one namespace answers to it, and must write the qualified name once two do.\n\n" +
		"Every entry carries the same descriptor. `version` is the implementation's own; `contract` is the " +
		"version of the extension contract it was compiled against, and a host serving another major refuses " +
		"it at registration rather than at first use. `stability` says what may change between releases: an " +
		"experimental entry may change in any release, a stable one needs a major release and a migration " +
		"note, and a deprecated one names what replaces it.\n\n" +
		"A registration missing any mandatory field does not compile into a working binary, so nothing on this " +
		"page is a placeholder.\n\n" +
		"A section a kind has no registration for is absent rather than empty. Route sets are absent and stay " +
		"absent: the in-process contract that registered them is deleted, and a module is declared in " +
		"configuration rather than compiled in, so the host serves its routes and its state store at " +
		"`/api/v1/modules/<namespace>/` without it registering here.\n\n" +
		"The machine-readable copy of this page is `" + defaultCataloguePath + "`.\n"
)

// renderReference writes the page. Entries must arrive sorted by kind and
// then by qualified name, so one pass groups them by kind.
func renderReference(entries []extension.Descriptor) []byte {
	var page strings.Builder

	page.WriteString(frontMatter)
	page.WriteString(generatedNotice)
	page.WriteString("\n\n")
	page.WriteString(preamble)

	if len(entries) == 0 {
		page.WriteString("\nNothing is registered.\n")

		return []byte(page.String())
	}

	kind := extension.Kind("")

	for _, entry := range entries {
		if entry.Kind() != kind {
			kind = entry.Kind()
			writeKindHeading(&page, kind)
		}

		writeEntry(&page, entry)
	}

	return []byte(page.String())
}

func writeKindHeading(page *strings.Builder, kind extension.Kind) {
	fmt.Fprintf(page, "\n## %s\n", headingOf(kind))

	if contract := contractOf(kind); contract != "" {
		fmt.Fprintf(page, "\nWritten against `%s`.\n", contract)
	}
}

func writeEntry(page *strings.Builder, entry extension.Descriptor) {
	doc := entry.Doc()

	fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", entry.Qualified(), docsref.Anchor(entry.Qualified()))
	fmt.Fprintf(page, "%s\n\n", docsref.EscapeProse(doc.Summary))

	page.WriteString(docsref.DeprecationNotice(doc.Deprecated))
	fmt.Fprintf(page, "- **config name** `%s`\n", docsref.EscapeSpan(entry.Name()))
	fmt.Fprintf(page, "- **qualified name** `%s`\n", docsref.EscapeSpan(entry.Qualified()))
	fmt.Fprintf(page, "- **module** `%s`\n", docsref.EscapeSpan(entry.Module()))
	fmt.Fprintf(page, "- **version** %s\n", doc.Version)
	fmt.Fprintf(page, "- **contract** %s\n", doc.Contract)
	fmt.Fprintf(page, "- **stability** %s\n", doc.Stability)

	if doc.ReplacedBy != "" {
		fmt.Fprintf(page, "- **replaced by** `%s`\n", docsref.EscapeSpan(doc.ReplacedBy))
	}

	fmt.Fprintf(page, "\n%s\n", docsref.EscapeProse(doc.Description))

	writeOptions(page, doc.Options)
	writeEmissions(page, doc.Emits)
	writeDestinations(page, doc.Publishes)
	writeRequirements(page, doc.Requires)
}

func writeOptions(page *strings.Builder, options []extension.Option) {
	if len(options) == 0 {
		return
	}

	page.WriteString("\n#### Configuration\n\n")

	for _, option := range options {
		fmt.Fprintf(page, "- **`%s`** %s, %s, default %s. %s\n",
			docsref.EscapeSpan(option.Key), option.Type, requiredWord(option.Required),
			defaultOf(option), docsref.EscapeProse(option.Summary))
	}
}

func writeEmissions(page *strings.Builder, emissions []extension.Emission) {
	if len(emissions) == 0 {
		return
	}

	page.WriteString("\n#### Events emitted\n\n")

	for _, emission := range emissions {
		fmt.Fprintf(page, "- **`%s`** version %s. %s\n",
			docsref.EscapeSpan(emission.Type), emission.Version, docsref.EscapeProse(emission.Summary))
	}
}

// writeDestinations lists the addresses an entry publishes to on its own
// transport. They are not event types of this server's stream.
func writeDestinations(page *strings.Builder, destinations []extension.Destination) {
	if len(destinations) == 0 {
		return
	}

	page.WriteString("\n#### Published on its own transport\n\n")

	for _, destination := range destinations {
		fmt.Fprintf(page, "- **`%s`** version %s. %s\n",
			docsref.EscapeSpan(destination.Address), destination.Version,
			docsref.EscapeProse(destination.Summary))
	}
}

func writeRequirements(page *strings.Builder, requirements []extension.Requirement) {
	if len(requirements) == 0 {
		return
	}

	page.WriteString("\n#### Required of the host\n\n")

	for _, requirement := range requirements {
		fmt.Fprintf(page, "- %s **`%s`** since %s\n", requirementSubject(requirement),
			docsref.EscapeSpan(requirement.Subject()), requirement.Since)
	}
}

// requirementSubject says whether a requirement is a capability key, a type
// name or a core version range.
func requirementSubject(requirement extension.Requirement) string {
	switch {
	case requirement.Capability != "":
		return "capability"
	case requirement.Type != "":
		return "type"
	}

	return "core"
}

func requiredWord(required bool) string {
	if required {
		return "required"
	}

	return "optional"
}

func defaultOf(option extension.Option) string {
	if option.Default == "" {
		return "none"
	}

	return "`" + docsref.EscapeSpan(option.Default) + "`"
}
