// Package joinwire encodes and checks the join request and its answer. It
// also has the checks a node uses to decide whether a node may join it.
//
// The join request is a compact JWS, signed with the node's identity NKEY.
// The answer is JSON. Verify returns the request only after it has verified
// the signature.
//
// TokenAuthorizer implements JWT mode: an authority signs a token with RS256,
// and the node verifies it with a key from a JWKS endpoint. AskFirst denies a
// key nobody has approved and grants it after an operator approves.
package joinwire
