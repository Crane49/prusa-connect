package natsbind

import (
	"reflect"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// TestAStreamThatDiscardsOldIsTurnedToRefuseNew: a stream that an older
// version created discards its oldest message when it is full. ensureStream
// turns it to refuse a new message instead, and keeps the messages that wait
// in it (fractal-requirements.md, Method Invocation Guarantees).
func TestAStreamThatDiscardsOldIsTurnedToRefuseNew(t *testing.T) {
	t.Parallel()
	srv, err := server.NewServer(&server.Options{Port: -1, JetStream: true, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(testLifetime) {
		t.Fatal("the server did not start")
	}
	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(conn.Close)
	js, err := conn.JetStream()
	if err != nil {
		t.Fatalf("reach jetstream: %v", err)
	}
	older := &nats.StreamConfig{Name: resStreamName, Subjects: []string{SubjectRES}, MaxBytes: 1 << 20}
	if _, addErr := js.AddStream(older); addErr != nil {
		t.Fatalf("create the stream as an older version did: %v", addErr)
	}
	if _, pubErr := js.Publish(SubjectRES, []byte(`{}`)); pubErr != nil {
		t.Fatalf("store a waiting message: %v", pubErr)
	}

	wanted := *older
	wanted.Discard = nats.DiscardNew
	if ensureErr := ensureStream(t.Context(), js, &wanted); ensureErr != nil {
		t.Fatalf("ensureStream: %v", ensureErr)
	}

	info, err := js.StreamInfo(resStreamName)
	if err != nil {
		t.Fatalf("read the stream: %v", err)
	}
	got := map[string]any{"discard": info.Config.Discard, "messages": info.State.Msgs}
	want := map[string]any{"discard": nats.DiscardNew, "messages": uint64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the stream is %v, want %v", got, want)
	}
}
