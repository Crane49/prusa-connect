// Package observability holds the metric vocabulary: what a series is called,
// which dimensions it may carry, and the bucket definition a histogram is built
// from. It imports the standard library only, so the rule that bounds the
// series count is compiled and tested in every build, not only in the ones that
// link a Prometheus client.
//
// That rule is why the package exists. A dimension with one value per printer
// costs one series per printer on a counter or a gauge, and one series per
// printer per bucket on a histogram. A histogram multiplies the cost by its
// bucket count, so Fanout is a type of its own and NewHistogram takes no
// parameter that can hold one.
//
// The client_golang binding lives in the prom subpackage, behind the
// observability build tag. Nothing here knows it exists.
package observability
