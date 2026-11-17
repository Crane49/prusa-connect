// Package auth is what a caller carries once this server knows who they are.
//
// The oidc subpackage establishes identity with the OAuth 2.1 authorization
// code flow and PKCE. What comes out of it is a credential in this package, and
// every transport resolves that credential locally instead of asking the issuer
// again.
//
// Three rules here are security rules and not conveniences. A credential kind
// is accepted on some transports and refused on the others. Re-validation
// happens on a schedule and not once at connect. Every refusal is worded the
// same way, whatever caused it.
package auth
