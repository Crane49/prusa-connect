package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/docsref"
)

// The Go SDK pages: one page per importable package. Each has the package
// comment, then one section per exported identifier with its declaration and
// its doc comment as the text.

const (
	goDir = "go-sdk"

	goFix = "change the comment in Go and regenerate."

	missingComment = "No comment. The declaration carries none, and lint refuses that in the sdk."
)

// goPageName is the file name of a package's page: `busapi.md`, or
// `authapi-authapitest.md` for a nested package. The directory stays flat.
func goPageName(rel string) string {
	return docsref.Anchor(rel) + ".md"
}

func goPages(referenceDir string, packages []goPackage) ([]page, error) {
	pages := make([]page, 0, len(packages))

	for _, pkg := range packages {
		region, err := renderGoRegion(pkg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", goPageName(pkg.Rel), err)
		}

		front := frontMatter{
			title:         pkg.Rel,
			description:   strings.TrimSuffix(pkg.Synopsis, ".") + ".",
			audience:      "extend",
			implementedBy: []string{"sdk/" + pkg.Rel, generatorPath},
		}

		placeholder := fmt.Sprintf("The `%s` package of the Go SDK.\n\n%s: write what the package is for and "+
			"when a module imports it, then delete this line.", pkg.Rel, placeholderMark)

		pages = append(pages, page{
			path:   filepath.Join(referenceDir, goDir, goPageName(pkg.Rel)),
			region: region,
			fresh:  freshPage(front, placeholder, region),
		})
	}

	return pages, nil
}

func renderGoRegion(pkg goPackage) ([]byte, error) {
	var body strings.Builder

	anchor := goPackageAnchor(pkg.Rel)
	taken := anchors{}

	if err := taken.claim(anchor, "package "+pkg.Rel); err != nil {
		return nil, err
	}

	fmt.Fprintf(&body, "## `%s` {#%s}\n\n", pkg.Rel, anchor)
	fmt.Fprintf(&body, "```go\nimport \"%s\"\n```\n\n", pkg.ImportPath)

	if pkg.Doc == "" {
		body.WriteString(missingComment)
		body.WriteString("\n")
	} else {
		body.WriteString(pkg.Doc)
		body.WriteString("\n")
	}

	for _, entry := range pkg.Entries {
		recv, name, _ := strings.Cut(entry.Name, ".")
		if name == "" {
			recv, name = "", recv
		}

		entryAnchor := goAnchor(pkg.Rel, recv, name)
		if err := taken.claim(entryAnchor, pkg.Rel+"."+entry.Name); err != nil {
			return nil, err
		}

		writeGoEntry(&body, entry, entryAnchor)
	}

	return regionOf("sdk/"+pkg.Rel, goFix, body.String()), nil
}

func writeGoEntry(body *strings.Builder, entry goEntry, anchor string) {
	fmt.Fprintf(body, "\n%s `%s` {#%s}\n\n", strings.Repeat("#", entry.Level), entry.Name, anchor)
	fmt.Fprintf(body, "```go\n%s\n```\n\n", strings.TrimSpace(entry.Decl))

	if entry.Doc == "" {
		body.WriteString(missingComment)
		body.WriteString("\n")

		return
	}

	body.WriteString(entry.Doc)
	body.WriteString("\n")
}
