package diagnosticsapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/diagnosticsapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// callWith runs the handler on one request, as the host sends it.
func callWith(t *testing.T, handler wirerpc.Handler, cursor string) (any, error) {
	t.Helper()

	params, err := json.Marshal(diagnosticsapi.Request{Cursor: cursor})
	if err != nil {
		t.Fatalf("render the request: %v", err)
	}

	return handler(t.Context(), wirerpc.Call{Params: params})
}

// The module is handed the cursor the host sent back, so it can answer the
// next page, and the last page carries no cursor.
func TestAModuleAnswersPageByPage(t *testing.T) {
	t.Parallel()

	var seen []string

	handler := diagnosticsapi.Serve(func(_ context.Context, cursor string) (diagnosticsapi.Page, error) {
		seen = append(seen, cursor)

		if cursor == "" {
			return diagnosticsapi.Page{
				Files:  []diagnosticsapi.File{{Name: "syslog/printer-1.log", Text: "newest\n"}},
				Cursor: "2",
			}, nil
		}

		return diagnosticsapi.Page{}, nil
	})

	first, err := callWith(t, handler, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}

	if page := first.(diagnosticsapi.Page); page.Cursor != "2" || len(page.Files) != 1 {
		t.Fatalf("first page = %+v", page)
	}

	last, err := callWith(t, handler, "2")
	if err != nil {
		t.Fatalf("last page: %v", err)
	}

	if page := last.(diagnosticsapi.Page); page.Cursor != "" || page.Files == nil {
		t.Fatalf("last page = %+v, want no cursor and an empty list", page)
	}

	if strings.Join(seen, ",") != ",2" {
		t.Errorf("the module saw the cursors %q", seen)
	}
}

// A page the host would refuse is refused where the module answers it.
func TestAnUnusablePageIsRefusedBeforeItLeaves(t *testing.T) {
	t.Parallel()

	for name, file := range map[string]diagnosticsapi.File{
		"a path that climbs":   {Name: "../config.yaml", Text: "x"},
		"an absolute path":     {Name: "/etc/passwd", Text: "x"},
		"an empty segment":     {Name: "logs//a", Text: "x"},
		"a space":              {Name: "my log", Text: "x"},
		"a name too long":      {Name: strings.Repeat("a", diagnosticsapi.MaxNameBytes+1), Text: "x"},
		"text that is not UTF": {Name: "a.log", Text: "\xff"},
		"a page too large":     {Name: "a.log", Text: strings.Repeat("x", diagnosticsapi.MaxPageBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler := diagnosticsapi.Serve(func(context.Context, string) (diagnosticsapi.Page, error) {
				return diagnosticsapi.Page{Files: []diagnosticsapi.File{file}}, nil
			})

			if _, err := callWith(t, handler, ""); !errors.Is(err, diagnosticsapi.ErrPage) {
				t.Fatalf("err = %v, want ErrPage", err)
			}
		})
	}
}

// A page of more entries than MaxPageFiles is refused, even when every entry
// is empty, because each one still costs the host a name to keep.
func TestAPageHoldsAtMostMaxPageFiles(t *testing.T) {
	t.Parallel()

	page := diagnosticsapi.Page{Files: make([]diagnosticsapi.File, diagnosticsapi.MaxPageFiles)}
	for i := range page.Files {
		page.Files[i] = diagnosticsapi.File{Name: "f" + strconv.Itoa(i)}
	}

	if err := page.Validate(); err != nil {
		t.Fatalf("a full page: %v", err)
	}

	page.Files = append(page.Files, diagnosticsapi.File{Name: "one-more"})

	if err := page.Validate(); !errors.Is(err, diagnosticsapi.ErrPage) {
		t.Fatalf("err = %v, want ErrPage", err)
	}
}

func TestAFileNameIsARelativePath(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"syslog/printer-1.log": true,
		"metrics.lp":           true,
		"a/b/c_d-e.txt":        true,
		"":                     false,
		".":                    false,
		"a/../b":               false,
		"a/":                   false,
		"con":                  false,
		"logs/AUX.log":         false,
		"com1.txt":             false,
		"lpt9":                 false,
		"name.":                false,
		"console.log":          true,
		"community.txt":        true,
		"com0.txt":             true,
	} {
		if got := diagnosticsapi.ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}
