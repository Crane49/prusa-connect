package main

import (
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

const (
	frontMatter = "---\ntitle: Device models\n" +
		"description: The closed set of device models this build carries, what each machine is sold " +
		"as, who makes it and the icon key a page draws it with.\n" +
		"audience: run\n" +
		"rewrite:\n  describes: current\n  implementedBy:\n    - internal/printer/profile\n" +
		"    - internal/transport/http/httpapi\n---\n\n"

	preamble = "A device record carries the machine's own spelling in `model` and nothing else. " +
		"`CoreOneL_INDX` is a firmware string, and a person reading a fleet screen wants the name the " +
		"machine is sold under and a picture of it. This set is the join between the two.\n\n" +
		"The key is the machine's own spelling. It is what a device reports, what the `model` member " +
		"of a device record carries, and what the listing's model filter takes, so a client joins a " +
		"device row onto a row here without a second table.\n\n" +
		"The icon key names a family and not a model. Machines in one family look alike, so a page " +
		"ships a handful of drawings and maps several models onto one. Read it as an open value: a " +
		"newer core may name a key a page has no drawing for, and the answer to that is a generic " +
		"device.\n\n" +
		"It is the set this build carries and not a catalogue of everything Prusa makes. A machine " +
		"reporting a model no row names still connects and is still listed: it runs on its driver's " +
		"baseline instead of a capability profile.\n"
)

// renderReference writes the page a client reads before drawing a device row.
func renderReference(entries []profile.ModelDoc) []byte {
	var page strings.Builder

	page.WriteString(frontMatter)
	page.WriteString(generatedNotice)
	page.WriteString("\n\n")
	page.WriteString(preamble)

	writeVersion(&page)
	writeModels(&page, entries)
	writeAPI(&page)

	return []byte(page.String())
}

// writeModels renders one anchored section per model: the machine's spelling
// first, then the name a person reads and the facts a page needs.
func writeModels(page *strings.Builder, entries []profile.ModelDoc) {
	page.WriteString("\n## The set\n\n")
	page.WriteString("Each model below is its own section, anchored by the machine's own spelling.\n")

	for _, entry := range entries {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", docsref.EscapeSpan(entry.Model), docsref.Anchor(entry.Model))
		fmt.Fprintf(page, "%s\n\n", docsref.EscapeProse(entry.DisplayName))
		page.WriteString(docsref.DeprecationNotice(entry.Deprecated))
		fmt.Fprintf(page, "- **vendor** %s\n", docsref.EscapeProse(entry.Vendor))
		fmt.Fprintf(page, "- **icon** `%s`\n", docsref.EscapeSpan(entry.Icon))
		fmt.Fprintf(page, "- **connector** `%s`\n", docsref.EscapeSpan(entry.Connector))

		if entry.ToolChanger {
			page.WriteString("- **tool changer** yes\n")
		}

		if entry.DeviceTypeID != "" {
			fmt.Fprintf(page, "- **device type code** `%s`\n", docsref.EscapeSpan(entry.DeviceTypeID))
		}
	}
}

func writeVersion(page *strings.Builder) {
	page.WriteString("\n## Vocabulary version\n\n")
	fmt.Fprintf(page, "This build carries `%s` version `%s`, in format `%s`. The content version "+
		"moves when a model is added, retired or redefined; the format version moves when the shape "+
		"of a row changes. A row leaves the set the way every published entry leaves one: it is "+
		"marked deprecated in one release and removed in a later one. Before the server's 1.0.0 "+
		"release, a major version of the set may drop a row without the deprecation.\n",
		profile.VocabularyName, profile.VocabularyVersion, profile.VocabularyFormat)
}

func writeAPI(page *strings.Builder) {
	page.WriteString("\n## Reading the set from a running server\n\n")
	page.WriteString("`GET /api/v1/vocabularies/device-models` answers with what **this build** " +
		"carries, from the same catalogue this page is generated from. It takes no credential.\n\n" +
		"That answer also carries what this page cannot know, because it depends on the modules " +
		"a binary was built with:\n\n" +
		"- a row for each machine a driver in the build declares, with its drawing when the " +
		"driver has one\n" +
		"- each dialect the rows are reached through, and what it needs from a person before a " +
		"machine can arrive\n" +
		"- each declared model the server refused, and why\n")
}
