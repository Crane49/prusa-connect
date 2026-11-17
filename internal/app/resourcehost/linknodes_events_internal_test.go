package resourcehost

import (
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// node builds one server below this site for these tests.
func node(id string, connected bool) linkapi.Node {
	return linkapi.Node{
		ID:        linkapi.NodeID(id),
		Path:      linkapi.NodePath("root.site." + id),
		Admission: linkapi.AdmissionPending,
		PublicKey: "UA" + id,
		Connected: connected,
	}
}

// TestTheFirstReadingReportsNothing: the host has no reading to compare the
// first one with. Reporting every server as newly joined at every restart would
// fill a webhook with servers that joined months ago.
func TestTheFirstReadingReportsNothing(t *testing.T) {
	t.Parallel()

	now := linkapi.Status{Nodes: []linkapi.Node{node("a", true), node("b", false)}}

	if frames := nodeChanges("link:site", linkapi.Status{}, now, true); len(frames) != 0 {
		t.Fatalf("the first reading produced %d events, want none", len(frames))
	}
}

// TestTheDifferenceBetweenTwoReadingsNamesWhatMoved covers every change the
// comparison finds, and both directions of each: a server that joined against
// one that was already there, and a connection that closed against one that
// opened again.
func TestTheDifferenceBetweenTwoReadingsNamesWhatMoved(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		before, now linkapi.Status
		want        []string
	}{
		"nothing moved": {
			before: linkapi.Status{Nodes: []linkapi.Node{node("a", true)}},
			now:    linkapi.Status{Nodes: []linkapi.Node{node("a", true)}},
			want:   nil,
		},
		"one server joined": {
			before: linkapi.Status{Nodes: []linkapi.Node{node("a", true)}},
			now:    linkapi.Status{Nodes: []linkapi.Node{node("a", true), node("b", true)}},
			want:   []string{event.TypeLinkNodeJoined},
		},
		"a connection closed": {
			before: linkapi.Status{Nodes: []linkapi.Node{node("a", true)}},
			now:    linkapi.Status{Nodes: []linkapi.Node{node("a", false)}},
			want:   []string{event.TypeLinkNodeLost},
		},
		"a connection opened again": {
			before: linkapi.Status{Nodes: []linkapi.Node{node("a", false)}},
			now:    linkapi.Status{Nodes: []linkapi.Node{node("a", true)}},
			want:   []string{event.TypeLinkNodeReturned},
		},
		"a server that went away stays away": {
			before: linkapi.Status{Nodes: []linkapi.Node{node("a", true)}},
			now:    linkapi.Status{},
			want:   nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			frames := nodeChanges("link:site", tc.before, tc.now, false)

			if len(frames) != len(tc.want) {
				t.Fatalf("the comparison produced %d events, want %d: %+v", len(frames), len(tc.want), frames)
			}

			for i, wanted := range tc.want {
				if frames[i].eventType != wanted {
					t.Errorf("event %d is %s, want %s", i, frames[i].eventType, wanted)
				}
			}
		})
	}
}

// TestAJoinedEventCarriesWhatAnOperatorNeeds: the body names the link, the id
// the server joined under, where it sits in the tree, whether it may act here
// yet, and its key.
func TestAJoinedEventCarriesWhatAnOperatorNeeds(t *testing.T) {
	t.Parallel()

	frames := nodeChanges("link:site", linkapi.Status{},
		linkapi.Status{Nodes: []linkapi.Node{node("b", true)}}, false)

	if len(frames) != 1 {
		t.Fatalf("the comparison produced %d events, want one", len(frames))
	}

	body, ok := frames[0].body.(event.LinkNodeJoined)
	if !ok {
		t.Fatalf("the body is a %T, want an event.LinkNodeJoined", frames[0].body)
	}

	want := event.LinkNodeJoined{
		Link:      "link:site",
		Node:      "b",
		Path:      "root.site.b",
		Admission: "pending",
		PublicKey: "UAb",
	}

	if body != want {
		t.Errorf("the body is %+v, want %+v", body, want)
	}

	if frames[0].key != "link:site/b" {
		t.Errorf("the conflation key is %q, and it names the link and the server", frames[0].key)
	}
}

// TestALostEventCarriesTheTimeSomethingLastArrived: a screen showing a server
// as gone needs to say since when.
func TestALostEventCarriesTheTimeSomethingLastArrived(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, time.September, 21, 10, 0, 0, 0, time.UTC)

	was := node("a", true)
	gone := node("a", false)
	gone.LastSeen = &when

	frames := nodeChanges("link:site",
		linkapi.Status{Nodes: []linkapi.Node{was}},
		linkapi.Status{Nodes: []linkapi.Node{gone}}, false)

	if len(frames) != 1 {
		t.Fatalf("the comparison produced %d events, want one", len(frames))
	}

	body, ok := frames[0].body.(event.LinkNodePresence)
	if !ok {
		t.Fatalf("the body is a %T, want an event.LinkNodePresence", frames[0].body)
	}

	if body.Connected {
		t.Error("the body says the connection is open and the event says it closed")
	}

	if body.LastSeen == nil || !body.LastSeen.Equal(when) {
		t.Errorf("the body's last_seen is %v, want %v", body.LastSeen, when)
	}
}

// TestARefusedJoinIsReportedOnceWithBothIdentifiers: the refusal of this site's
// own request carries the protocol's number and the link's registered code, and
// a link that reports the same refusal again produces no second event.
func TestARefusedJoinIsReportedOnceWithBothIdentifiers(t *testing.T) {
	t.Parallel()

	denied := &linkapi.Uplink{
		Address:   "nats://above:4222",
		State:     linkapi.UplinkDenied,
		Code:      0x2050,
		FaultCode: "com.prusa.fractal/JOIN_DENIED",
		Reason:    "this node has been revoked",
	}

	joining := &linkapi.Uplink{Address: "nats://above:4222", State: linkapi.UplinkJoining}

	frames := nodeChanges("link:site",
		linkapi.Status{Uplink: joining}, linkapi.Status{Uplink: denied}, false)

	if len(frames) != 1 || frames[0].eventType != event.TypeLinkNodeDenied {
		t.Fatalf("the comparison produced %+v, want one %s", frames, event.TypeLinkNodeDenied)
	}

	body, ok := frames[0].body.(event.LinkJoinDenied)
	if !ok {
		t.Fatalf("the body is a %T, want an event.LinkJoinDenied", frames[0].body)
	}

	want := event.LinkJoinDenied{
		Link:      "link:site",
		Address:   "nats://above:4222",
		Code:      0x2050,
		FaultCode: "com.prusa.fractal/JOIN_DENIED",
		Reason:    "this node has been revoked",
	}

	if body != want {
		t.Errorf("the body is %+v, want %+v", body, want)
	}

	again := nodeChanges("link:site",
		linkapi.Status{Uplink: denied}, linkapi.Status{Uplink: denied}, false)
	if len(again) != 0 {
		t.Errorf("the same refusal reported twice produced %+v, want nothing", again)
	}
}
