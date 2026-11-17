package routesapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// A person's request reaches a module without the person's credentials or
// the route it took, and with what a route needs to answer it.
func TestARequestReachesAModuleWithoutCredentials(t *testing.T) {
	t.Parallel()

	headers := http.Header{}
	for _, name := range []string{
		"Cookie", "Authorization", "Proxy-Authorization", "Forwarded", "X-Forwarded-For",
		"X-Forwarded-Host", "X-Real-Ip", "Origin", "Referer", "X-Csrf-Token",
	} {
		headers.Set(name, "secret")
	}

	kept := map[string]string{
		"Accept": "application/json", "Content-Type": "application/json", "If-None-Match": `"3"`,
		"Range": "bytes=0-9", "Accept-Language": "cs",
	}
	for name, value := range kept {
		headers.Set(name, value)
	}

	passed := routesapi.RequestHeaders(headers)

	if len(passed) != len(kept) {
		t.Fatalf("passed %v", passed)
	}

	for name, value := range kept {
		if got := passed[name]; len(got) != 1 || got[0] != value {
			t.Errorf("%s is %v, want %q", name, got, value)
		}
	}
}

// A module's answer reaches the person without a cookie, a security header
// of the core's origin or a redirect, and with what describes the body.
func TestAModuleCannotSetTheCoresHeaders(t *testing.T) {
	t.Parallel()

	answer := routesapi.Response{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Set-Cookie":                          {"session=planted; Path=/"},
			"Content-Security-Policy":             {"default-src *"},
			"Content-Security-Policy-Report-Only": {"default-src *"},
			"Strict-Transport-Security":           {"max-age=0"},
			"Access-Control-Allow-Origin":         {"*"},
			"Access-Control-Allow-Credentials":    {"true"},
			"X-Frame-Options":                     {"ALLOWALL"},
			"Location":                            {"https://example.com/"},
			"Connection":                          {"close"},
			"content-type":                        {"application/json"},
			"ETag":                                {`"3"`},
			"Cache-Control":                       {"private, no-cache"},
			"X-Content-Type-Options":              {"nosniff"},
		},
		Body: []byte("{}"),
	}

	recorded := httptest.NewRecorder()
	if err := answer.Write(recorded); err != nil {
		t.Fatal(err)
	}

	written := recorded.Header()
	if len(written) != 4 {
		t.Fatalf("wrote %v", written)
	}

	for _, name := range []string{"Content-Type", "Etag", "Cache-Control", "X-Content-Type-Options"} {
		if written.Get(name) == "" {
			t.Errorf("%s was dropped", name)
		}
	}
}

// A call from one module reaches the other without the caller's credentials,
// so a credential one module captured goes no further.
func TestACallPassesNoCredentialOn(t *testing.T) {
	t.Parallel()

	params := routesapi.CallParams{
		Module: "com.example.planner", Method: http.MethodGet, Path: "/jobs",
		Headers: map[string][]string{
			"Authorization": {"Bearer captured"}, "Cookie": {"session=captured"},
			"If-None-Match": {`"7"`},
		},
	}

	request := params.Request("user:1")

	if len(request.Headers) != 1 || request.Headers["If-None-Match"][0] != `"7"` {
		t.Fatalf("the other module received %v", request.Headers)
	}
}
