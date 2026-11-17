//go:build !diagnostics

package httpapi

import "github.com/danielgtaylor/huma/v2"

// diagnosticsCarried says this build leaves the support bundle out: the
// minimal one.
const diagnosticsCarried = false

// registerDiagnostics registers nothing in a build without the bundle.
func registerDiagnostics(huma.API, Config) {}
