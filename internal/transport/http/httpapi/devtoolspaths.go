package httpapi

// The developer surface. The page is outside /api/v1 because it is a page and
// not an operation a generated client calls. It is still registered through
// huma.Register, so it is in the document, it passes the route and document
// check, and it takes the same identity every other operation takes.
//
// The printer build leaves the surface out. The paths stay declared in every
// build, so a module cannot claim /dev on a printer either.
const (
	devPagePath       = "/dev"
	devConnectorsPath = "/api/v1/dev/connectors"
	devCommandsPath   = "/api/v1/dev/devices/{device_id}/commands"
)
