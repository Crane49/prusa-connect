package script_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/script"
	"github.com/prusa3d/prusa-connect/sdk/scriptapi"
)

// sample is the event every case here runs against.
func sample() scriptapi.Event {
	return scriptapi.Event{
		Kind:     "printer.state_changed",
		DeviceID: "prn-42",
		State:    "PRINTING",
		Time:     "2026-08-26T09:59:12Z",
	}
}

// TestAScriptMapsAnEventOntoMessages covers an expression an operator could
// write, every shape it may return, and the empty return that drops the event.
func TestAScriptMapsAnEventOntoMessages(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		source string
		want   []scriptapi.Message
	}{
		"one message": {
			source: `msg("rabbit-main", "printer." + event.device_id + "." + lower(event.state), event.state)`,
			want: []scriptapi.Message{
				{Endpoint: "rabbit-main", Key: "printer.prn-42.printing", Body: "PRINTING"},
			},
		},
		"a body the script encodes": {
			source: `msg("rabbit-main", "k", toJSON({"printer": event.device_id, "state": event.state}))`,
			want: []scriptapi.Message{
				{Endpoint: "rabbit-main", Key: "k", Body: `{"printer":"prn-42","state":"PRINTING"}`},
			},
		},
		"two destinations through fan": {
			source: `fan(msg("a", "k1", "one"), msg("b", "k2", "two"))`,
			want: []scriptapi.Message{
				{Endpoint: "a", Key: "k1", Body: "one"},
				{Endpoint: "b", Key: "k2", Body: "two"},
			},
		},
		"two destinations as an array literal": {
			source: `[msg("a", "k1", "one"), msg("b", "k2", "two")]`,
			want: []scriptapi.Message{
				{Endpoint: "a", Key: "k1", Body: "one"},
				{Endpoint: "b", Key: "k2", Body: "two"},
			},
		},
		"a branch that drops the event": {
			source: `event.state == "ERROR" ? msg("a", "k", "b") : nil`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			program, err := script.Compile(tc.source)
			require.NoError(t, err)

			got, err := program.Run(sample())
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestABodyIsCompactRatherThanIndented checks the shadowed encoder. The
// runtime's own toJSON indents with two spaces, so every body a script built
// with it would go on the wire padded.
func TestABodyIsCompactRatherThanIndented(t *testing.T) {
	t.Parallel()

	program, err := script.Compile(`toJSON({"a": 1, "b": 2})`)
	require.NoError(t, err)

	_, err = program.Run(sample())
	require.ErrorIs(t, err, script.ErrInvalidResult, "a bare string is not a message")

	program, err = script.Compile(`msg("a", "k", toJSON({"a": 1, "b": 2}))`)
	require.NoError(t, err)

	got, err := program.Run(sample())
	require.NoError(t, err)
	require.Equal(t, `{"a":1,"b":2}`, got[0].Body)
	require.NotContains(t, got[0].Body, "\n")
}

// TestTheCheckerRefusesAMisreadEvent shows what the typed environment gives.
// Each of these would compile against an untyped map. Here each is refused
// before any event arrives.
func TestTheCheckerRefusesAMisreadEvent(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a field that does not exist":     `msg("a", event.jbo, "b")`,
		"a string added to a number":      `msg("a", event.device_id + 1, "b")`,
		"a message built from a map":      `fan({"endpoint": "a"})`,
		"a constructor short an argument": `msg("a", "k")`,
		"a name the host never declared":  `publish("a", "k", "b")`,
	}

	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := script.Compile(source)
			require.ErrorIs(t, err, script.ErrInvalidSource)
		})
	}
}

// TestACompileFailureCarriesTheLineTheColumnAndTheCaret keeps the runtime's own
// error. It names the token an author has to change, and rewriting it in our
// own words would lose the only part they can act on.
func TestACompileFailureCarriesTheLineTheColumnAndTheCaret(t *testing.T) {
	t.Parallel()

	_, err := script.Compile("msg(\"a\", \"k\", \"b\")\n+ event.jbo")
	require.Error(t, err)

	require.Contains(t, err.Error(), "jbo", "the refusal does not name the token")
	require.Contains(t, err.Error(), "(2:9)", "the refusal does not name the line and column")
	require.Contains(t, err.Error(), "^", "the refusal does not carry the caret")
}

// TestASourceOverTheLimitIsRefusedBeforeTheParser covers the only guard against
// the parser's unbounded recursion. A deep enough source ends in a stack
// overflow that no recover catches and no node budget prevents, and it takes
// the process with it.
//
// Both sides are asserted: a source at the limit compiles and one byte more is
// refused, so the test fails whether the check is removed or set to the wrong
// number.
func TestASourceOverTheLimitIsRefusedBeforeTheParser(t *testing.T) {
	t.Parallel()

	atTheLimit := padTo(script.MaxSourceBytes)
	require.Len(t, atTheLimit, script.MaxSourceBytes)

	_, err := script.Compile(atTheLimit)
	require.NoError(t, err, "a script at the limit must still compile")

	_, err = script.Compile(atTheLimit + " ")
	require.ErrorIs(t, err, script.ErrInvalidSource)
	require.Contains(t, err.Error(), "8193")
}

// TestADeepSourceIsRefusedRatherThanCrashingTheProcess reproduces the crash the
// length check prevents. Deeply nested parentheses are what a generated or
// corrupted script looks like, and the parser hits a stack overflow that cannot
// be recovered.
//
// Removing the length check from Compile does not make this test fail. It kills
// the test binary, because nothing inside the process survives to report it.
func TestADeepSourceIsRefusedRatherThanCrashingTheProcess(t *testing.T) {
	t.Parallel()

	const depth = 700_000

	deep := strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth)

	_, err := script.Compile(deep)
	require.ErrorIs(t, err, script.ErrInvalidSource)
}

// TestRepeatIsNotAvailableToAScript checks that the repeat builtin is disabled.
// It multiplies a length by a count, and its own cap is a million copies of a
// string the script builds, so a short expression can allocate gigabytes.
//
// len is compiled in the same test, so the case cannot pass because every
// builtin is unavailable.
func TestRepeatIsNotAvailableToAScript(t *testing.T) {
	t.Parallel()

	_, err := script.Compile(`msg("a", "k", repeat("x", 4))`)
	require.ErrorIs(t, err, script.ErrInvalidSource)
	require.Contains(t, err.Error(), "repeat")

	_, err = script.Compile(`msg("a", "k", string(len(event.device_id)))`)
	require.NoError(t, err, "the other builtins are still available")
}

// TestTheScriptEnvironmentIsFlat keeps the values a script sees to flat copies.
// The rule decays when somebody adds a convenient field.
//
// A script reads everything in its environment, including through a method on a
// value there. A live pointer in an environment returned a secret, and a method
// on it read an unexported field. A collection is the other risk: a fold over
// a large slice runs for seconds and cannot be interrupted.
func TestTheScriptEnvironmentIsFlat(t *testing.T) {
	t.Parallel()

	// A pointer, an interface and a slice have each been reached from a script.
	// The rest are the other ways a field can hold something that is not a
	// bounded copy.
	refusedKinds := []reflect.Kind{
		reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice,
		reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Array,
	}

	declared := []any{scriptapi.Event{}, scriptapi.Message{}}

	for _, value := range declared {
		shape := reflect.TypeOf(value)

		t.Run(shape.Name(), func(t *testing.T) {
			t.Parallel()

			require.Zero(t, shape.NumMethod(), "a method on the value is reachable from a script")
			require.Zero(t, reflect.PointerTo(shape).NumMethod(),
				"a pointer method is reachable once the value is addressable")

			for field := range shape.Fields() {
				require.True(t, field.IsExported(), "%s is unexported and cannot be read", field.Name)

				require.NotContains(t, refusedKinds, field.Type.Kind(),
					"%s.%s is a %s, and a script is handed flat copies of bounded size",
					shape.Name(), field.Name, field.Type.Kind())
			}
		})
	}
}

// TestAPooledProgramIsSafeForConcurrentRuns is why the machines are pooled. A
// vm.VM is not safe for concurrent use, and a compiled program can have callers
// besides one instance's queue.
func TestAPooledProgramIsSafeForConcurrentRuns(t *testing.T) {
	t.Parallel()

	program, err := script.Compile(`msg("a", event.device_id, event.state)`)
	require.NoError(t, err)

	done := make(chan error, 16)

	for range cap(done) {
		go func() {
			for range 200 {
				got, runErr := program.Run(sample())
				if runErr != nil {
					done <- runErr

					return
				}

				if got[0].Key != "prn-42" {
					done <- errWrongKey

					return
				}
			}

			done <- nil
		}()
	}

	for range cap(done) {
		require.NoError(t, <-done)
	}
}

// padTo builds a valid script of exactly n bytes, padded with a comment so the
// length is the only thing under test.
func padTo(n int) string {
	const source = `msg("a", "k", "b")`

	return source + " /*" + strings.Repeat("x", n-len(source)-5) + "*/"
}

// errWrongKey reports a concurrent run that returned another run's answer.
var errWrongKey = errors.New("the pooled machine returned another run's key")
