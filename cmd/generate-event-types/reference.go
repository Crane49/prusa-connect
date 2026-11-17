package main

import (
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

const (
	frontMatter = "---\ntitle: Event types\n" +
		"description: The core event types this build publishes, the class that decides how each one " +
		"is queued and propagated, and the rule a type name is held to.\n" +
		"audience: extend\n" +
		"rewrite:\n  describes: current\n  implementedBy:\n    - sdk/event\n" +
		"    - internal/transport/stream\n---\n\n"

	preamble = "Every event on this bus is a CloudEvents 1.0 envelope, and `type` is what a consumer " +
		"routes on. The types below are the ones this build publishes. They are closed: the hub " +
		"refuses a publication naming a type in this project's namespace that is not here.\n\n" +
		"A type outside that namespace belongs to whoever owns it. A plugin declares the events it " +
		"emits in its own descriptor and publishes them under its own prefix, and a federated peer's " +
		"types arrive with the peer. Those are checked for syntax and not for membership, because a " +
		"node forwarding what it does not understand is the point of the class field.\n\n" +
		"`class` is not derived from the type and is never guessed from it. A node that has never " +
		"heard of an event still knows its queue class, its propagation policy and whether expiry " +
		"applies, which is what makes forwarding an unknown event safe.\n"
)

// renderReference writes the page a consumer reads before routing on a type.
func renderReference(entries []event.TypeDoc) []byte {
	var page strings.Builder

	page.WriteString(frontMatter)
	page.WriteString(generatedNotice)
	page.WriteString("\n\n")
	page.WriteString(preamble)

	writeVersion(&page)
	writeNameRule(&page)
	writeTypes(&page, entries)
	writeAPI(&page)

	return []byte(page.String())
}

// writeTypes renders one anchored section per type.
func writeTypes(page *strings.Builder, entries []event.TypeDoc) {
	page.WriteString("\n## The set\n\n")
	page.WriteString("Each type below is its own section, anchored by the type name, so a router's comment can " +
		"link to the type it handles.\n")

	for _, entry := range entries {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", entry.Type, docsref.Anchor(entry.Type))
		fmt.Fprintf(page, "%s.\n\n", strings.TrimSuffix(docsref.EscapeProse(entry.Summary), "."))
		page.WriteString(docsref.DeprecationNotice(entry.Deprecated))
		fmt.Fprintf(page, "- **class** `%s`\n\n", entry.Class)
		fmt.Fprintf(page, "%s\n", docsref.EscapeProse(entry.Description))
	}
}

func writeVersion(page *strings.Builder) {
	page.WriteString("\n## Vocabulary version\n\n")
	fmt.Fprintf(page, "This build carries `%s` version `%s`, in format `%s`, and closes the namespace "+
		"`%s`. The content version moves when a type is added, retired or redefined; the format "+
		"version moves when the shape of an entry changes.\n",
		event.VocabularyName, event.VocabularyVersion, event.VocabularyFormat, event.CoreNamespace)
}

func writeNameRule(page *strings.Builder) {
	page.WriteString("\n## What a type name may be\n\n")
	page.WriteString("Dot-separated segments, each of lowercase letters, digits, `-` and `_`, with the " +
		"hyphen and the underscore inside a segment rather than at either end. At least two segments, " +
		"at most 128 bytes, and no other character at all.\n\n")
	page.WriteString("This is a security rule and not a tidiness one. A type is written verbatim into " +
		"an SSE `event:` field and into a messaging subject, so a name carrying a newline would write " +
		"a frame of its own into every reader's stream. Every transport that carries an envelope " +
		"applies the rule before the name reaches a wire.\n")
}

func writeAPI(page *strings.Builder) {
	page.WriteString("\n## Reading the set from a running server\n\n")
	page.WriteString("`GET /api/v1/vocabularies/event-types` answers with what **this build** " +
		"publishes, from the same table this page is generated from. A consumer writing a router " +
		"reads it before it holds a credential, which is why it takes none.\n")
}
