package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// The assertions this package's tests use, in standard library terms.
//
// ADR-20260824-gfr rule 2 is not qualified and a test dependency lands in sdk/go.mod
// exactly as a production one does, so testify is part of the module graph
// every extension author resolves. These are the seven shapes the tests here
// actually asked for; anything narrower stays an `if` at its call site.
//
// Argument order is testify's, want before got, because that is the order the
// call sites were written in.

// equal fails unless want and got are deeply equal. It is generic so the two
// sides must already be the same type: testify compared an int against a uint64
// at runtime and reported a mismatch, and this reports it at compile time.
func equal[T any](t *testing.T, want, got T, msgAndArgs ...any) {
	t.Helper()

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %#v, got %#v%s", want, got, because(msgAndArgs))
	}
}

// equalAny is equal for a value that arrives as any, such as a field read out
// of a decoded JSON object.
func equalAny(t *testing.T, want, got any) {
	t.Helper()

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %#v, got %#v", want, got)
	}
}

func noError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func errorIs(t *testing.T, err, target error, msgAndArgs ...any) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Fatalf("want an error matching %v, got %v%s", target, err, because(msgAndArgs))
	}
}

func isTrue(t *testing.T, got bool, msgAndArgs ...any) {
	t.Helper()

	if !got {
		t.Fatalf("want true, got false%s", because(msgAndArgs))
	}
}

func isFalse(t *testing.T, got bool, msgAndArgs ...any) {
	t.Helper()

	if got {
		t.Fatalf("want false, got true%s", because(msgAndArgs))
	}
}

// empty fails unless got has no length, or is the zero value of a type that has
// no length.
func empty[T any](t *testing.T, got T, msgAndArgs ...any) {
	t.Helper()

	value := reflect.ValueOf(got)

	if !value.IsValid() {
		return
	}

	if measurable(value.Kind()) {
		if value.Len() != 0 {
			t.Fatalf("want nothing, got %#v%s", got, because(msgAndArgs))
		}

		return
	}

	if !value.IsZero() {
		t.Fatalf("want the zero value, got %#v%s", got, because(msgAndArgs))
	}
}

// measurable reports whether a kind answers Len, which is what "empty" means
// for it. Anything else is empty when it is its own zero value.
func measurable(kind reflect.Kind) bool {
	return slices.Contains(
		[]reflect.Kind{reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String}, kind)
}

// jsonEq compares two JSON documents by their decoded shape, so key order and
// whitespace do not decide the result.
func jsonEq(t *testing.T, want, got string, msgAndArgs ...any) {
	t.Helper()

	var wantDoc, gotDoc any

	if err := json.Unmarshal([]byte(want), &wantDoc); err != nil {
		t.Fatalf("the expected document is not JSON: %v", err)
	}

	if err := json.Unmarshal([]byte(got), &gotDoc); err != nil {
		t.Fatalf("the produced document is not JSON: %v%s", err, because(msgAndArgs))
	}

	if !reflect.DeepEqual(wantDoc, gotDoc) {
		t.Fatalf("want %s, got %s%s", want, got, because(msgAndArgs))
	}
}

// noKey fails when a decoded JSON object carries key at all, which is a
// different claim from carrying it with a null or empty value.
func noKey(t *testing.T, wire map[string]any, key string, msgAndArgs ...any) {
	t.Helper()

	if value, present := wire[key]; present {
		t.Fatalf("the key %q is on the wire as %#v%s", key, value, because(msgAndArgs))
	}
}

// because renders the optional trailing context in testify's shape: a format
// string and its arguments, or a single value.
func because(msgAndArgs []any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}

	format, ok := msgAndArgs[0].(string)
	if !ok {
		return ": " + fmt.Sprint(msgAndArgs...)
	}

	return ": " + fmt.Sprintf(format, msgAndArgs[1:]...)
}
