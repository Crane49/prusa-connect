package main

import (
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
)

const (
	frontMatter = "---\ntitle: Device issues\n" +
		"description: The closed set of reasons a device needs attention, what each one means, and " +
		"what a person does about it.\n" +
		"audience: run\n" +
		"rewrite:\n  describes: current\n  implementedBy:\n    - internal/kernel/deviceissue\n" +
		"    - internal/app\n---\n\n"

	preamble = "Every device record carries an `issues` array. Each entry is a `reason` from the " +
		"closed set below and the `severity` that reason is declared with, and that is all it " +
		"carries. The sentence a person reads is here, so a client branches on the token and a " +
		"translator translates this page rather than the wire.\n\n" +
		"The array is ordered, worst first, so the first entry is what somebody should read first. " +
		"An empty array is a device with nothing wrong.\n\n" +
		"An issue is what this server has found, not what the machine said. The machine's own words " +
		"travel in `detail` when a reason has one, and they are shown to a person rather than " +
		"branched on: a firmware may spell one this build has never seen.\n"

	rollupSection = "\n## The rollup on a fleet listing\n\n" +
		"`GET /api/v1/devices` carries an `issues` member beside the page, holding `affected` and a " +
		"`counts` key for every reason below. It is over every printer the caller may see, whatever " +
		"page was asked for and whether or not `with_issues` narrowed it, so one request loads a " +
		"page and the badge that goes beside it.\n\n" +
		"`with_issues=true` narrows the page to the printers carrying at least one issue. It costs " +
		"no extra query: an issue is read from what the server already holds for a device it has " +
		"been dialled by.\n"
)

// renderReference renders the reference page.
func renderReference(entries []deviceissue.Doc) []byte {
	var page strings.Builder

	page.WriteString(frontMatter)
	page.WriteString(generatedNotice)
	page.WriteString("\n\n")
	page.WriteString(preamble)

	writeVersion(&page)
	writeSeverities(&page)
	writeReasons(&page, entries)
	page.WriteString(rollupSection)
	writeAPI(&page)

	return []byte(page.String())
}

// writeReasons renders one anchored section per reason.
func writeReasons(page *strings.Builder, entries []deviceissue.Doc) {
	page.WriteString("\n## The set\n\n")
	page.WriteString("Each reason below is its own section, anchored by the token, so a client's " +
		"comment can link to the reason it handles.\n")

	for _, entry := range entries {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", entry.Reason, docsref.Anchor(entry.Reason.String()))
		fmt.Fprintf(page, "%s\n\n", docsref.EscapeProse(entry.Summary))
		page.WriteString(docsref.DeprecationNotice(entry.Deprecated))
		fmt.Fprintf(page, "- **severity** `%s`\n\n", entry.Severity)
		fmt.Fprintf(page, "%s\n\n", docsref.EscapeProse(entry.Description))
		fmt.Fprintf(page, "**What to do:** %s\n", docsref.EscapeProse(entry.Action))
	}
}

func writeVersion(page *strings.Builder) {
	page.WriteString("\n## Vocabulary version\n\n")
	fmt.Fprintf(page, "This build carries `%s` version `%s`, in format `%s`. The content version "+
		"moves when a reason is added, retired or redefined; the format version moves when the shape "+
		"of an entry changes.\n",
		deviceissue.VocabularyName, deviceissue.VocabularyVersion, deviceissue.VocabularyFormat)
}

// writeSeverities renders the severities in order. A client needs the order
// to pick the worse of two reasons on one device.
func writeSeverities(page *strings.Builder) {
	page.WriteString("\n## Severities\n\n")
	page.WriteString("A severity belongs to the reason and never to one device, so every record " +
		"carrying a reason carries the same severity. They are listed least to most urgent, and a " +
		"device showing one badge shows the highest its issues carry.\n\n")

	for _, severity := range deviceissue.Severities() {
		fmt.Fprintf(page, "1. `%s`\n", severity)
	}
}

func writeAPI(page *strings.Builder) {
	page.WriteString("\n## Reading the set from a running server\n\n")
	page.WriteString("`GET /api/v1/vocabularies/device-issues` answers with what **this build** " +
		"raises, from the same table this page is generated from. A client reads it before it holds " +
		"a credential, which is why it takes none.\n")
}
