package main

import (
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const (
	frontMatter = "---\ntitle: Resource kinds\n" +
		"description: The closed set of resource kinds this build addresses, which half of it an " +
		"operator declares in configuration, and what each kind is.\n" +
		"audience: extend\n" +
		"rewrite:\n  describes: current\n  implementedBy:\n    - internal/kernel/ref\n---\n\n"

	preamble = "Every instance in this system is named `kind:id`, and the kind comes from the closed set " +
		"below. There is no registration hook: a new kind is a change to the descriptor table in Go " +
		"and to this page in the same commit.\n\n" +
		"The set has two halves. A **configurable** kind is declared in the configuration document and " +
		"created, edited and deleted through `/api/v1/resources`. An **intrinsic** kind names something " +
		"the system already knows, so no operator declares one: a printer arrives when a connector " +
		"admits it, and a node arrives when a link handshake completes. Both halves are addressable, " +
		"because the event envelope, the health key and the authorization key are one vocabulary and a " +
		"second set here would drift from it.\n\n" +
		"An id is a DNS-1123 label: lowercase letters, digits and `-`, starting and ending with a " +
		"letter or a digit, at most 63 bytes. That intersection is what makes one string legal at once " +
		"as a URL path segment, a metric label, a config key and a messaging subject token.\n"
)

func renderReference(entries []ref.KindDoc) []byte {
	var page strings.Builder

	page.WriteString(frontMatter)
	page.WriteString(generatedNotice)
	page.WriteString("\n\n")
	page.WriteString(preamble)

	writeVersion(&page)
	writeKinds(&page, entries)
	writeRuntimes(&page, ref.RuntimeDocs())
	writeAPI(&page)

	return []byte(page.String())
}

func writeVersion(page *strings.Builder) {
	page.WriteString("\n## Vocabulary version\n\n")
	fmt.Fprintf(page, "This build carries `%s` version `%s`, in format `%s`. The content version moves "+
		"when a kind is added, retired or redefined; the format version moves when the shape of an "+
		"entry changes, so a consumer can tell a reshaped artefact from a reworded one.\n",
		ref.VocabularyName, ref.VocabularyVersion, ref.VocabularyFormat)
}

func writeKinds(page *strings.Builder, entries []ref.KindDoc) {
	page.WriteString("\n## The set\n\n")
	page.WriteString("Each kind below is its own section, anchored by the kind name.\n")

	for _, entry := range entries {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", entry.Kind, docsref.Anchor(string(entry.Kind)))
		fmt.Fprintf(page, "%s.\n\n", strings.TrimSuffix(docsref.EscapeProse(entry.Summary), "."))
		page.WriteString(docsref.DeprecationNotice(entry.Deprecated))
		fmt.Fprintf(page, "- **declared in configuration** %s\n", docsref.YesNo(entry.Configurable))
		fmt.Fprintf(page, "- **runtimes** %s\n\n", runtimeList(entry.Runtimes))
		fmt.Fprintf(page, "%s\n", docsref.EscapeProse(entry.Description))
	}
}

func writeRuntimes(page *strings.Builder, runtimes []ref.RuntimeDoc) {
	page.WriteString("\n## Runtimes\n\n")
	page.WriteString("An instance declares one of these under `runtime`. It says what is behind the " +
		"instance, which is a different question from whether the instance runs. A kind refuses a " +
		"runtime it does not offer at load, and the refusal names what it does offer.\n")

	for _, entry := range runtimes {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", entry.Runtime,
			docsref.Anchor(entry.Runtime.String()))
		fmt.Fprintf(page, "%s.\n\n", strings.TrimSuffix(docsref.EscapeProse(entry.Summary), "."))
		fmt.Fprintf(page, "%s\n", docsref.EscapeProse(entry.Description))
	}
}

func runtimeList(offered []ref.Runtime) string {
	if len(offered) == 0 {
		return "none, because no instance of this kind is declared"
	}

	names := make([]string, 0, len(offered))
	for _, runtime := range offered {
		names = append(names, "`"+runtime.String()+"`")
	}

	return strings.Join(names, ", ")
}

func writeAPI(page *strings.Builder) {
	page.WriteString("\n## Reading the set from a running server\n\n")
	page.WriteString("`GET /api/v1/vocabularies/resource-kinds` answers with what **this build** " +
		"carries, from the same table this page is generated from. A page describes the release it " +
		"was generated from; the endpoint describes the binary that is running.\n")
}
