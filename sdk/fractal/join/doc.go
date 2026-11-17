// Package join implements the join exchange of the Fractal protocol, in which
// a node asks the node above it for a place in the tree and gets a grant or a
// denial. This package sends no bytes itself.
//
// Parent is the node above. It runs the checks on each join request in the
// order the protocol requires and answers with the code of the first check
// that fails. It keeps an admission record for every node it admits.
//
// Joiner is the node that joins. It sends the request, decides with Decide
// whether to accept the grant, and runs the exchange again before its
// connection credentials expire.
//
// A transport binding implements ProofVerifier, Authorizer, Issuer,
// RevocationHook and Requester, and encodes the messages.
package join
