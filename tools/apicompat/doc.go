// Command apicompat classifies every difference between the HTTP API this
// build serves and the baseline the last release published, and decides whether
// the difference is one ADR-20260825-zkq permits.
//
// Detecting a change is covered elsewhere:
// internal/transport/http/httpapi/openapi.json is generated from the registered
// operations, and a build whose document differs from the committed one fails.
// The rule about which differences are allowed is ADR-20260825-zkq, and this is the
// gate that holds it.
//
// # What it does
//
//	apicompat check    classify baseline against current, and exit per policy
//	apicompat accept   rewrite the baseline from the current document
//
// # Why it is not oasdiff
//
// oasdiff is the right tool and its classification is what this one's rules are
// taken from. It is not adopted because it is a separate binary a runner has to
// install, and a design requirement puts every gate under `go test` or a `go run` of
// something in this tree, so a runner that failed to install a tool cannot pass
// a gate by skipping it. The rule table here is small because this API is small:
// thirteen paths and seventeen schemas. If it stops being small, adopt oasdiff
// and delete this.
//
// # The property that makes it a gate rather than a report
//
// A difference the rule table has no rule for is classified unknown, and an
// unknown difference always fails. A classifier that silently passed a shape it
// did not understand would report "no breaking changes" for a change nobody
// looked at, which is worse than no gate at all.
package main
