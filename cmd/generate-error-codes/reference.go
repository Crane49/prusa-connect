package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	frontMatter = "---\ntitle: Error codes\n" +
		"description: Every error code a client can see, what it means, what to do about it, " +
		"and the parameters it interpolates.\n" +
		"audience: run\n" +
		"rewrite:\n  describes: current\n  implementedBy:\n    - sdk/fault\n---\n\n"

	preamble = "Every failure a client can see carries `code`, a stable `domain/REASON` identifier, next to an " +
		"English `title`. Branch on `code`. Fall back to `title` for a code the client does not know, and quote " +
		"`trace_id` when reporting one.\n\n" +
		"A refusal answers with the values it interpolates, under `params`. The **parameters** line on each " +
		"code below names the keys that member carries, and it is a promise a client may read. Read them " +
		"rather than parsing the `detail` sentence, which is English prose and is not a contract. A refusal " +
		"about something the caller named puts what it named there: a 403 says which action was missing and " +
		"on which object.\n\n" +
		"A redacted code renders `code`, `title`, `status` and `trace_id` and nothing else. Its occurrence detail " +
		"and its params stay in the server log line that the trace id keys.\n\n" +
		"A retryable code is worth repeating with backoff. Every other code fails the same way however often it is " +
		"sent.\n\n" +
		"Each code below is its own section, anchored by the code itself: `" + exampleCode + "` is at " +
		"`#" + exampleAnchor + "`, so a link to one code can be written from a response without opening this " +
		"page.\n\n" +
		"Every refusal carries that address already, under `type`. It is this page and the code's own " +
		"section of it, so a screen that has just failed can offer it as a help link and the person lands " +
		"on what went wrong and what to do about it.\n\n" +
		"The machine-readable copy of this page is `" + defaultRegistryPath + "`.\n"

	// paramKeysSection heads the published key set.
	paramKeysSection = "\n## Parameter keys\n\n" +
		"Every key a `params` object can carry is in this set, and a code that declares a key outside it " +
		"fails the build. A key names one fact: the instance a refusal is about is `resource`, carrying " +
		"the kind, a colon and the name. A key holding a bound names its unit or its subject, so a byte " +
		"count is `limit_bytes` and a count of streams is `limit_streams`.\n\n"

	// The example in the preamble. It must be a registered code.
	exampleCode   = "core.printer/GCODE_NOT_ALLOWED"
	exampleAnchor = "core-printer-gcode-not-allowed"
)

// errDuplicateAnchor means two entries share one anchor. Anchors are linked
// from outside, so a collision is an error and is never renamed away.
var errDuplicateAnchor = errors.New("two entries share one anchor")

// renderReference writes the page. Entries must arrive sorted by code, so one
// pass groups them by domain.
func renderReference(entries []fault.Entry) ([]byte, error) {
	var page strings.Builder

	page.WriteString(frontMatter)
	page.WriteString(generatedNotice)
	page.WriteString("\n\n")
	page.WriteString(preamble)
	page.WriteString(paramKeysSection)
	page.WriteString(paramList(fault.ParamKeys()))
	page.WriteString("\n")

	if len(entries) == 0 {
		page.WriteString("\nNo codes are registered.\n")

		return []byte(page.String()), nil
	}

	taken := make(map[string]string, len(entries))
	domain := ""

	for _, entry := range entries {
		anchor := entry.Code.Anchor()
		if owner, clash := taken[anchor]; clash {
			return nil, fmt.Errorf("%w: %s and %s both anchor at #%s", errDuplicateAnchor, owner, entry.Code, anchor)
		}

		taken[anchor] = entry.Code.String()

		if entry.Code.DomainName() != domain {
			domain = entry.Code.DomainName()
			fmt.Fprintf(&page, "\n## %s\n", domain)
		}

		writeEntry(&page, entry, anchor)
	}

	return []byte(page.String()), nil
}

func writeEntry(page *strings.Builder, entry fault.Entry, anchor string) {
	fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", entry.Code, anchor)
	fmt.Fprintf(page, "%s.\n\n", strings.TrimSuffix(docsref.EscapeProse(entry.Summary), "."))
	page.WriteString(docsref.DeprecationNotice(entry.Deprecated))
	fmt.Fprintf(page, "- **status** `%d`\n", entry.Status)
	fmt.Fprintf(page, "- **retryable** %s\n", docsref.YesNo(entry.Retry == fault.RetryAllowed))
	fmt.Fprintf(page, "- **occurrence detail** %s\n", detailOf(entry.Redaction))
	fmt.Fprintf(page, "- **parameters** %s\n", paramList(entry.Params))
	fmt.Fprintf(page, "- **what to do** %s\n", docsref.EscapeProse(entry.Action))
}

func paramList(params []string) string {
	if len(params) == 0 {
		return docsref.None
	}

	quoted := make([]string, 0, len(params))
	for _, param := range params {
		quoted = append(quoted, "`"+param+"`")
	}

	return strings.Join(quoted, ", ")
}

func detailOf(redaction fault.Redaction) string {
	if redaction == fault.RedactDetail {
		return "redacted"
	}

	return "shown"
}
