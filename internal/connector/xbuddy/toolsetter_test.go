package xbuddy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// toolSetters are the three setters the firmware reads under a tools.<k> key,
// each with a value its own argument accepts.
//
//nolint:gochecknoglobals // test data.
var toolSetters = []struct {
	verb  connectorapi.Capability
	name  string
	value string
}{
	{verb: VerbSetToolDiameter, name: "nozzle_diameter", value: "0.6"},
	{verb: VerbSetToolHardened, name: "hardened", value: "true"},
	{verb: VerbSetToolHighFlow, name: "high_flow", value: "false"},
}

// sentKwargs reads the command name and the kwargs back off an encoded frame,
// so a test measures what leaves the socket.
func sentKwargs(t *testing.T, frame []byte) (string, map[string]json.RawMessage) {
	t.Helper()

	var body struct {
		Command string                     `json:"command"`
		Kwargs  map[string]json.RawMessage `json:"kwargs"`
	}

	if err := json.Unmarshal(frame[headerLength:], &body); err != nil {
		t.Fatalf("the frame %q is not JSON: %v", frame, err)
	}

	return body.Command, body.Kwargs
}

// TestToolSetterKey holds the key each setter travels under. The tool argument
// is a head id from 0; the numbered firmware counts from 1 and the tree-shaped
// firmware from 0.
func TestToolSetterKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		base toolBase
		tool string
		key  string
	}{
		{name: "numbered, first head", base: toolBaseOne, tool: "0", key: "tools.1."},
		{name: "numbered, right head of an XL", base: toolBaseOne, tool: "1", key: "tools.2."},
		{name: "numbered, eighth head", base: toolBaseOne, tool: "7", key: "tools.8."},
		{name: "tree, first head", base: toolBaseZero, tool: "0", key: "tools.0."},
		{name: "tree, right head of an XL", base: toolBaseZero, tool: "1", key: "tools.1."},
		{name: "tree, eighth head", base: toolBaseZero, tool: "7", key: "tools.7."},
	}

	for _, setter := range toolSetters {
		for _, tc := range cases {
			t.Run(setter.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				form := wireTable[setter.verb]

				frame, err := encodeCommand(1, form, arguments{
					verb: setter.verb, declared: form.args,
					values: map[string]string{"tool": tc.tool, setter.name: setter.value},
				}, tc.base)
				if err != nil {
					t.Fatalf("encode: %v", err)
				}

				command, kwargs := sentKwargs(t, frame)
				if command != setValueCommand {
					t.Errorf("the setter travels as %q, want %s", command, setValueCommand)
				}

				want := tc.key + setter.name
				if len(kwargs) != 1 || string(kwargs[want]) != setter.value {
					t.Errorf("the kwargs are %v, want exactly {%q: %s}", kwargs, want, setter.value)
				}
			})
		}
	}
}

// TestToolSetterRefusedBeforeInfo: until the printer has sent an INFO nothing
// says which numbering it reads, and a guess changes the wrong nozzle. The
// setter is refused with a code a caller may retry on, and nothing is built.
func TestToolSetterRefusedBeforeInfo(t *testing.T) {
	t.Parallel()

	for _, setter := range toolSetters {
		t.Run(setter.name, func(t *testing.T) {
			t.Parallel()

			pending, err := (&Session{storage: defaultStorage}).build(connectorapi.Command{
				Verb: setter.verb, Args: map[string]string{"tool": "1", setter.name: setter.value},
			}, wireTable[setter.verb])
			if err == nil {
				t.Fatalf("the setter was built as %q before any INFO", pending.frame)
			}

			if !errors.Is(err, ErrToolBaseUnknown) {
				t.Errorf("the refusal %v does not wrap ErrToolBaseUnknown", err)
			}

			if !fault.HasCode(err, fault.PrinterVerbNotAllowedInState.Code()) {
				t.Errorf("the refusal %v carries no %s, so a caller reads it as a failure and not as "+
					"a command to send again", err, fault.PrinterVerbNotAllowedInState.Code())
			}
		})
	}
}

// infoEvent is an INFO event carrying one fixture as its data.
func infoEvent(t *testing.T, fixture string) inboundMessage {
	t.Helper()

	data, err := os.ReadFile("testdata/tools/" + fixture)
	if err != nil {
		t.Fatalf("read %s: %v", fixture, err)
	}

	message, err := classify([]byte(`{"event":"INFO","data":` + string(data) + `}`))
	if err != nil {
		t.Fatalf("classify %s: %v", fixture, err)
	}

	return message
}

// TestToolSetterReadsTheBaseFromInfo drives the setter through the session's
// own INFO path: a firmware 7 INFO and a firmware 8 INFO, and the key tool 1
// travels under after each. The second INFO in one session replaces the first.
func TestToolSetterReadsTheBaseFromInfo(t *testing.T) {
	t.Parallel()

	session := newSession(settings{
		identity: connectorapi.Identity{Vendor: Vendor, Model: "XL", Serial: "SN-SETTER"},
		storage:  defaultStorage,
	})
	t.Cleanup(func() { _ = session.Close() })

	for _, step := range []struct {
		info string
		key  string
	}{
		{info: "info-fw7.json", key: "tools.2.hardened"},
		{info: "info-xl.json", key: "tools.1.hardened"},
		{info: "info-fw7.json", key: "tools.2.hardened"},
	} {
		session.observe(infoEvent(t, step.info))

		pending, err := session.build(connectorapi.Command{
			Verb: VerbSetToolHardened, Args: map[string]string{"tool": "1", "hardened": "true"},
		}, wireTable[VerbSetToolHardened])
		if err != nil {
			t.Fatalf("after %s: %v", step.info, err)
		}

		if _, kwargs := sentKwargs(t, pending.frame); len(kwargs) != 1 || string(kwargs[step.key]) != "true" {
			t.Errorf("after %s tool 1 travels as %v, want {%q: true}", step.info, kwargs, step.key)
		}
	}
}

// TestToolSetterBaseIsReadUnderOneLock builds setters while INFOs of both
// shapes arrive on another goroutine. Under -race it fails if the base is read
// without its atomic; without -race, every key must still be one of the two a
// tool 1 setter may take.
func TestToolSetterBaseIsReadUnderOneLock(t *testing.T) {
	t.Parallel()

	session := newSession(settings{
		identity: connectorapi.Identity{Vendor: Vendor, Model: "XL", Serial: "SN-RACE"},
		storage:  defaultStorage,
	})
	t.Cleanup(func() { _ = session.Close() })

	infos := []inboundMessage{infoEvent(t, "info-fw7.json"), infoEvent(t, "info-xl.json")}
	session.observe(infos[0])

	const rounds = 200

	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range rounds {
			session.observe(infos[i%len(infos)])
		}
	})

	for range rounds {
		pending, err := session.build(connectorapi.Command{
			Verb: VerbSetToolHighFlow, Args: map[string]string{"tool": "1", "high_flow": "true"},
		}, wireTable[VerbSetToolHighFlow])
		if err != nil {
			t.Fatalf("build: %v", err)
		}

		_, kwargs := sentKwargs(t, pending.frame)
		if string(kwargs["tools.1.high_flow"]) != "true" && string(kwargs["tools.2.high_flow"]) != "true" {
			t.Fatalf("tool 1 travels as %v", kwargs)
		}
	}

	wg.Wait()
}

// TestAReconnectionForgetsTheToolNumbering connects a printer that said it runs
// firmware 7, and connects it again, as a printer does after a firmware update.
// Until the new connection's INFO, the numbering the old firmware used is a
// guess, and a setter is refused rather than sent under it.
func TestAReconnectionForgetsTheToolNumbering(t *testing.T) {
	t.Parallel()

	session := newSession(settings{
		identity: connectorapi.Identity{Vendor: Vendor, Model: "XL", Serial: "SN-UPDATE"},
		storage:  defaultStorage,
	})
	t.Cleanup(func() { _ = session.Close() })

	session.observe(infoEvent(t, "info-fw7.json"))

	attached := make(chan struct{})

	var upgrader websocket.Upgrader

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		if session.attach(r.Context(), conn) {
			close(attached)
		}
	}))
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws://"+strings.TrimPrefix(server.URL, "http://"), nil)
	if err != nil {
		t.Fatalf("dialling the driver: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })

	select {
	case <-attached:
	case <-time.After(2 * time.Second):
		t.Fatal("the connection never attached")
	}

	pending, err := session.build(connectorapi.Command{
		Verb: VerbSetToolHardened, Args: map[string]string{"tool": "1", "hardened": "true"},
	}, wireTable[VerbSetToolHardened])
	if err == nil {
		t.Fatalf("a setter after a reconnection and before its INFO was built as %q, "+
			"under the numbering of the firmware the printer ran before", pending.frame)
	}

	if !errors.Is(err, ErrToolBaseUnknown) {
		t.Errorf("the refusal %v does not wrap ErrToolBaseUnknown", err)
	}
}

// TestAQueuedSetterIsNotSentUnderAnotherNumbering builds a setter under the
// firmware 7 numbering and lets the printer report firmware 8 before the
// setter leaves the queue. Its key would name another tool, so it is settled
// with the refusal and never written.
func TestAQueuedSetterIsNotSentUnderAnotherNumbering(t *testing.T) {
	t.Parallel()

	session := newSession(settings{
		identity: connectorapi.Identity{Vendor: Vendor, Model: "XL", Serial: "SN-QUEUED"},
		storage:  defaultStorage,
	})
	t.Cleanup(func() { _ = session.Close() })

	session.observe(infoEvent(t, "info-fw7.json"))

	pending, err := session.build(connectorapi.Command{
		Verb: VerbSetToolHardened, Args: map[string]string{"tool": "1", "hardened": "true"},
	}, wireTable[VerbSetToolHardened])
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if session.toolBaseMoved(pending) {
		t.Fatal("a setter under the numbering the printer still reports was held back")
	}

	session.observe(infoEvent(t, "info-xl.json"))

	if !session.toolBaseMoved(pending) {
		t.Fatalf("a setter built as %q for firmware 7 would be sent to firmware 8", pending.frame)
	}

	if answer := <-pending.result; !errors.Is(answer.err, ErrToolBaseUnknown) {
		t.Errorf("the held setter was settled with %v, want the refusal", answer.err)
	}
}
