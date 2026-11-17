package stock

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/materials/internal/dbtest"
)

func TestFiltersAreKeptPerPersonAndReplaced(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			got, err := s.Filters(ctx, "alice")
			if err != nil || string(got) != `{}` {
				t.Fatalf("%s %v", got, err)
			}

			for _, kept := range []string{`{"in_stock":true}`, ` {"brand":"prusament"} `} {
				if err = s.KeepFilters(ctx, "alice", json.RawMessage(kept)); err != nil {
					t.Fatal(err)
				}
			}

			if got, err = s.Filters(ctx, "alice"); err != nil || string(got) != `{"brand":"prusament"}` {
				t.Fatalf("%s %v", got, err)
			}

			if got, err = s.Filters(ctx, "bob"); err != nil || string(got) != `{}` {
				t.Fatalf("bob sees %s %v", got, err)
			}
		})
	}
}

func TestFiltersMustBeASmallObject(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			for _, bad := range []string{``, `[]`, `"x"`, `{`, `{"q":"` + strings.Repeat("a", MaxFilters) + `"}`} {
				if err := s.KeepFilters(ctx, "alice", json.RawMessage(bad)); !errors.Is(err, ErrInvalid) {
					t.Fatalf("%.20q: want ErrInvalid, got %v", bad, err)
				}
			}
		})
	}
}
