// Package session is where a credential is enforced.
//
// Require is always fail-closed. No flag turns the check off, because a flag
// added for a test would also work in a deployment. A server that runs without
// authentication wires a different Principals in its composition root, and
// this package has no branch for it.
//
// It never imports a transport. Principals satisfies the httpapi.Principals
// port by having its method, not by naming it.
package session
