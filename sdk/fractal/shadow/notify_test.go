package shadow_test

import (
	"encoding/json"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// drain returns every Event that is in the channel of the subscription, and
// does not wait for more.
func drain(sub *shadow.Subscription) []shadow.Event {
	var events []shadow.Event
	for {
		select {
		case ev := <-sub.Events():
			events = append(events, ev)
		default:
			return events
		}
	}
}

// TestSubscriptionReportsEveryChange checks that a Subscription gets an Event
// for a REP that ApplyREP applied, for an interface MarkStale marked, and for
// an interface RemoveNode deleted.
func TestSubscriptionReportsEveryChange(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	sub := store.Subscribe(16)
	defer sub.Close()

	store.ApplyREP(rep(key, 100, fractal.Props{"n": json.RawMessage("1")}))
	events := drain(sub)
	want := shadow.Event{Key: key, Status: shadow.StatusOK, UpdatedAt: 100}
	if len(events) != 1 || events[0] != want {
		t.Fatalf("after a REP: %v, want %v", events, want)
	}

	store.MarkStale("hq/a")
	events = drain(sub)
	want = shadow.Event{Key: key, Status: shadow.StatusStale, UpdatedAt: 100}
	if len(events) != 1 || events[0] != want {
		t.Fatalf("after MarkStale: %v, want %v", events, want)
	}

	store.RemoveNode("hq/a")
	events = drain(sub)
	want = shadow.Event{Key: key, Status: shadow.StatusStale, UpdatedAt: 100, Removed: true}
	if len(events) != 1 || events[0] != want {
		t.Fatalf("after RemoveNode: %v, want %v", events, want)
	}
}

// TestSubscriptionIsSilentAboutWhatDidNotChange checks that a Subscription
// gets no Event for a REP that ApplyREP discarded or did not select, and none
// for a MarkStale that changed no interface.
func TestSubscriptionIsSilentAboutWhatDidNotChange(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	store.ApplyREP(rep(key, 100, nil))
	sub := store.Subscribe(16)
	defer sub.Close()

	store.ApplyREP(rep(key, 99, nil))
	store.ApplyREP(rep(shadow.Key{Node: "hq/b", Interface: "Camera1", Instance: "1"}, 100, nil))
	store.MarkStale("hq/b")
	if events := drain(sub); len(events) != 0 {
		t.Fatalf("events = %v, want none", events)
	}
}

// TestSlowSubscriberDoesNotBlockTheStore checks a Subscription nobody reads.
// When its channel is full, ApplyREP still applies every REP, and Missed counts
// the dropped Events. The Store has the timestamp of the last REP, so a reader
// can get the current state with Status or Get.
func TestSlowSubscriberDoesNotBlockTheStore(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	sub := store.Subscribe(2)
	defer sub.Close()
	const messages = 50
	for i := range messages {
		ts := fractal.Timestamp(100 + i)
		if res := store.ApplyREP(rep(key, ts, nil)); res.Outcome != shadow.OutcomeApplied {
			t.Fatalf("message %d: outcome = %s, want applied", i, res.Outcome)
		}
	}
	if got := sub.Missed(); got != messages-2 {
		t.Fatalf("Missed() = %d, want %d", got, messages-2)
	}
	_, updatedAt, ok := store.Status(key)
	if !ok || updatedAt != 100+messages-1 {
		t.Fatalf("updatedAt = %d, want %d, the timestamp of the last REP", updatedAt, 100+messages-1)
	}
}

// TestSubscriptionCloseIsSafeTwice checks that a second Close does not panic,
// and that the channel is closed afterwards.
func TestSubscriptionCloseIsSafeTwice(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	sub := store.Subscribe(0)
	sub.Close()
	sub.Close()
	if _, open := <-sub.Events(); open {
		t.Fatal("the channel is still open after Close")
	}
	// The Store still applies a REP after its last Subscription is closed.
	if res := store.ApplyREP(rep(key, 100, nil)); res.Outcome != shadow.OutcomeApplied {
		t.Fatalf("outcome = %s, want applied", res.Outcome)
	}
}

// TestSeveralSubscribersEachSeeTheChange checks that every open Subscription
// gets the Event of one change, also after another Subscription was closed.
func TestSeveralSubscribersEachSeeTheChange(t *testing.T) {
	t.Parallel()
	key := shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	subs := []*shadow.Subscription{store.Subscribe(4), store.Subscribe(4), store.Subscribe(4)}
	defer func() {
		for _, sub := range subs {
			sub.Close()
		}
	}()
	store.ApplyREP(rep(key, 100, nil))
	for i, sub := range subs {
		if events := drain(sub); len(events) != 1 {
			t.Fatalf("subscriber %d got %v, want one event", i, events)
		}
	}
	subs[1].Close()
	store.ApplyREP(rep(key, 200, nil))
	for _, i := range []int{0, 2} {
		if events := drain(subs[i]); len(events) != 1 {
			t.Fatalf("subscriber %d got %v after another subscriber closed, want one event", i, events)
		}
	}
}
