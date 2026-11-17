// Command generate-sdk-reference writes the generated region of every page
// under docs/src/content/docs/reference: api/<tag>.md per OpenAPI tag,
// go-sdk/<package>.md per sdk package and extensions/<folder>.md per official
// extension.
//
// Only the region between two marker comments is generated. The rest of a
// page is written by hand. A missing page is created with a TODO intro. With
// -check it writes nothing and fails when a region differs or a page is missing.
//
// Run via: make generate-sdk-reference, gated by make sdk-reference-check.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/internal/codegen/openapidoc"
)

const (
	defaultDocumentPath  = "internal/transport/http/httpapi/openapi.json"
	defaultSDKDir        = "sdk"
	defaultReferenceDir  = "docs/src/content/docs/reference"
	defaultExtensionsDir = "extensions"

	makeTarget = "generate-sdk-reference"
)

// errStale means a committed page differs from the generated one.
var errStale = errors.New("committed page differs from the generated one")

type options struct {
	documentPath  string
	sdkDir        string
	extensionsDir string
	referenceDir  string
	check         bool
}

// page is one reference page: its path, its generated region and the whole
// file to write when none is committed.
type page struct {
	path   string
	region []byte
	fresh  []byte
}

func main() {
	opts := options{}

	flag.StringVar(&opts.documentPath, "document", defaultDocumentPath, "path of the committed OpenAPI document")
	flag.StringVar(&opts.sdkDir, "sdk", defaultSDKDir, "directory of the sdk module")
	flag.StringVar(&opts.extensionsDir, "extensions", defaultExtensionsDir,
		"directory of the official extensions, one folder each")
	flag.StringVar(&opts.referenceDir, "reference", defaultReferenceDir,
		"directory the api/, go-sdk/ and extensions/ pages are written under")
	flag.BoolVar(&opts.check, "check", false,
		"compare the committed regions against the generated ones and write nothing")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "generate-sdk-reference: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	pages, err := render(opts)
	if err != nil {
		return err
	}

	created, err := apply(pages, opts.check)
	if err != nil {
		return err
	}

	if !opts.check {
		fmt.Fprintf(os.Stderr, "OK sdk reference: %d pages under %s, %d created\n",
			len(pages), filepath.ToSlash(opts.referenceDir), created)
	}

	return nil
}

// apply writes every page, or compares every page under check. It reports
// all problems, not only the first, and returns how many pages it created.
func apply(pages []page, check bool) (int, error) {
	var (
		problems []error
		created  int
	)

	for _, p := range pages {
		want, fresh, err := assemble(p)
		if err != nil {
			problems = append(problems, err)

			continue
		}

		if check {
			problems = append(problems, codegen.CompareFile(p.path, want, errStale, makeTarget))

			continue
		}

		if fresh {
			created++
		}

		if err = codegen.WriteFile(p.path, want); err != nil {
			return created, err
		}
	}

	return created, errors.Join(problems...)
}

// assemble returns the committed file with its region replaced, or the fresh
// page when none is committed. The second result is true for a fresh page.
func assemble(p page) ([]byte, bool, error) {
	committed, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return p.fresh, true, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", p.path, err)
	}

	spliced, err := splice(committed, p.region)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", filepath.ToSlash(p.path), err)
	}

	if bytes.Equal(spliced, committed) {
		return committed, false, nil
	}

	return spliced, false, nil
}

func render(opts options) ([]page, error) {
	api, err := readAPIInputs(opts.documentPath)
	if err != nil {
		return nil, err
	}

	packages, err := readGoSDK(opts.sdkDir)
	if err != nil {
		return nil, err
	}

	pages, err := apiPages(opts.referenceDir, api)
	if err != nil {
		return nil, err
	}

	exts, err := readExtensions(opts.extensionsDir)
	if err != nil {
		return nil, err
	}

	goPages, err := goPages(opts.referenceDir, packages)
	if err != nil {
		return nil, err
	}

	pages = append(pages, goPages...)

	return append(pages, extensionPages(opts.referenceDir, exts)...), nil
}

func readAPIInputs(documentPath string) (*apiInputs, error) {
	doc, err := openapidoc.Read(documentPath)
	if err != nil {
		return nil, err
	}

	decoded, err := doc.Operations()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", documentPath, err)
	}

	return &apiInputs{tags: doc.Tags, operations: operationsOf(decoded)}, nil
}
