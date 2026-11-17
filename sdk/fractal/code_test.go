package fractal_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestDefinedCodeValues tests the value of every code in the Error Codes table
// of fractal.md. It also tests what the bits of each value say: whether the
// caller caused the failure, and whether the callee decided to refuse.
func TestDefinedCodeValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		code        fractal.Code
		value       uint16
		constant    string
		caller      bool
		intentional bool
	}{
		{name: "ERR_INTERNAL", code: fractal.CodeInternal, value: 0x0010, constant: "ERR_INTERNAL"},
		{
			name:     "ERR_JOIN_UNAVAILABLE",
			code:     fractal.CodeJoinUnavailable,
			value:    0x0020,
			constant: "ERR_JOIN_UNAVAILABLE",
		},
		{name: "ERR_DENIED", code: fractal.CodeDenied, value: 0x2010, constant: "ERR_DENIED", intentional: true},
		{name: "ERR_EXPIRED", code: fractal.CodeExpired, value: 0x2020, constant: "ERR_EXPIRED", intentional: true},
		{
			name:        "ERR_JOIN_UNAUTHORIZED",
			code:        fractal.CodeJoinUnauthorized,
			value:       0x2030,
			constant:    "ERR_JOIN_UNAUTHORIZED",
			intentional: true,
		},
		{
			name:        "ERR_JOIN_COLLISION",
			code:        fractal.CodeJoinCollision,
			value:       0x2040,
			constant:    "ERR_JOIN_COLLISION",
			intentional: true,
		},
		{
			name:        "ERR_JOIN_REVOKED",
			code:        fractal.CodeJoinRevoked,
			value:       0x2050,
			constant:    "ERR_JOIN_REVOKED",
			intentional: true,
		},
		{
			name:     "ERR_INVALID_PARAMS",
			code:     fractal.CodeInvalidParams,
			value:    0x4010,
			constant: "ERR_INVALID_PARAMS",
			caller:   true,
		},
		{
			name:     "ERR_NODE_NOT_FOUND",
			code:     fractal.CodeNodeNotFound,
			value:    0x4020,
			constant: "ERR_NODE_NOT_FOUND",
			caller:   true,
		},
		{
			name:     "ERR_JOIN_REJECTED",
			code:     fractal.CodeJoinRejected,
			value:    0x4030,
			constant: "ERR_JOIN_REJECTED",
			caller:   true,
		},
		{
			name:     "ERR_OBJECT_NOT_FOUND",
			code:     fractal.CodeObjectNotFound,
			value:    0x4040,
			constant: "ERR_OBJECT_NOT_FOUND",
			caller:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if uint16(tc.code) != tc.value {
				t.Errorf("value = %#04x, want %#04x", uint16(tc.code), tc.value)
			}
			if got, ok := tc.code.Name(); !ok || got != tc.constant {
				t.Errorf("Name = %q %v, want %q", got, ok, tc.constant)
			}
			if got := tc.code.String(); got != tc.constant {
				t.Errorf("String = %q, want %q", got, tc.constant)
			}
			if tc.code.CallerSide() != tc.caller || tc.code.CalleeSide() == tc.caller {
				t.Errorf("CallerSide = %v, want %v", tc.code.CallerSide(), tc.caller)
			}
			if tc.code.Intentional() != tc.intentional {
				t.Errorf("Intentional = %v, want %v", tc.code.Intentional(), tc.intentional)
			}
			if !tc.code.ProtocolDefined() || tc.code.ApplicationDefined() {
				t.Error("ProtocolDefined is false, or ApplicationDefined is true, for a code fractal.md defines")
			}
			if tc.code.Refinement() != 0 {
				t.Error("a code fractal.md defines has a refinement that is not zero")
			}
			if !tc.code.Valid() {
				t.Error("Valid returned false for a code fractal.md defines")
			}
		})
	}
	if got := len(fractal.DefinedCodes()); got != len(tests) {
		t.Fatalf("DefinedCodes returned %d codes, want %d", got, len(tests))
	}
}

// TestCodeExamples tests each line of the Examples block of the Error Codes
// section of fractal.md.
func TestCodeExamples(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		code       fractal.Code
		base       fractal.Code
		refinement uint8
		rendered   string
		app        bool
	}{
		{name: "0x0010 ERR_INTERNAL base", code: 0x0010, base: fractal.CodeInternal, rendered: "ERR_INTERNAL"},
		{name: "0x2010 ERR_DENIED base", code: 0x2010, base: fractal.CodeDenied, rendered: "ERR_DENIED"},
		{
			name:       "0x2011 ERR_DENIED refine 1",
			code:       0x2011,
			base:       fractal.CodeDenied,
			refinement: 1,
			rendered:   "ERR_DENIED+1",
		},
		{
			name:       "0x2012 ERR_DENIED refine 2",
			code:       0x2012,
			base:       fractal.CodeDenied,
			refinement: 2,
			rendered:   "ERR_DENIED+2",
		},
		{
			name:     "0x4010 ERR_INVALID_PARAMS base",
			code:     0x4010,
			base:     fractal.CodeInvalidParams,
			rendered: "ERR_INVALID_PARAMS",
		},
		{name: "0x8010 application defined", code: 0x8010, base: 0x8010, rendered: "0x8010", app: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.code.Base(); got != tc.base {
				t.Errorf("Base = %s, want %s", got, tc.base)
			}
			if got := tc.code.Refinement(); got != tc.refinement {
				t.Errorf("Refinement = %d, want %d", got, tc.refinement)
			}
			if got := tc.code.String(); got != tc.rendered {
				t.Errorf("String = %q, want %q", got, tc.rendered)
			}
			if tc.code.ApplicationDefined() != tc.app {
				t.Errorf("ApplicationDefined = %v, want %v", tc.code.ApplicationDefined(), tc.app)
			}
			if !tc.code.Valid() {
				t.Error("Valid returned false for a code from the examples of fractal.md")
			}
		})
	}
}

// TestCodeIs tests the match rule of fractal.md: a code matches a base code
// whatever refinement the code has, and it does not match another base code.
func TestCodeIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code fractal.Code
		base fractal.Code
		want bool
	}{
		{name: "base against itself", code: 0x2010, base: fractal.CodeDenied, want: true},
		{name: "refinement one", code: 0x2011, base: fractal.CodeDenied, want: true},
		{name: "refinement fifteen", code: 0x201F, base: fractal.CodeDenied, want: true},
		{name: "another base code", code: 0x2020, base: fractal.CodeDenied},
		{name: "a caller-side code against a callee-side base", code: 0x4010, base: fractal.CodeDenied},
		{name: "expired against expired", code: 0x2021, base: fractal.CodeExpired, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.code.Is(tc.base); got != tc.want {
				t.Fatalf("%s.Is(%s) = %v, want %v", tc.code, tc.base, got, tc.want)
			}
		})
	}
	if !fractal.Code(0x2010).Is(0x2010) {
		t.Error("Is returned false for a base code compared with itself")
	}
}

// TestCodeRefine tests that Refine sets the refinement bits of a base code, and
// refuses a refinement over 15 and a code that already has a refinement.
func TestCodeRefine(t *testing.T) {
	t.Parallel()
	got, err := fractal.CodeDenied.Refine(1)
	if err != nil || got != 0x2011 {
		t.Fatalf("Refine(1) = %#04x %v", uint16(got), err)
	}
	if _, overflow := fractal.CodeDenied.Refine(16); overflow == nil {
		t.Error("Refine accepted a refinement over 15")
	}
	if _, twice := fractal.Code(0x2011).Refine(2); twice == nil {
		t.Error("Refine accepted a code that already has a refinement")
	}
}

// TestCodeReservedBits tests the rule of fractal.md that bits 12 to 9 of a
// protocol-defined code are zero.
func TestCodeReservedBits(t *testing.T) {
	t.Parallel()
	for _, c := range fractal.DefinedCodes() {
		if !c.ReservedBitsZero() {
			t.Errorf("%s has a reserved bit set", c)
		}
	}
	if fractal.Code(0x0210).ReservedBitsZero() {
		t.Error("ReservedBitsZero returned true for 0x0210, which has bit 9 set")
	}
	if fractal.Code(0x0210).Valid() {
		t.Error("Valid returned true for the protocol-defined code 0x0210, which has a reserved bit set")
	}
	if !fractal.Code(0x8210).Valid() {
		t.Error("Valid returned false for the application-defined code 0x8210, which may set bits 12 to 9")
	}
}

// TestCodeReservedRange tests the rule of fractal.md that a node must not send
// a code in 0x6000 to 0x61FF.
func TestCodeReservedRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		code     fractal.Code
		reserved bool
	}{
		{name: "below the range", code: 0x5FFF},
		{name: "first of the range", code: 0x6000, reserved: true},
		{name: "inside the range", code: 0x6100, reserved: true},
		{name: "last of the range", code: 0x61FF, reserved: true},
		{name: "above the range", code: 0x6200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.code.InReservedRange(); got != tc.reserved {
				t.Errorf("InReservedRange = %v, want %v", got, tc.reserved)
			}
			if tc.reserved && tc.code.Valid() {
				t.Error("Valid returned true for a code in the reserved range")
			}
		})
	}
	if fractal.ReservedRangeFirst != 0x6000 || fractal.ReservedRangeLast != 0x61FF {
		t.Fatal("ReservedRangeFirst and ReservedRangeLast are not 0x6000 and 0x61FF")
	}
}

// TestCodeNameOfAnUnknownCode tests that Name returns ok false for a code
// fractal.md does not define, and that String still names the base code of a
// refinement.
func TestCodeNameOfAnUnknownCode(t *testing.T) {
	t.Parallel()
	if _, ok := fractal.Code(0x8010).Name(); ok {
		t.Error("Name returned ok for the application-defined code 0x8010")
	}
	if _, ok := fractal.Code(0x2011).Name(); ok {
		t.Error("Name returned ok for 0x2011, a refinement of ERR_DENIED")
	}
	if got := fractal.Code(0x0011).String(); got != "ERR_INTERNAL+1" {
		t.Fatalf("String = %q", got)
	}
}
