package fault_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func TestNewDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "reverse dns", input: "io.github.acme.s3"},
		{name: "two labels", input: "dev.prusa"},
		{name: "digits and hyphens", input: "io.github.acme-2.s3x"},
		{name: "core itself", input: "core", wantErr: fault.ErrReservedDomain},
		{name: "core context", input: "core.printer", wantErr: fault.ErrReservedDomain},
		{name: "unknown core context", input: "core.anything", wantErr: fault.ErrReservedDomain},
		{name: "core as a prefix of a longer label is free", input: "coreutils.acme"},
		{name: "empty", input: "", wantErr: fault.ErrInvalidDomain},
		{name: "single label", input: "acme", wantErr: fault.ErrInvalidDomain},
		{name: "uppercase", input: "IO.github.acme", wantErr: fault.ErrInvalidDomain},
		{name: "leading dot", input: ".acme.io", wantErr: fault.ErrInvalidDomain},
		{name: "trailing dot", input: "acme.io.", wantErr: fault.ErrInvalidDomain},
		{name: "empty label", input: "acme..io", wantErr: fault.ErrInvalidDomain},
		// A DNS-1123 label may start with a digit and carry a hyphen, and a
		// module namespace renders in that grammar, so a domain has to accept
		// one or an extension cannot use the namespace the host derived for it
		// (ADR-20260826-6x5 rule 2). github.com/99designs is the shape.
		{name: "label starts with a digit", input: "1acme.io"},
		{name: "inner hyphen", input: "com.github.aux.acme.prusa-driver-mk4"},
		{name: "label ends with a hyphen", input: "acme-.io", wantErr: fault.ErrInvalidDomain},
		{name: "label starts with a hyphen", input: "-acme.io", wantErr: fault.ErrInvalidDomain},
		{name: "underscore", input: "acme_io.s3", wantErr: fault.ErrInvalidDomain},
		{name: "slash", input: "acme.io/s3", wantErr: fault.ErrInvalidDomain},
		{name: "label of 63 bytes", input: strings.Repeat("a", 63) + ".io"},
		{name: "label of 64 bytes", input: strings.Repeat("a", 64) + ".io", wantErr: fault.ErrInvalidDomain},
		{name: "name of 254 bytes", input: strings.Repeat("a", 251) + ".io", wantErr: fault.ErrInvalidDomain},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			domain, err := fault.NewDomain(tt.input)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewDomain(%q) error = %v, want %v", tt.input, err, tt.wantErr)
				}

				if !domain.IsZero() {
					t.Fatalf("NewDomain(%q) returned %q alongside an error", tt.input, domain)
				}

				return
			}

			if err != nil {
				t.Fatalf("NewDomain(%q) unexpected error: %v", tt.input, err)
			}

			if domain.String() != tt.input {
				t.Fatalf("NewDomain(%q) = %q", tt.input, domain)
			}
		})
	}
}

// The core namespace is closed to NewDomain. It is not closed to a tier 1
// extension, which holds the accessors below like any other caller.
func TestCoreNamespaceIsRefusedByNewDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		domain fault.Domain
		want   string
	}{
		{name: "printer", domain: fault.DomainPrinter(), want: "core.printer"},
		{name: "job", domain: fault.DomainJob(), want: "core.job"},
		{name: "auth", domain: fault.DomainAuth(), want: "core.auth"},
		{name: "storage", domain: fault.DomainStorage(), want: "core.storage"},
		{name: "validation", domain: fault.DomainValidation(), want: "core.validation"},
		{name: "resource", domain: fault.DomainResource(), want: "core.resource"},
		{name: "internal", domain: fault.DomainInternal(), want: "core.internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.domain.String() != tt.want {
				t.Fatalf("domain = %q, want %q", tt.domain, tt.want)
			}

			if _, err := fault.NewDomain(tt.want); !errors.Is(err, fault.ErrReservedDomain) {
				t.Fatalf("NewDomain(%q) error = %v, want %v", tt.want, err, fault.ErrReservedDomain)
			}
		})
	}
}

// A client branches on the domain half of a code. An instance that is not there
// and a request that does not parse are different things to do about, so the
// two domains have to disown each other's codes rather than only spell
// differently.
func TestAResourceCodeIsNotAValidationCode(t *testing.T) {
	t.Parallel()

	absent, err := fault.NewCode(fault.DomainResource(), "RESOURCE_NOT_FOUND")
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	malformed, err := fault.NewCode(fault.DomainValidation(), "REQUEST_INVALID")
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	if !fault.DomainResource().Owns(absent) {
		t.Fatalf("%s is not owned by its own domain", absent)
	}

	if fault.DomainValidation().Owns(absent) {
		t.Fatalf("%s reads as a validation failure, so a client corrects a request that was fine", absent)
	}

	if fault.DomainResource().Owns(malformed) {
		t.Fatalf("%s reads as a resource failure", malformed)
	}
}

func TestNewCode(t *testing.T) {
	t.Parallel()

	acme, domainErr := fault.NewDomain("io.github.acme.s3")
	if domainErr != nil {
		t.Fatalf("NewDomain: %v", domainErr)
	}

	tests := []struct {
		name    string
		domain  fault.Domain
		reason  string
		want    string
		wantErr error
	}{
		{
			name:   "core reason",
			domain: fault.DomainPrinter(),
			reason: "COMMAND_REJECTED",
			want:   "core.printer/COMMAND_REJECTED",
		},
		{
			name:   "plugin reason",
			domain: acme,
			reason: "BUCKET_UNREACHABLE",
			want:   "io.github.acme.s3/BUCKET_UNREACHABLE",
		},
		{name: "digits", domain: acme, reason: "HTTP2_REQUIRED", want: "io.github.acme.s3/HTTP2_REQUIRED"},
		{name: "ends in a digit", domain: acme, reason: "SLOT2", want: "io.github.acme.s3/SLOT2"},
		{name: "unminted domain", domain: fault.Domain{}, reason: "BUSY", wantErr: fault.ErrInvalidDomain},
		{name: "empty reason", domain: acme, reason: "", wantErr: fault.ErrInvalidReason},
		{name: "single character", domain: acme, reason: "X", wantErr: fault.ErrInvalidReason},
		{name: "lowercase", domain: acme, reason: "bucket_unreachable", wantErr: fault.ErrInvalidReason},
		{name: "leading underscore", domain: acme, reason: "_BUSY", wantErr: fault.ErrInvalidReason},
		{name: "trailing underscore", domain: acme, reason: "BUSY_", wantErr: fault.ErrInvalidReason},
		{name: "leading digit", domain: acme, reason: "2BUSY", wantErr: fault.ErrInvalidReason},
		{name: "hyphen", domain: acme, reason: "IS-BUSY", wantErr: fault.ErrInvalidReason},
		{name: "space", domain: acme, reason: "IS BUSY", wantErr: fault.ErrInvalidReason},
		{name: "slash", domain: acme, reason: "IS/BUSY", wantErr: fault.ErrInvalidReason},
		{
			name:    "over 63 characters",
			domain:  acme,
			reason:  strings.Repeat("A", 64),
			wantErr: fault.ErrInvalidReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, err := fault.NewCode(tt.domain, tt.reason)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewCode(%q, %q) error = %v, want %v", tt.domain, tt.reason, err, tt.wantErr)
				}

				if !code.IsZero() {
					t.Fatalf("NewCode(%q, %q) returned %q alongside an error", tt.domain, tt.reason, code)
				}

				return
			}

			if err != nil {
				t.Fatalf("NewCode(%q, %q) unexpected error: %v", tt.domain, tt.reason, err)
			}

			if code.String() != tt.want {
				t.Fatalf("NewCode(%q, %q) = %q, want %q", tt.domain, tt.reason, code, tt.want)
			}
		})
	}
}

func TestCodeParts(t *testing.T) {
	t.Parallel()

	code, err := fault.NewCode(fault.DomainPrinter(), "COMMAND_REJECTED")
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	if got := code.String(); got != "core.printer/COMMAND_REJECTED" {
		t.Fatalf("String() = %q", got)
	}

	if got := code.DomainName(); got != "core.printer" {
		t.Fatalf("DomainName() = %q", got)
	}

	if got := code.Reason(); got != "COMMAND_REJECTED" {
		t.Fatalf("Reason() = %q", got)
	}

	if got := code.Anchor(); got != "core-printer-command-rejected" {
		t.Fatalf("Anchor() = %q", got)
	}

	if got := code.TypeURI(); got != fault.TypeBaseURI+"#core-printer-command-rejected" {
		t.Fatalf("TypeURI() = %q", got)
	}

	if !fault.DomainPrinter().Owns(code) {
		t.Fatal("DomainPrinter() does not own its own code")
	}

	if fault.DomainJob().Owns(code) || (fault.Domain{}).Owns(code) {
		t.Fatal("a foreign domain claims the code")
	}
}

func TestZeroCodeNamesNothing(t *testing.T) {
	t.Parallel()

	var zero fault.Code

	if !zero.IsZero() {
		t.Fatal("the zero code does not report itself as zero")
	}

	if zero.String() != "" || zero.TypeURI() != "" || zero.DomainName() != "" || zero.Reason() != "" {
		t.Fatalf("zero code = %q/%q/%q/%q", zero, zero.TypeURI(), zero.DomainName(), zero.Reason())
	}

	if zero.Anchor() != "" {
		t.Fatalf("the zero code anchors at %q", zero.Anchor())
	}

	if _, err := json.Marshal(zero); !errors.Is(err, fault.ErrInvalidCode) {
		t.Fatalf("Marshal(zero code) error = %v, want %v", err, fault.ErrInvalidCode)
	}
}

func TestCodeMarshalsAsText(t *testing.T) {
	t.Parallel()

	code, err := fault.NewCode(fault.DomainPrinter(), "COMMAND_REJECTED")
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	encoded, err := json.Marshal(code)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if string(encoded) != `"core.printer/COMMAND_REJECTED"` {
		t.Fatalf("Marshal = %s", encoded)
	}
}
