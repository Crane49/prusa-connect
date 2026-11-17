package fractal_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestJoinRequestValidate tests that Validate accepts a join request with and
// without a join credential, and refuses one with an unsupported version, an
// invalid Node ID or no identity.
func TestJoinRequestValidate(t *testing.T) {
	t.Parallel()
	good := fractal.JoinRequest{Version: 1, NodeID: "printer-1", Identity: "UABC", Credential: "token"}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate refused a valid request: %v", err)
	}
	noCredential := good
	noCredential.Credential = ""
	if err := noCredential.Validate(); err != nil {
		t.Fatalf("Validate refused a request with no join credential, which a deployment may allow: %v", err)
	}

	tests := []struct {
		name string
		req  fractal.JoinRequest
		is   error
	}{
		{
			name: "unsupported version",
			req:  fractal.JoinRequest{Version: 2, NodeID: "a", Identity: "U"},
			is:   fractal.ErrUnsupportedVersion,
		},
		{
			name: "invalid node id",
			req:  fractal.JoinRequest{Version: 1, NodeID: "-a", Identity: "U"},
			is:   fractal.ErrBadFirstCharacter,
		},
		{name: "no identity", req: fractal.JoinRequest{Version: 1, NodeID: "a"}, is: fractal.ErrMissingIdentity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.req.Validate(); !errors.Is(err, tc.is) {
				t.Fatalf("Validate = %v, want %v", err, tc.is)
			}
		})
	}
}

// TestJoinGrantValidate tests that Validate requires every field of a join
// grant: a supported version, a valid Node Path, the identity of the sender,
// connection credentials and the time they expire.
func TestJoinGrantValidate(t *testing.T) {
	t.Parallel()
	good := fractal.JoinGrant{
		Version: 1, NodePath: "hq/brno/printer-1", ParentIdentity: "UPARENT",
		Credentials: "jwt", ExpiresAt: 1751400055000,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate refused a valid grant: %v", err)
	}
	if got := good.ParentIdentity.String(); got != "UPARENT" {
		t.Fatalf("Identity.String = %q", got)
	}

	tests := []struct {
		name  string
		grant fractal.JoinGrant
		is    error
	}{
		{
			name:  "unsupported version",
			grant: fractal.JoinGrant{Version: 2, NodePath: "hq", ParentIdentity: "U", Credentials: "c", ExpiresAt: 1},
			is:    fractal.ErrUnsupportedVersion,
		},
		{
			name: "invalid node path",
			grant: fractal.JoinGrant{
				Version:        1,
				NodePath:       "hq//a",
				ParentIdentity: "U",
				Credentials:    "c",
				ExpiresAt:      1,
			},
			is: fractal.ErrEmptySegment,
		},
		{
			name:  "no parent identity",
			grant: fractal.JoinGrant{Version: 1, NodePath: "hq", Credentials: "c", ExpiresAt: 1},
			is:    fractal.ErrMissingIdentity,
		},
		{
			name:  "no credentials",
			grant: fractal.JoinGrant{Version: 1, NodePath: "hq", ParentIdentity: "U", ExpiresAt: 1},
			is:    fractal.ErrMissingCredentials,
		},
		{
			name:  "no expiry",
			grant: fractal.JoinGrant{Version: 1, NodePath: "hq", ParentIdentity: "U", Credentials: "c"},
			is:    fractal.ErrMissingExpiry,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.grant.Validate(); !errors.Is(err, tc.is) {
				t.Fatalf("Validate = %v, want %v", err, tc.is)
			}
		})
	}
}

// TestJoinDenialValidate tests the rule that the code of a join denial is one of
// the join denial codes, or a refinement of one.
func TestJoinDenialValidate(t *testing.T) {
	t.Parallel()
	for _, code := range fractal.JoinDenialCodes() {
		denial := fractal.JoinDenial{Version: 1, Code: code, Reason: "no"}
		if bad := denial.Validate(); bad != nil {
			t.Errorf("Validate refused a denial with the join denial code %s: %v", code, bad)
		}
	}
	refined, err := fractal.CodeJoinUnauthorized.Refine(3)
	if err != nil {
		t.Fatalf("Refine: %v", err)
	}
	if bad := (fractal.JoinDenial{Version: 1, Code: refined}).Validate(); bad != nil {
		t.Errorf("Validate refused a denial with a refinement of a join denial code: %v", bad)
	}

	tests := []struct {
		name   string
		denial fractal.JoinDenial
		is     error
	}{
		{
			name:   "unsupported version",
			denial: fractal.JoinDenial{Version: 2, Code: fractal.CodeJoinRevoked},
			is:     fractal.ErrUnsupportedVersion,
		},
		{
			name:   "a code for a failed RPC",
			denial: fractal.JoinDenial{Version: 1, Code: fractal.CodeInvalidParams},
			is:     fractal.ErrNotAJoinDenialCode,
		},
		{
			name:   "an application code",
			denial: fractal.JoinDenial{Version: 1, Code: 0x8010},
			is:     fractal.ErrNotAJoinDenialCode,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if bad := tc.denial.Validate(); !errors.Is(bad, tc.is) {
				t.Fatalf("Validate = %v, want %v", bad, tc.is)
			}
		})
	}
}

// TestJoinRetry tests the retry rule of fractal.md: the code of a join denial
// decides whether the node sends its join request again.
func TestJoinRetry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		code  fractal.Code
		retry fractal.Retry
		ok    bool
	}{
		{name: "ERR_JOIN_REJECTED", code: fractal.CodeJoinRejected, retry: fractal.RetryNever, ok: true},
		{name: "ERR_JOIN_COLLISION", code: fractal.CodeJoinCollision, retry: fractal.RetryNever, ok: true},
		{name: "ERR_JOIN_REVOKED", code: fractal.CodeJoinRevoked, retry: fractal.RetryNever, ok: true},
		{
			name:  "ERR_JOIN_UNAUTHORIZED",
			code:  fractal.CodeJoinUnauthorized,
			retry: fractal.RetryWithNewCredential,
			ok:    true,
		},
		{name: "ERR_JOIN_UNAVAILABLE", code: fractal.CodeJoinUnavailable, retry: fractal.RetryWithBackoff, ok: true},
		{name: "a refinement has the rule of its base code", code: 0x2052, retry: fractal.RetryNever, ok: true},
		{name: "ERR_DENIED is not a denial code", code: fractal.CodeDenied},
		{name: "ERR_INTERNAL is not a denial code", code: fractal.CodeInternal},
		{name: "an application code is not a denial code", code: 0x8010},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := fractal.JoinRetry(tc.code)
			if ok != tc.ok || got != tc.retry {
				t.Fatalf("JoinRetry(%s) = %s %v, want %s %v", tc.code, got, ok, tc.retry, tc.ok)
			}
			if fractal.IsJoinDenialCode(tc.code) != tc.ok {
				t.Fatalf("IsJoinDenialCode(%s) = %v, want %v", tc.code, !tc.ok, tc.ok)
			}
		})
	}
	codes := fractal.JoinDenialCodes()
	if !slices.Contains(codes, fractal.CodeJoinRevoked) || len(codes) != 5 {
		t.Fatalf("JoinDenialCodes = %v", codes)
	}
}

// TestRetryNames tests the string Retry.String returns for each retry rule.
func TestRetryNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		retry fractal.Retry
		want  string
	}{
		{retry: fractal.RetryNever, want: "never"},
		{retry: fractal.RetryWithNewCredential, want: "with a new credential"},
		{retry: fractal.RetryWithBackoff, want: "with backoff"},
		{retry: 0, want: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.retry.String(); got != tc.want {
				t.Fatalf("String = %q, want %q", got, tc.want)
			}
		})
	}
}
