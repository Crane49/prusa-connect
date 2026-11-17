package config

import "fmt"

// Observability is the Prometheus client and the exposition it serves.
type Observability struct {
	// Enabled links the metrics client and serves the exposition.
	Enabled bool `yaml:"enabled"`
	// Addr moves the exposition to a listener of its own, which serves it to a
	// caller holding no credential and serves nothing else. Empty binds no
	// such socket and is the default: the exposition is then an operation on
	// the API listener, carrying identity and the fleet-wide telemetry grant.
	//
	// Most exporters work this way. etcd, CoreDNS and ingress-nginx each serve
	// their exposition on a port of their own, which a deployment publishes
	// only to its own network. The address is then the boundary, so a
	// collector needs no account on this server.
	//
	// One listener serves it, never both. The API listener answers 404 at
	// /metrics while this key is set, and its OpenAPI document names no such
	// operation, so only the one address the operator chose is readable
	// without a credential.
	Addr string `yaml:"addr"`
}

// On reports whether the metrics client is switched on.
func (o Observability) On() bool { return o.Enabled }

// validate holds the listener to the same key that asks for the metrics client,
// and to an address net.Listen will take.
//
// A file that binds the socket must also set enabled, so the build tag and the
// port are asked for together.
func (o Observability) validate() error {
	if o.Addr == "" {
		return nil
	}

	if !o.On() {
		return fmt.Errorf("%w: core.observability.addr is %q and core.observability.enabled is "+
			"false, so the listener would serve an exposition this build does not carry",
			ErrInvalid, o.Addr)
	}

	return validAddr("core.observability.addr", o.Addr)
}
