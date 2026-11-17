// Package prom binds the metric catalog to client_golang and serves it.
//
// It is the only package in internal/ that imports a Prometheus client, so the
// cardinality rule covers every series: each one is built from a validated Def,
// and a bucketed metric carrying a per-device dimension is refused before it
// can be registered. The gate is TestOnlyOnePackageImportsAPrometheusClient in
// internal/arch.
//
// The implementation is behind the observability build tag. Without the tag the
// same constructors return a registry that records nothing and serves 404, and
// the binary links no Prometheus client, which keeps it smaller.
package prom
