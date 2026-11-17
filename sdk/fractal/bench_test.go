package fractal_test

import (
	"encoding/json"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// A node does this work for every REP a printer sends: it decodes the message,
// validates the names, works out where the message goes, and encodes it. The
// benchmarks below measure each of those steps.

func BenchmarkNodeIDValidate(b *testing.B) {
	b.ReportAllocs()
	id := fractal.NodeID("printer-001")
	for b.Loop() {
		if err := id.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNodePathValidate(b *testing.B) {
	b.ReportAllocs()
	path := fractal.NodePath("hq/brno/hall-2/rack-7/printer-001")
	for b.Loop() {
		if err := path.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInterfaceNameValidate(b *testing.B) {
	b.ReportAllocs()
	name := fractal.InterfaceName("FDMPrinter1")
	for b.Loop() {
		if err := name.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNodePathIsAncestorOf(b *testing.B) {
	b.ReportAllocs()
	own := fractal.NodePath("hq/brno/hall-2")
	receiver := fractal.NodePath("hq/brno/hall-2/rack-7/printer-001")
	for b.Loop() {
		if !own.IsAncestorOf(receiver) {
			b.Fatal("not an ancestor")
		}
	}
}

func BenchmarkNodePathNextHop(b *testing.B) {
	b.ReportAllocs()
	own := fractal.NodePath("hq/brno/hall-2")
	receiver := fractal.NodePath("hq/brno/hall-2/rack-7/printer-001")
	for b.Loop() {
		if _, ok := own.NextHop(receiver); !ok {
			b.Fatal("not routable")
		}
	}
}

func BenchmarkNodePathCovers(b *testing.B) {
	b.ReportAllocs()
	child := fractal.NodePath("hq/brno/hall-2")
	sender := fractal.NodePath("hq/brno/hall-2/rack-7/printer-001")
	for b.Loop() {
		if !child.Covers(sender) {
			b.Fatal("sender refused")
		}
	}
}

func BenchmarkPatternMatch(b *testing.B) {
	b.ReportAllocs()
	pattern := fractal.Pattern("FDM*1")
	name := fractal.InterfaceName("FDMPrinter1")
	for b.Loop() {
		if !pattern.Match(name) {
			b.Fatal("no match")
		}
	}
}

func BenchmarkPatternMatchAdversarial(b *testing.B) {
	b.ReportAllocs()
	pattern := fractal.Pattern("a*a*a*a*b")
	name := fractal.InterfaceName("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	for b.Loop() {
		if pattern.Match(name) {
			b.Fatal("matched")
		}
	}
}

func BenchmarkREPDecode(b *testing.B) {
	b.ReportAllocs()
	payload := []byte(goldenREPReconnect)
	for b.Loop() {
		var rep fractal.REP
		if err := json.Unmarshal(payload, &rep); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkREPValidate(b *testing.B) {
	b.ReportAllocs()
	var rep fractal.REP
	if err := json.Unmarshal([]byte(goldenREPReconnect), &rep); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if err := rep.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkREPEncode(b *testing.B) {
	b.ReportAllocs()
	var rep fractal.REP
	if err := json.Unmarshal([]byte(goldenREPReconnect), &rep); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := json.Marshal(rep); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodeIs(b *testing.B) {
	b.ReportAllocs()
	code := fractal.Code(0x2011)
	for b.Loop() {
		if !code.Is(fractal.CodeDenied) {
			b.Fatal("no match")
		}
	}
}

func BenchmarkNewRPCID(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fractal.NewRPCID(); err != nil {
			b.Fatal(err)
		}
	}
}
