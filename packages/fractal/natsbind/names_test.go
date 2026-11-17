package natsbind

import (
	"reflect"
	"testing"

	"github.com/nats-io/jwt/v2"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestTheDomainOfANodePath checks Domain. The cases include "hq/a/b" and
// "hq/a_b", the pair fractal-nats.md gives as the reason to double an
// underscore.
func TestTheDomainOfANodePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path fractal.NodePath
		want string
	}{
		{"a root path", "hq", "hq"},
		{"a path of three segments", "hq/brno/printer-1", "hq_brno_printer-1"},
		{"a node id carrying an underscore", "hq/brno/printer_1", "hq_brno_printer__1"},
		{"the pair the rule exists for, split", "hq/a/b", "hq_a_b"},
		{"the pair the rule exists for, joined", "hq/a_b", "hq_a__b"},
		{"an underscore at the start of a node id", "hq/_x", "hq___x"},
		{"nothing", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Domain(c.path); got != c.want {
				t.Errorf("the domain of %q is %q, want %q", c.path, got, c.want)
			}
		})
	}
}

// TestTheDerivationIsInjective checks that two different Node Paths never have
// the same JetStream domain.
func TestTheDerivationIsInjective(t *testing.T) {
	t.Parallel()
	paths := []fractal.NodePath{
		"hq", "hq/a", "hq/b", "hq/a/b", "hq/a_b", "hq/a__b", "hq/a/b/c", "hq_a", "hq_a/b",
	}
	seen := map[string]fractal.NodePath{}
	for _, path := range paths {
		domain := Domain(path)
		if held, ok := seen[domain]; ok {
			t.Errorf("%q and %q share the domain %q", held, path, domain)
		}
		seen[domain] = path
	}
}

// TestTheSubjectsAreTheOnesInTheTable checks every subject against
// fractal-nats.md, Subject Namespace.
func TestTheSubjectsAreTheOnesInTheTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"a method invocation", SubjectRPC("printer-1"), "fractal.down.rpc.printer-1"},
		{"a requested property change", SubjectDES("printer-1"), "fractal.down.des.printer-1"},
		{"a full state report request", SubjectRBR("printer-1"), "fractal.down.rbr.printer-1"},
		{"a result", SubjectRES, "fractal.up.res"},
		{"a state report", SubjectREP, "fractal.up.rep"},
		{"a connection loss message", SubjectLOS, "fractal.up.los"},
		{"a heartbeat", SubjectHB, "fractal.up.hb"},
		{"an ancestor state report", SubjectAREP("printer-1"), "fractal.down.arep.printer-1"},
		{"an ancestor connection loss message", SubjectALOS("printer-1"), "fractal.down.alos.printer-1"},
		{"an ancestor full state report request", SubjectARBR, "fractal.up.arbr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.got != c.want {
				t.Errorf("the subject is %q, want %q", c.got, c.want)
			}
		})
	}
}

// TestAMessageTakesTheSubjectOfItsType checks subjectOf for every message type
// in both directions. A type that has no subject in one direction returns an
// error.
func TestAMessageTakesTheSubjectOfItsType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		d    fractalnode.Dispatch
		want string
		ok   bool
	}{
		{"a method invocation goes down", down(fractal.KindRPC), "fractal.down.rpc.printer-1", true},
		{"a property change goes down", down(fractal.KindDES), "fractal.down.des.printer-1", true},
		{"a report request goes down", down(fractal.KindRBR), "fractal.down.rbr.printer-1", true},
		{"a state report goes up", up(fractal.KindREP), SubjectREP, true},
		{"a loss message goes up", up(fractal.KindLOS), SubjectLOS, true},
		{"a result goes up", up(fractal.KindRES), SubjectRES, true},
		{"a state report does not go down", down(fractal.KindREP), "", false},
		{"a result does not go down", down(fractal.KindRES), "", false},
		{"a loss message does not go down", down(fractal.KindLOS), "", false},
		{"a method invocation does not go up", up(fractal.KindRPC), "", false},
		{"a property change does not go up", up(fractal.KindDES), "", false},
		{"a report request does not go up", up(fractal.KindRBR), "", false},
		{"a type that does not exist", up(0), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := subjectOf(c.d)
			if c.ok && err != nil {
				t.Fatalf("subjectOf returned an error: %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("subjectOf returned the subject %q, want an error", got)
			}
			if got != c.want {
				t.Errorf("the subject is %q, want %q", got, c.want)
			}
		})
	}
}

// TestTheJetStreamGrantsNameOneStream checks the whole permissions of the two
// users of a link. Every JetStream subject in them names the stream of that
// link, so a link reaches no other stream and no other part of the JetStream
// API (fractal-nats.md, Security).
func TestTheJetStreamGrantsNameOneStream(t *testing.T) {
	t.Parallel()
	const (
		own   = "hq_brno"
		child = "hq_brno_printer-1"
		rpc   = "fractal-rpc-printer-1"
		res   = "fractal-res"
	)
	resConsumers := []string{
		"$JS.hq_brno_printer-1.API.CONSUMER.CREATE.fractal-res",
		"$JS.hq_brno_printer-1.API.CONSUMER.CREATE.fractal-res.>",
		"$JS.hq_brno_printer-1.API.CONSUMER.DURABLE.CREATE.fractal-res.>",
		"$JS.hq_brno_printer-1.API.CONSUMER.INFO.fractal-res.>",
		"$JS.hq_brno_printer-1.API.CONSUMER.MSG.NEXT.fractal-res.>",
		"$JS.hq_brno_printer-1.API.CONSUMER.DELETE.fractal-res.>",
		"$JS.hq_brno_printer-1.API.CONSUMER.LIST.fractal-res",
		"$JS.hq_brno_printer-1.API.CONSUMER.NAMES.fractal-res",
	}
	got := map[string]jwt.UserPermissionLimits{
		"client": linkClient("printer-1", child),
		"leaf":   linkLeaf("printer-1", own, child),
	}
	want := map[string]jwt.UserPermissionLimits{
		"client": permissions(
			append([]string{
				"fractal.down.>", "_INBOX.>",
				"$JS.API.STREAM.INFO." + rpc, "$JS.API.STREAM.CREATE." + rpc, "$JS.API.STREAM.UPDATE." + rpc,
				"$JS.API.STREAM.PURGE." + rpc,
				"$JS.ACK." + res + ".>", "$JS.ACK.*.*." + res + ".>",
			}, resConsumers...),
			[]string{"fractal.up.>", "_INBOX.>"},
		),
		"leaf": permissions(
			[]string{
				"fractal.up.>", "_INBOX.>",
				"$JS.ACK." + rpc + ".>", "$JS.ACK.*.*." + rpc + ".>",
				"$JS.hq_brno.API.CONSUMER.CREATE.fractal-rpc-printer-1",
				"$JS.hq_brno.API.CONSUMER.CREATE.fractal-rpc-printer-1.>",
				"$JS.hq_brno.API.CONSUMER.DURABLE.CREATE.fractal-rpc-printer-1.>",
				"$JS.hq_brno.API.CONSUMER.INFO.fractal-rpc-printer-1.>",
				"$JS.hq_brno.API.CONSUMER.MSG.NEXT.fractal-rpc-printer-1.>",
				"$JS.hq_brno.API.CONSUMER.DELETE.fractal-rpc-printer-1.>",
				"$JS.hq_brno.API.CONSUMER.LIST.fractal-rpc-printer-1",
				"$JS.hq_brno.API.CONSUMER.NAMES.fractal-rpc-printer-1",
			},
			append([]string{
				"fractal.down.>", "_INBOX.>",
				"$JS.ACK." + res + ".>", "$JS.ACK.*.*." + res + ".>",
			}, resConsumers...),
		),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the permissions of the link are %+v, want %+v", got, want)
	}
}

// down builds a Dispatch of the given kind for the node below printer-1.
func down(kind fractal.Kind) fractalnode.Dispatch {
	return fractalnode.Dispatch{Kind: kind, Child: "printer-1"}
}

// up builds a Dispatch of the given kind for the node above.
func up(kind fractal.Kind) fractalnode.Dispatch {
	return fractalnode.Dispatch{Kind: kind}
}
