package document

import (
	"math"
	"reflect"
	"testing"
)

// near fails the test when got is further than 1e-9 from want.
func near(t *testing.T, want, got float64, what string) {
	t.Helper()

	if math.Abs(want-got) > 1e-9 {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

// same fails the test when got is not deeply equal to want.
func same(t *testing.T, want, got any, what ...string) {
	t.Helper()

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("%v: got %#v, want %#v", what, got, want)
	}
}
