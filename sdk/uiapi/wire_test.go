package uiapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/uiapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// hostFake is a Hider that records what it was handed.
type hostFake struct {
	held   []string
	calls  int
	failed error
}

func (h *hostFake) Hide(_ context.Context, hidden []string) error {
	h.calls++
	if h.failed != nil {
		return h.failed
	}

	h.held = hidden

	return nil
}

// session sends a client's call straight to the host's handler, the way the
// wire would after encoding the params.
type session struct {
	methods wirerpc.Methods
	sent    int
}

func (s *session) Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error) {
	s.sent++

	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	handler, ok := s.methods[method]
	if !ok {
		return nil, &wirerpc.Error{Code: wirerpc.CodeMethodNotFound, Message: "no such method"}
	}

	answer, err := handler(ctx, wirerpc.Call{Method: method, Params: body})
	if err != nil {
		return nil, err
	}

	return json.Marshal(answer)
}

// A module hands the host the whole list, and the next call replaces it.
func TestTheHostHoldsTheLastListAModuleSent(t *testing.T) {
	t.Parallel()

	host := &hostFake{}
	client := uiapi.NewClient(&session{methods: uiapi.Handlers(host)})

	if err := client.Hide(t.Context(), "pages/customers", "file_action/plan"); err != nil {
		t.Fatalf("hide: %v", err)
	}

	if !slices.Equal(host.held, []string{"pages/customers", "file_action/plan"}) {
		t.Fatalf("held = %v", host.held)
	}

	if err := client.Hide(t.Context()); err != nil {
		t.Fatalf("show all: %v", err)
	}

	if host.held == nil || len(host.held) != 0 {
		t.Fatalf("held after an empty call = %#v, expected an empty list", host.held)
	}
}

// A list outside the bounds makes no call, and the error says why.
func TestTheClientRefusesAListOutsideTheBounds(t *testing.T) {
	t.Parallel()

	tooMany := make([]string, uiapi.MaxEntries+1)
	for i := range tooMany {
		tooMany[i] = "pages/p" + strings.Repeat("x", i)
	}

	cases := map[string][]string{
		"a card":             {"cards/overview"},
		"an unknown slot":    {"sidebar/customers"},
		"no identity":        {"pages/"},
		"no slash":           {"pages"},
		"white space":        {"pages/two words"},
		"an entry twice":     {"pages/customers", "pages/customers"},
		"a long entry":       {"pages/" + strings.Repeat("a", uiapi.MaxEntryBytes)},
		"more than the most": tooMany,
	}

	for name, hidden := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			wire := &session{methods: uiapi.Handlers(&hostFake{})}

			err := uiapi.NewClient(wire).Hide(t.Context(), hidden...)
			if !errors.Is(err, uiapi.ErrInvalid) {
				t.Fatalf("err = %v, expected ErrInvalid", err)
			}

			if wire.sent != 0 {
				t.Fatalf("the client made %d calls for a list it refused", wire.sent)
			}
		})
	}
}

// Every slot key but cards is accepted, so a module hides any entry the
// operator can hide.
func TestEverySlotButCardsCanBeHidden(t *testing.T) {
	t.Parallel()

	keys := uiapi.SlotKeys()
	if slices.Contains(keys, "cards") {
		t.Fatalf("keys = %v, cards is not hideable", keys)
	}

	for _, want := range []string{"pages", "settings", "file_action", "print_section", "device_section"} {
		if !slices.Contains(keys, want) {
			t.Fatalf("keys = %v, missing %s", keys, want)
		}
	}

	hidden := make([]string, 0, len(keys))
	for _, key := range keys {
		hidden = append(hidden, key+"/one")
	}

	if err := (uiapi.HideParams{Hidden: hidden}).Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// The host checks again, so a module written without this package is held
// to the same bounds, and it reads CodeInvalidParams.
func TestTheHostRefusesAListTheClientWouldHave(t *testing.T) {
	t.Parallel()

	host := &hostFake{}
	handler := uiapi.Handlers(host)[uiapi.MethodHide]

	for name, params := range map[string]string{
		"a card":       `{"hidden":["cards/overview"]}`,
		"no params":    ``,
		"not a list":   `{"hidden":"pages/customers"}`,
		"twice listed": `{"hidden":["pages/a","pages/a"]}`,
	} {
		_, err := handler(t.Context(), wirerpc.Call{Method: uiapi.MethodHide, Params: json.RawMessage(params)})

		coded, ok := errors.AsType[*wirerpc.Error](err)
		if !ok || coded.Code != wirerpc.CodeInvalidParams {
			t.Fatalf("%s: err = %v, expected CodeInvalidParams", name, err)
		}
	}

	if host.calls != 0 {
		t.Fatalf("the host was handed %d lists it should have refused", host.calls)
	}
}

// A failure of the host reaches the module as a fixed sentence, never as the
// host's own text.
func TestAHostFailureNamesNoInternals(t *testing.T) {
	t.Parallel()

	host := &hostFake{failed: errors.New("the map at 0xc000 is locked")}
	client := uiapi.NewClient(&session{methods: uiapi.Handlers(host)})

	err := client.Hide(t.Context(), "pages/customers")
	if err == nil || strings.Contains(err.Error(), "0xc000") || !strings.Contains(err.Error(), uiapi.HostFailed) {
		t.Fatalf("err = %v", err)
	}

	if errors.Is(err, uiapi.ErrInvalid) {
		t.Fatalf("a host failure read as an invalid list: %v", err)
	}
}

// A host that keeps no list serves no method, and a call reads
// CodeMethodNotFound.
func TestANilHiderServesNothing(t *testing.T) {
	t.Parallel()

	if methods := uiapi.Handlers(nil); len(methods) != 0 {
		t.Fatalf("methods = %v", methods)
	}

	err := uiapi.NewClient(&session{methods: uiapi.Handlers(nil)}).Hide(t.Context(), "pages/customers")

	coded, ok := errors.AsType[*wirerpc.Error](err)
	if !ok || coded.Code != wirerpc.CodeMethodNotFound {
		t.Fatalf("err = %v, expected CodeMethodNotFound", err)
	}
}
