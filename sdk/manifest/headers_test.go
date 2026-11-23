package manifest_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

func TestARoutesBlockReadsTheHeadersAModuleAsksFor(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withRoutes(`{
		"headers": [
			{"name": "print-after-upload", "reason": "the slicer says whether to print the file"}
		]
	}`)), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := []manifest.RequestHeader{
		{Name: "Print-After-Upload", Reason: "the slicer says whether to print the file"},
	}
	if !slices.Equal(held.Routes.Headers, want) {
		t.Errorf("headers = %+v, want %+v", held.Routes.Headers, want)
	}
}

func TestAHeaderRequestIsRefused(t *testing.T) {
	t.Parallel()

	header := func(name string) string {
		return withRoutes(`{"headers": [{"name": "` + name + `", "reason": "a reason"}]}`)
	}

	for _, tc := range []struct {
		name     string
		document string
		says     string
	}{
		{"the person's cookie", header("Cookie"), "never passed"},
		{"the person's bearer token", header("authorization"), "never passed"},
		{"the API key", header("X-Api-Key"), "never passed"},
		{"a proxy credential", header("Proxy-Authorization"), "never passed"},
		{"the host", header("Host"), "never passed"},
		{"a hop-by-hop header", header("Transfer-Encoding"), "never passed"},
		{"a header a proxy adds", header("X-Forwarded-For"), "never passed"},
		{"the client's address", header("X-Real-IP"), "never passed"},
		{"Forwarded", header("Forwarded"), "never passed"},
		{"a header every module gets", header("Content-Type"), "every module already"},
		{"a header nobody added to the list", header("Overwrite"), "not one a module may ask for"},
		{"a method override", header("X-Http-Method-Override"), "not one a module may ask for"},
		{"a proxy's identity header", header("X-Userinfo"), "not one a module may ask for"},
		{"a name that is not a token", header("Print After"), "not an HTTP token"},
		{"no reason", withRoutes(`{"headers": [{"name": "Print-After-Upload"}]}`), "reason is empty"},
		{
			"an escape in the reason",
			withRoutes(`{"headers": [{"name": "Print-After-Upload", "reason": "r\u001b[2K"}]}`),
			"control or invisible",
		},
		{
			"a reversal in the reason",
			withRoutes(`{"headers": [{"name": "Print-After-Upload", "reason": "r\u202eevil"}]}`),
			"control or invisible",
		},
		{"a name too long", header("X-" + strings.Repeat("a", 63)), "the limit is 64"},
		{"the printer's token", header("Token"), "never passed"},
		{"the printer's fingerprint", header("Fingerprint"), "never passed"},
		{"a proof of possession", header("DPoP"), "never passed"},
		{"a CDN's client address", header("CF-Connecting-IP"), "never passed"},
		{"another client address", header("True-Client-IP"), "never passed"},
		{
			"a required that is not true or false",
			withRoutes(`{"headers": [{"name": "Print-After-Upload", "reason": "r", "required": "yes"}]}`),
			"expected true or false",
		},
		{"an unknown key", withRoutes(`{"headers": [{"name": "Print-After-Upload", "reason": "r", "value": "?1"}]}`), "value"},
		{
			"a reason too long",
			withRoutes(`{"headers": [{"name": "Print-After-Upload", "reason": "` + strings.Repeat("a", 201) + `"}]}`),
			"the limit is 200",
		},
		{
			"a header twice in other cases",
			withRoutes(`{"headers": [{"name": "Print-After-Upload", "reason": "r"}, {"name": "print-after-upload", "reason": "r"}]}`),
			"twice",
		},
		{
			"too many headers",
			withRoutes(`{"headers": [` + strings.Repeat(`{"name": "X-A", "reason": "r"},`, 8) +
				`{"name": "X-B", "reason": "r"}]}`),
			"the limit is 8",
		},
		{
			"a header without a process",
			`{"name": "x", "kind": "module", "license": "MIT", "routes": {"headers": [{"name": "Print-After-Upload", "reason": "r"}]}}`,
			"contract_major is not",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(tc.document), decode)
			if !errors.Is(err, manifest.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}

			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("err = %q, want it to say %q", err, tc.says)
			}
		})
	}
}

// A header the module marks required is read as required, and one that says
// nothing is not.
func TestARequiredHeaderIsRead(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withRoutes(`{
		"headers": [{"name": "Print-After-Upload", "reason": "prints", "required": true}]
	}`)), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !held.Routes.Headers[0].Required {
		t.Errorf("headers = %+v, want the header required", held.Routes.Headers)
	}

	plain, err := manifest.Parse("module.yaml", []byte(withRoutes(`{
		"headers": [{"name": "Print-After-Upload", "reason": "prints"}]
	}`)), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if plain.Routes.Headers[0].Required {
		t.Errorf("headers = %+v, want the header not required", plain.Routes.Headers)
	}
}
