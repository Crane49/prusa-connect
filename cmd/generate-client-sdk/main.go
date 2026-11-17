// Command generate-client-sdk writes the generated half of the client package
// @prusa/connect: operations.ts, errors.ts, events.ts and vocabulary.ts
// under packages/typescript/src/generated/. It also stamps VERSION into the
// package.json. The types file core-api.ts comes from openapi-typescript.
//
// With -check it writes nothing and fails when a committed file differs.
// With -module it generates a module's own client package from the OpenAPI
// fragment the module ships. See module.go.
//
// Run via: make generate-client-sdk, gated by make client-sdk-check.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/prusa3d/prusa-connect/internal/codegen"
)

const (
	defaultDocumentPath   = "internal/transport/http/httpapi/openapi.json"
	defaultErrorCodesPath = "docs/errors/error-codes.json"
	defaultEventTypesPath = "docs/vocabularies/event-types.json"
	defaultAuthzPath      = "docs/vocabularies/authorization.json"
	defaultVersionPath    = "VERSION"
	defaultPackageDir     = "packages/typescript"

	generatedDir = "src/generated"
	manifestName = "package.json"
)

var (
	// errStale means a committed file differs from the generated one.
	errStale = errors.New("committed file differs from the generated one")
	// errNoVersionField means package.json has no version to stamp.
	errNoVersionField = errors.New("package.json carries no version field")
)

type options struct {
	documentPath   string
	errorCodesPath string
	eventTypesPath string
	authzPath      string
	versionPath    string
	packageDir     string
	check          bool

	// Module mode only. It also reads documentPath, versionPath and check.
	module      bool
	namespace   string
	packageName string
	outDir      string
	token       string
	coreVersion string
}

// inputs is read in full before anything is rendered, so a wrong path fails
// before any file is written.
type inputs struct {
	operations []operation
	codes      []errorCode
	events     eventTypesFile
	authz      authzFile
	version    string
	manifest   []byte
}

// artefact is one rendered file and the path it is committed at.
type artefact struct {
	path    string
	content []byte
}

func main() {
	opts := options{}

	flag.StringVar(&opts.documentPath, "document", defaultDocumentPath, "path of the committed OpenAPI document")
	flag.StringVar(&opts.errorCodesPath, "error-codes", defaultErrorCodesPath, "path of the error-code registry")
	flag.StringVar(&opts.eventTypesPath, "event-types", defaultEventTypesPath, "path of the event-type set")
	flag.StringVar(&opts.authzPath, "authorization", defaultAuthzPath, "path of the authorization vocabulary")
	flag.StringVar(&opts.versionPath, "version", defaultVersionPath, "path of the core's VERSION file")
	flag.StringVar(&opts.packageDir, "package", defaultPackageDir, "directory of the client package")
	flag.BoolVar(&opts.check, "check", false,
		"compare the committed files against the generated ones and write nothing")

	flag.BoolVar(&opts.module, "module", false,
		"generate a module's own client package from its OpenAPI fragment instead of the core package")
	flag.StringVar(&opts.namespace, "namespace", "",
		"the module's namespace, which names its mount under "+modulesMount)
	flag.StringVar(&opts.packageName, "name", "",
		"the npm name of the module package (default prusa-connect-<last label of the namespace>)")
	flag.StringVar(&opts.outDir, "out", "", "the directory the module package is written to")
	flag.StringVar(&opts.token, "token", "",
		"the bearer token a -document URL is fetched with (default the "+tokenEnv+" environment variable)")
	flag.StringVar(&opts.coreVersion, "core-version", "",
		"the @prusa/connect version the module package peers on (default what the -version file says)")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "generate-client-sdk: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	if opts.module {
		return runModule(opts)
	}

	in, err := readInputs(opts)
	if err != nil {
		return err
	}

	artefacts, err := render(in, opts.packageDir)
	if err != nil {
		return err
	}

	if opts.check {
		var problems []error

		for _, a := range artefacts {
			problems = append(problems, codegen.CompareFile(a.path, a.content, errStale, "generate-client-sdk"))
		}

		return errors.Join(problems...)
	}

	for _, a := range artefacts {
		if err = codegen.WriteFile(a.path, a.content); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "OK client sdk: %s (%d operations, %d error codes, %d event types)\n",
		filepath.Join(opts.packageDir, generatedDir), len(in.operations), len(in.codes), len(in.events.Entries))

	return nil
}

func readInputs(opts options) (*inputs, error) {
	doc, err := readDocument(opts.documentPath)
	if err != nil {
		return nil, err
	}

	ops, err := operationsOf(doc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opts.documentPath, err)
	}

	codes, err := readRegistry[errorCodesFile](opts.errorCodesPath)
	if err != nil {
		return nil, err
	}

	events, err := readRegistry[eventTypesFile](opts.eventTypesPath)
	if err != nil {
		return nil, err
	}

	authz, err := readRegistry[authzFile](opts.authzPath)
	if err != nil {
		return nil, err
	}

	if len(codes.Codes) == 0 || len(events.Entries) == 0 || len(authz.Actions) == 0 {
		return nil, errEmptyRegistry
	}

	version, err := readVersion(opts.versionPath)
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(opts.packageDir, manifestName)

	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}

	return &inputs{
		operations: ops,
		codes:      codes.Codes,
		events:     *events,
		authz:      *authz,
		version:    version,
		manifest:   manifest,
	}, nil
}

func render(in *inputs, packageDir string) ([]artefact, error) {
	manifest, err := stampVersion(in.manifest, in.version)
	if err != nil {
		return nil, err
	}

	generated := filepath.Join(packageDir, generatedDir)

	return []artefact{
		{path: filepath.Join(generated, "operations.ts"), content: renderOperations(in.operations)},
		{path: filepath.Join(generated, "errors.ts"), content: renderErrors(in.codes)},
		{path: filepath.Join(generated, "events.ts"), content: renderEvents(in.events)},
		{path: filepath.Join(generated, "vocabulary.ts"), content: renderVocabulary(in.authz)},
		{path: filepath.Join(packageDir, manifestName), content: manifest},
	}, nil
}
