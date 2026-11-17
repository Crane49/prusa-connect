package files

import "embed"

// InterfaceName is the interface a node exposes once per content store.
const InterfaceName = "ContentStore1"

// Definitions is the definition of ContentStore1. Pass it to schema.Load.
//
// The List method takes the identity of a listing in text form and returns
// the listing as unpadded base64url text. It answers CodeObjectNotFound for
// an identity the instance does not publish and CodeInvalidParams for one
// that is missing or malformed.
//
//go:embed ContentStore1.json
var Definitions embed.FS
