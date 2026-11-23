package manifest

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Every denied header is refused however its name is spelled, so dropping one
// from the table, or a case that slips past it, fails here.
func TestEveryDeniedHeaderIsRefusedInEverySpelling(t *testing.T) {
	t.Parallel()

	for _, name := range deniedHeaders {
		for _, spelling := range []string{name, strings.ToUpper(name), http.CanonicalHeaderKey(name)} {
			if err := CheckRequestHeader(spelling); !errors.Is(err, ErrInvalid) {
				t.Errorf("CheckRequestHeader(%q) = %v, want ErrInvalid", spelling, err)
			}
		}
	}
}

// A header on the list a module may ask for is never denied and never one
// every module gets, and each one passes the check, so a mistake in the list
// fails here and not at a person's install.
func TestEveryGrantableHeaderIsAllowedAndNoneIsDenied(t *testing.T) {
	t.Parallel()

	for _, name := range grantableHeaders {
		if slices.Contains(deniedHeaders, name) || strings.HasPrefix(name, deniedHeaderPrefix) {
			t.Errorf("%s is on the list a module may ask for and on the denied list", name)
		}

		if slices.Contains(DefaultRequestHeaders, name) {
			t.Errorf("%s is on the list a module may ask for and every module gets it already", name)
		}

		if err := CheckRequestHeader(http.CanonicalHeaderKey(name)); err != nil {
			t.Errorf("CheckRequestHeader(%q) = %v, want nil", name, err)
		}
	}
}
