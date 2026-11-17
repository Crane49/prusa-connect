// Package version exposes the application version. The string is embedded from
// the VERSION file in this package, which `make sync-version` copies from the
// repository root. A build may replace it with a stamp, see Stamped.
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var raw string

// stamped is written by the linker, never by code:
//
//	-ldflags "-X github.com/prusa3d/prusa-connect/internal/kernel/version.stamped=0.2.0"
//
// The Makefile writes what scripts/build-version.sh prints. The release
// workflow writes the tag without its v. Both Dockerfiles write their
// BUILD_VERSION build argument, which the caller has to pass. It is empty in a
// plain `go build`. It is a package-level string because that is the only
// thing the linker can write.
var stamped string

// Version returns the application version, without surrounding whitespace. It
// is the stamp when the build wrote one, and the VERSION file otherwise.
func Version() string {
	if stamp := Stamped(); stamp != "" {
		return stamp
	}

	return strings.TrimSpace(raw)
}

// Stamped returns the version the build stamped into the binary, without a
// leading "v", and empty for a build that stamped none.
func Stamped() string {
	return strings.TrimPrefix(strings.TrimSpace(stamped), "v")
}

// docsURL is where this build's documentation is read, written by the linker
// like stamped:
//
//	-ldflags "-X github.com/prusa3d/prusa-connect/internal/kernel/version.docsURL=https://example.org/docs/{version}/"
//
// `{version}` in it is replaced by Version(), so a build links to the pages
// written for it. The default is the beta portal. A build for another site
// sets DOCS_URL in the Makefile, and nothing else changes.
var docsURL = "https://beta.prusa3d.com/connect-local/docs/{version}/"

// versionPlaceholder is the part of docsURL that names the version.
const versionPlaceholder = "{version}"

// Documentation returns the address of this build's documentation, ending in a
// slash so a page path can be appended.
func Documentation() string {
	address := strings.ReplaceAll(strings.TrimSpace(docsURL), versionPlaceholder, Version())
	if address != "" && !strings.HasSuffix(address, "/") {
		address += "/"
	}

	return address
}
