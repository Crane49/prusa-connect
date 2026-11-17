// Package supervised applies the supervision wrapper to real contracts.
//
// There is one adapter per contract, and each one is mechanical: a few lines
// per method, with no policy in them. The queue, the timeout, the breaker and
// the panic recovery stay in the supervisor, so no resource author writes them.
package supervised
