package resourcehost

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/uiapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// keptEmitter records every emission it is handed.
type keptEmitter struct {
	mu   sync.Mutex
	seen []bus.Emission
}

func (k *keptEmitter) Emit(emission bus.Emission) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.seen = append(k.seen, emission)

	return nil
}

func (k *keptEmitter) emitted() []bus.Emission {
	k.mu.Lock()
	defer k.mu.Unlock()

	return append([]bus.Emission(nil), k.seen...)
}

// hidingHost is a host running one service module, whose events go to the
// emitter returned beside it.
func hidingHost(t *testing.T) (*Host, *keptEmitter, wirerpc.Methods, ref.Ref) {
	t.Helper()

	declared := declaredAs(t, ref.KindSink, "planner", "acme.example/planner", install.RuntimeService)
	host := supervisingDeclarations(declared)
	emitter := &keptEmitter{}
	host.cfg.Events = func(ref.Ref) bus.Emitter { return emitter }

	methods, err := host.servesTo(declared)
	require.NoError(t, err)

	return host, emitter, methods, declared.Ref
}

func hide(t *testing.T, methods wirerpc.Methods, hidden ...string) {
	t.Helper()

	handler, served := methods[uiapi.MethodHide]
	require.True(t, served, "ui.hide is not served to a module")

	params, err := json.Marshal(uiapi.HideParams{Hidden: hidden})
	require.NoError(t, err)

	_, err = handler(t.Context(), wirerpc.Call{Method: uiapi.MethodHide, Params: params})
	require.NoError(t, err)
}

// A module states its list, the host holds it for that instance, and a
// change is published once with the whole list (ADR-20261021-6h2).
func TestAModuleHidesItsOwnEntries(t *testing.T) {
	t.Parallel()

	host, emitter, methods, instance := hidingHost(t)

	hide(t, methods, "pages/customers")
	require.Equal(t, []string{"pages/customers"}, host.hidden.of(instance))

	// The same list again changes nothing, so it publishes nothing.
	hide(t, methods, "pages/customers")

	hide(t, methods)
	require.Empty(t, host.hidden.of(instance))

	seen := emitter.emitted()
	require.Len(t, seen, 2)

	for i, want := range []string{`["pages/customers"]`, `[]`} {
		require.Equal(t, event.TypeResourceUIChanged, seen[i].Type)
		require.Equal(t, instance.String(), seen[i].Key)
		require.JSONEq(t, `{"resource":"`+instance.String()+`","hidden":`+want+`}`, seen[i].Body)
	}
}

// A process that starts again has stated nothing, so it does not keep the
// list of the process before it.
func TestAStartForgetsTheList(t *testing.T) {
	t.Parallel()

	host, _, methods, instance := hidingHost(t)
	hide(t, methods, "pages/customers")

	_, err := host.servesTo(host.running[instance].declaration)
	require.NoError(t, err)
	require.Empty(t, host.hidden.of(instance), "a restart kept the list of the process before it")
}

// Two instances keep two lists: the wire names no instance, so one module
// cannot touch another's list.
func TestOneModuleCannotHideAnothersEntries(t *testing.T) {
	t.Parallel()

	planner := declaredAs(t, ref.KindSink, "planner", "acme.example/planner", install.RuntimeService)
	spools := declaredAs(t, ref.KindSink, "spools", "acme.example/spools", install.RuntimeService)
	host := supervisingDeclarations(planner, spools)

	plannerMethods, err := host.servesTo(planner)
	require.NoError(t, err)

	_, err = host.servesTo(spools)
	require.NoError(t, err)

	hide(t, plannerMethods, "pages/customers")

	require.Equal(t, []string{"pages/customers"}, host.hidden.of(planner.Ref))
	require.Empty(t, host.hidden.of(spools.Ref))
}

// A list the module wrote wrong is refused and leaves the held list as it was.
func TestARefusedListLeavesTheHeldOne(t *testing.T) {
	t.Parallel()

	host, emitter, methods, instance := hidingHost(t)
	hide(t, methods, "pages/customers")

	_, err := methods[uiapi.MethodHide](t.Context(), wirerpc.Call{
		Method: uiapi.MethodHide,
		Params: json.RawMessage(`{"hidden":["cards/overview"]}`),
	})
	require.Error(t, err)

	require.Equal(t, []string{"pages/customers"}, host.hidden.of(instance))
	require.Len(t, emitter.emitted(), 1)
}
