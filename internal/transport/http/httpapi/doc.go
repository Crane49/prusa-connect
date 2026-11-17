// Package httpapi is the HTTP surface: a chi mux that only this package can
// reach, a Huma v2 API over it, the OpenAPI document that describes it, and the
// Scalar reference that renders it.
//
// The package guarantees that a handler cannot reach production undocumented.
// Five layers hold that guarantee, and none of them is enough alone:
//
//  1. the router is sealed. New keeps the mux unexported and returns an
//     http.Handler that cannot be asserted back to it.
//     TestOnlyTheSealedPackageImportsTheRouter refuses a router import elsewhere
//  2. net/http's own mux is refused by symbol and not by import, because every
//     package that speaks HTTP imports net/http.
//     TestNothingOutsideTheSealedPackageMountsAServeMux stops a composition
//     root wrapping Handler() and adding a route beside it
//  3. the routes the mux will serve are diffed against the operations the
//     OpenAPI document publishes. New runs the diff and refuses to build a
//     server that fails it, so an undocumented route fails at startup too
//  4. the document is committed. TestCommittedSpecMatchesTheCode regenerates it
//     and fails on any difference. It is the only layer that catches a contract
//     changing shape while its routes stay put
//  5. the document declares the security scheme the middleware enforces.
//     TestTheDocumentStatesWhatTheMiddlewareEnforces fails when an operation
//     publishes an empty requirement that principalMiddleware would let through
//
// huma.Operation.Hidden is the one in-framework way to register a live route
// the document omits. A hidden operation still reaches the mux, so layer 3
// catches it, and TestNoOperationIsHidden names it directly.
//
// # What the seal still cannot see
//
// chi.Walk enumerates routes, so a handler that is not a route is invisible to
// it. Three shapes qualify:
//
//   - a middleware that answers a path instead of delegating. It never becomes
//     a route, so the diff never sees it
//   - the NotFound and MethodNotAllowed handlers this package installs. They
//     serve every path the router does not match. If one of them returned
//     content and not a refusal, it would be a wildcard route nothing
//     enumerates
//   - anything wrapping Server.Handler() that is not a mux: a hand-written
//     http.HandlerFunc switching on r.URL.Path, a reverse proxy in front of the
//     process, or a second process on the same origin
//
// TestNoMiddlewareOrFallbackHandlerServesAPathOfItsOwn probes the first two by
// requesting paths a bypass would likely use and requiring a refusal. It
// samples paths and does not prove that no handler is there. No gate inside
// this package reaches the third shape. The composition root covers it by
// serving Server.Handler() as the whole origin.
package httpapi
