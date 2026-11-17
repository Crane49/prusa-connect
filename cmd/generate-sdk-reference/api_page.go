package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/codegen/openapidoc"
	"github.com/prusa3d/prusa-connect/internal/docsref"
)

// The API pages: one page per tag of the document, in the document's order,
// with one section per operation. Summaries and descriptions are copied from
// the document unchanged.

const (
	apiDir = "api"

	// errorCodesPage is the link target of a problem status, relative to a
	// page under reference/api/.
	errorCodesPage = "../../error-codes.md"

	apiImplementedBy = "internal/transport/http/httpapi"
	generatorPath    = "cmd/generate-sdk-reference"

	apiFix = "change the route's summary, description or parameters in Go and regenerate."
)

var (
	errUndeclaredTag = errors.New("an operation carries a tag the document does not declare")
	errNoTags        = errors.New("the document declares no tags")
)

type apiInputs struct {
	tags       []openapidoc.Tag
	operations []operation
}

func apiPageName(tag string) string {
	return tag + ".md"
}

// apiPages renders one page per declared tag. An operation with an undeclared
// tag is an error in the document.
func apiPages(referenceDir string, in *apiInputs) ([]page, error) {
	if len(in.tags) == 0 {
		return nil, errNoTags
	}

	byTag := map[string][]operation{}
	for _, t := range in.tags {
		byTag[t.Name] = nil
	}

	for _, op := range in.operations {
		for _, t := range op.Tags {
			if _, declared := byTag[t]; !declared {
				return nil, fmt.Errorf("%w: %s carries %q", errUndeclaredTag, op.ID, t)
			}

			byTag[t] = append(byTag[t], op)
		}
	}

	pages := make([]page, 0, len(in.tags))

	for i, t := range in.tags {
		region, err := renderAPIRegion(t.Name, byTag[t.Name])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", apiPageName(t.Name), err)
		}

		front := frontMatter{
			title:         strings.ToUpper(t.Name[:1]) + t.Name[1:],
			description:   t.Description,
			audience:      "run",
			order:         i + 1,
			implementedBy: []string{apiImplementedBy, generatorPath},
		}

		placeholder := fmt.Sprintf("The `%s` operations of the HTTP API.\n\n%s: write what the resource is, "+
			"how it works and when to use each operation, then delete this line.", t.Name, placeholderMark)

		pages = append(pages, page{
			path:   filepath.Join(referenceDir, apiDir, apiPageName(t.Name)),
			region: region,
			fresh:  freshPage(front, placeholder, region),
		})
	}

	return pages, nil
}

func renderAPIRegion(tag string, ops []operation) ([]byte, error) {
	var body strings.Builder

	body.WriteString("## Operations {#operations}\n")

	if len(ops) == 0 {
		fmt.Fprintf(&body, "\nNo operation carries the `%s` tag.\n", tag)
	}

	taken := anchors{}

	for _, op := range ops {
		if err := taken.claim(docsref.Anchor(op.ID), op.ID); err != nil {
			return nil, err
		}

		if err := writeOperation(&body, op, taken); err != nil {
			return nil, err
		}
	}

	return regionOf("the "+tag+" tag", apiFix, body.String()), nil
}

func writeOperation(body *strings.Builder, op operation, taken anchors) error {
	fmt.Fprintf(body, "\n### `%s` {#%s}\n\n", op.ID, docsref.Anchor(op.ID))
	fmt.Fprintf(body, "`%s %s`.", op.Method, op.Path)

	if op.Summary != "" {
		fmt.Fprintf(body, " %s", sentence(op.Summary))
	}

	body.WriteString("\n")

	if op.Description != "" {
		body.WriteString("\n")
		body.WriteString(docsref.EscapeProse(strings.TrimSpace(op.Description)))
		body.WriteString("\n")
	}

	writeParameters(body, op.Params)

	fmt.Fprintf(body, "\n- credential: %s. The client method is `%s`\n", credentialOf(op), op.Name)
	fmt.Fprintf(body, "- request body: %s\n", bodyOf(op.Body))

	var problems []string

	for _, r := range op.Responses {
		if strings.HasPrefix(r.Status, "2") {
			fmt.Fprintf(body, "- response `%s`: %s\n", r.Status, responseOf(r))

			continue
		}

		problems = append(problems, r.Status)
	}

	fmt.Fprintf(body, "- errors: %s\n", errorsOf(problems))

	return writeSample(body, op, taken)
}

func credentialOf(op operation) string {
	if op.Anonymous() {
		return "none, the request is sent without a token"
	}

	return "bearer token"
}

func writeParameters(body *strings.Builder, params []parameter) {
	if len(params) == 0 {
		return
	}

	body.WriteString("\n| parameter | in | type | required | description |\n|---|---|---|---|---|\n")

	for _, p := range params {
		description := strings.Join(strings.Fields(docsref.EscapeProse(p.Description)), " ")

		fmt.Fprintf(body, "| `%s` | %s | `%s` | %s | %s |\n",
			docsref.EscapeSpan(p.Name), p.In, docsref.EscapeSpan(p.Type), docsref.YesNo(p.Required),
			docsref.EscapeSpan(description))
	}
}

func bodyOf(b *body) string {
	if b == nil {
		return docsref.None
	}

	text := mediaList(b.Media)
	if b.Required {
		return text + ", required"
	}

	return text + ", optional"
}

func responseOf(r response) string {
	text := docsref.EscapeProse(strings.TrimSuffix(r.Description, "."))

	if len(r.Media) == 0 {
		return text + ", no body"
	}

	return text + ", " + mediaList(r.Media)
}

func errorsOf(statuses []string) string {
	if len(statuses) == 0 {
		return docsref.None
	}

	return codeList(statuses) + ", each a [problem document](" + errorCodesPage + ")"
}

// mediaList renders `application/json` (`AccessDecision`) per media type.
func mediaList(media []mediaSchema) string {
	parts := make([]string, 0, len(media))
	for _, m := range media {
		parts = append(parts, fmt.Sprintf("`%s` (`%s`)", m.Media, m.Schema))
	}

	return strings.Join(parts, " or ")
}

// sentence escapes a summary and ends it with one full stop.
func sentence(summary string) string {
	return strings.TrimSuffix(docsref.EscapeProse(strings.TrimSpace(summary)), ".") + "."
}

// codeList renders `a`, `b`, `c`, or the word for an empty list.
func codeList(items []string) string {
	if len(items) == 0 {
		return docsref.None
	}

	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, "`"+item+"`")
	}

	return strings.Join(quoted, ", ")
}

// anchors maps each anchor claimed on a page to its owner. Two entries on one
// anchor is an error, because an anchor must point at one thing.
type anchors map[string]string

func (a anchors) claim(anchor, owner string) error {
	if first, clash := a[anchor]; clash {
		return fmt.Errorf("%w: %s and %s both anchor at #%s", errDuplicateName, first, owner, anchor)
	}

	a[anchor] = owner

	return nil
}
