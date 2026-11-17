package arch_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// mdnsPackages are the packages the core's mDNS responder and listener are
// built on: the DNS message library, and the two packages that read and write
// multicast. ADR-20260916-b3x rule 22 names them, and mdnsAnnounce is the one package
// that may import them.
//
// The rule exists because a second responder on this machine is a second set of
// records for one socket, and because the record's contents are held by gates
// that read one file. Anything else importing these packages is announcing
// something nobody reviewed.
var mdnsPackages = []string{"github.com/miekg/dns", "golang.org/x/net/ipv4", "golang.org/x/net/ipv6"}

const mdnsAnnounce = "internal/discovery"

// TestOnlyTheAnnouncerImportsTheMdnsPackages is ADR-20260916-b3x rules 16, 18 and 22.
//
// It reads test imports as well as production ones. A test that opened a second
// responder would take the same socket, and a helper that grew out of one is
// how the second announcer arrives.
func TestOnlyTheAnnouncerImportsTheMdnsPackages(t *testing.T) {
	t.Parallel()

	graph := packageGraph(t)

	var violations []string

	judged, inside := 0, map[string]bool{}

	for _, pkg := range graph.packages {
		judged++

		for _, imported := range graph.importsOf(pkg) {
			module := slices.IndexFunc(mdnsPackages, func(one string) bool {
				return imported == one || strings.HasPrefix(imported, one+"/")
			})
			if module < 0 {
				continue
			}

			if rel(pkg) == mdnsAnnounce {
				inside[mdnsPackages[module]] = true

				continue
			}

			violations = append(violations, fmt.Sprintf("%s imports %s", rel(pkg), imported))
		}
	}

	requireScanned(t, "packages judged against the mdns packages", judged, 30,
		"The graph held no package, so nothing was asked whether it imports them.")

	// The positive control. This gate passes on a tree where a package is
	// absent from every graph it reads, and a build configuration that stopped
	// compiling the announcer would be exactly that tree.
	for _, one := range mdnsPackages {
		if !inside[one] {
			t.Errorf("%s imports %s in no configuration this gate reads, so the gate would pass\n"+
				"whatever any other package did with it. Either the announcer moved, or the\n"+
				"package did.", mdnsAnnounce, one)
		}
	}

	report(t, violations, "ADR-20260916-b3x rules 16, 18 and 22, "+
		"docs/decisions/20260916-b3x-instances-announce-themselves-on-the-network.md")
}
