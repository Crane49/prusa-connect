// Package oidc logs a person in against a real issuer, with the OAuth 2.1
// authorization code flow and PKCE.
//
// It uses the standard library and sdk/authapi/oidctoken only. What this
// client needs is discovery, one redirect, one form post and a signature
// check, and a dependency for that much would grow the binary the server
// ships on small hardware.
//
// It implements OAuth 2.1 and not 2.0: S256 is the only code challenge method
// accepted, the implicit and password grants are absent, and the redirect URI
// is compared exactly.
package oidc
