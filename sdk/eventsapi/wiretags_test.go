package eventsapi_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// The gate ADR-20260826-c77 rule 19 asks for. A sink marshals these six types, so a
// field with no tag reaches an integrator under its Go name and PascalCase is a
// spelling no other surface of this product uses. A new field is caught here
// rather than by the first person to read the forwarded JSON.
func TestEveryWireFieldCarriesASnakeCaseJSONTag(t *testing.T) {
	t.Parallel()

	onTheWire := []any{
		eventsapi.PrinterState{}, eventsapi.PrinterLabels{},
		eventsapi.JobInfo{}, eventsapi.Fact{}, eventsapi.Sample{}, eventsapi.Samples{},
	}

	for _, value := range onTheWire {
		payload := reflect.TypeOf(value)

		t.Run(payload.Name(), func(t *testing.T) {
			t.Parallel()

			for field := range payload.Fields() {
				if !field.IsExported() {
					continue
				}

				member, _, _ := strings.Cut(field.Tag.Get("json"), ",")
				if member == "" {
					t.Errorf("%s.%s carries no json name, so it marshals as %q",
						payload.Name(), field.Name, field.Name)

					continue
				}

				if !snakeCase(member) {
					t.Errorf("%s.%s is tagged %q, which is not snake_case",
						payload.Name(), field.Name, member)
				}
			}
		})
	}
}

// snakeCase reports whether a member name is lowercase words of letters and
// digits, joined by single underscores. One word is a name too: the envelope's
// own members are evclass and causationid, which CloudEvents 1.0 spells without
// a separator (ADR-20260826-c77 rule 11).
func snakeCase(member string) bool {
	for word := range strings.SplitSeq(member, "_") {
		if word == "" {
			return false
		}

		for _, letter := range word {
			if (letter < 'a' || letter > 'z') && (letter < '0' || letter > '9') {
				return false
			}
		}
	}

	return true
}
