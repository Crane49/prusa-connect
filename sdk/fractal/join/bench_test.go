package join

import (
	"strconv"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// benchRegistry is how many nodes the Parent has admitted before each
// benchmark runs. It stands for the server of a large print farm.
const benchRegistry = 1000

// benchParent builds a Parent with benchRegistry admissions.
func benchParent(b *testing.B) *Parent {
	b.Helper()
	issuer := &countingIssuer{}
	p, err := NewParent(b.Context(), ParentConfig{
		Identity:   "parent-key",
		Path:       parentPath,
		Store:      &MemoryAdmissionStore{},
		Verifier:   jsonVerifier{},
		Authorizer: TOFU{},
		Issuer:     issuer,
	})
	if err != nil {
		b.Fatalf("build the parent: %v", err)
	}
	for i := range benchRegistry {
		id := fractal.NodeID("printer-" + strconv.Itoa(i))
		identity := fractal.Identity("key-" + strconv.Itoa(i))
		if out := p.Evaluate(b.Context(), encode(request(id, identity))); !out.Granted {
			b.Fatalf("the Parent denied %s at %q", id, out.Check)
		}
	}
	return p
}

// BenchmarkEvaluateRejoin measures Evaluate for a request from an admitted
// node that renews its credentials. On a running network this is the request
// a Parent gets most often.
func BenchmarkEvaluateRejoin(b *testing.B) {
	p := benchParent(b)
	body := encode(request("printer-500", "key-500"))
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if out := p.Evaluate(ctx, body); !out.Granted {
			b.Fatalf("the Parent denied the renewal at %q", out.Check)
		}
	}
}

// BenchmarkEvaluateCollision measures Evaluate for a request that is denied
// because another identity owns the Node ID.
func BenchmarkEvaluateCollision(b *testing.B) {
	p := benchParent(b)
	body := encode(request("printer-500", "stranger"))
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if out := p.Evaluate(ctx, body); out.Granted {
			b.Fatal("the Parent granted a node id that another identity owns")
		}
	}
}

// BenchmarkEvaluateNewChild measures Evaluate for the first join of a new
// node. It runs every check and then saves an admission record.
func BenchmarkEvaluateNewChild(b *testing.B) {
	p := benchParent(b)
	bodies := make([][]byte, b.N)
	for i := range bodies {
		id := fractal.NodeID("fresh-" + strconv.Itoa(i))
		identity := fractal.Identity("fresh-key-" + strconv.Itoa(i))
		bodies[i] = encode(request(id, identity))
	}
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if out := p.Evaluate(ctx, bodies[i]); !out.Granted {
			b.Fatalf("the Parent denied a new node at %q", out.Check)
		}
	}
}

// BenchmarkDecide measures Decide on a renewal, the call a joined node makes
// for every grant it receives.
func BenchmarkDecide(b *testing.B) {
	s := Situation{
		Held: Membership{
			NodePath:       "hq/brno/printer-1",
			ParentIdentity: "parent-key",
			Credentials:    "old",
			Endpoint:       "tls://brno:4222",
		},
		Joined: true,
		Grant: fractal.JoinGrant{
			Version:        fractal.Version,
			NodePath:       "hq/brno/printer-1",
			ParentIdentity: "parent-key",
			Credentials:    "fresh",
			ExpiresAt:      fractal.TimestampOf(time.Unix(1700003600, 0)),
		},
		Endpoint: "tls://brno:4222",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if d := Decide(s); d.Action != ActionKeep {
			b.Fatalf("the action is %s", d.Action)
		}
	}
}
