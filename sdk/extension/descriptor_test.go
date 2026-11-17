package extension_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

const hostContract = extension.Version("1.2.0")

// complete is a descriptor with every mandatory field filled in, so a case that
// removes one is testing that field and nothing else.
func complete() extension.Doc {
	return extension.Doc{
		Summary:     "Publishes domain events to a broker.",
		Description: "A longer paragraph an author reads before choosing this entry.",
		Version:     "2.1.0",
		Contract:    hostContract,
		Stability:   extension.StabilityStable,
	}
}

func TestAMissingMandatoryFieldIsARegistrationError(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		mutate  func(*extension.Doc)
		wantErr error
	}{
		{name: "no summary", mutate: func(d *extension.Doc) { d.Summary = "" }, wantErr: extension.ErrUndocumented},
		{
			name:    "no description",
			mutate:  func(d *extension.Doc) { d.Description = "" },
			wantErr: extension.ErrUndocumented,
		},
		{name: "no version", mutate: func(d *extension.Doc) { d.Version = "" }, wantErr: extension.ErrUndocumented},
		{name: "no contract", mutate: func(d *extension.Doc) { d.Contract = "" }, wantErr: extension.ErrUndocumented},
		{name: "no stability", mutate: func(d *extension.Doc) { d.Stability = "" }, wantErr: extension.ErrUndocumented},
		{
			name:    "a stability outside the closed set",
			mutate:  func(d *extension.Doc) { d.Stability = "mostly-fine" },
			wantErr: extension.ErrUndocumented,
		},
		{
			name: "deprecated with nothing to move to",
			mutate: func(d *extension.Doc) {
				d.Stability = extension.StabilityDeprecated
			},
			wantErr: extension.ErrUndocumented,
		},
		{
			name: "a replacement on an entry that is not deprecated",
			mutate: func(d *extension.Doc) {
				d.ReplacedBy = "something-else"
			},
			wantErr: extension.ErrUndocumented,
		},
		{
			name:    "a version that is not MAJOR.MINOR.PATCH",
			mutate:  func(d *extension.Doc) { d.Version = "2.1" },
			wantErr: extension.ErrInvalidVersion,
		},
		{
			name: "a config key with no summary",
			mutate: func(d *extension.Doc) {
				d.Options = []extension.Option{{Key: "url", Type: extension.OptionURL}}
			},
			wantErr: extension.ErrUndocumented,
		},
		{
			name: "a config key with a type outside the closed set",
			mutate: func(d *extension.Doc) {
				d.Options = []extension.Option{{Key: "url", Type: "hostname", Summary: "the broker"}}
			},
			wantErr: extension.ErrUndocumented,
		},
		{
			name: "a required config key that also has a default",
			mutate: func(d *extension.Doc) {
				d.Options = []extension.Option{
					{Key: "url", Type: extension.OptionURL, Required: true, Default: "tcp://x", Summary: "the broker"},
				}
			},
			wantErr: extension.ErrUndocumented,
		},
		{
			name: "an emitted event with no summary",
			mutate: func(d *extension.Doc) {
				d.Emits = []extension.Emission{{Type: "job.updated", Version: "1.0.0"}}
			},
			wantErr: extension.ErrUndocumented,
		},
		{
			name: "an emitted event with no version of its own",
			mutate: func(d *extension.Doc) {
				d.Emits = []extension.Emission{{Type: "job.updated", Summary: "a job changed"}}
			},
			wantErr: extension.ErrInvalidVersion,
		},
		{
			name: "a destination with no address",
			mutate: func(d *extension.Doc) {
				d.Publishes = []extension.Destination{{Version: "1.0.0", Summary: "a job changed"}}
			},
			wantErr: extension.ErrUndocumented,
		},
		{
			name: "a destination with no summary",
			mutate: func(d *extension.Doc) {
				d.Publishes = []extension.Destination{{Address: "v1/jobs/{jobId}/state", Version: "1.0.0"}}
			},
			wantErr: extension.ErrUndocumented,
		},
		{
			name: "a destination with no version of its own",
			mutate: func(d *extension.Doc) {
				d.Publishes = []extension.Destination{
					{Address: "v1/jobs/{jobId}/state", Summary: "a job changed"},
				}
			},
			wantErr: extension.ErrInvalidVersion,
		},
		{
			name: "a host requirement with no capability",
			mutate: func(d *extension.Doc) {
				d.Requires = []extension.Requirement{{Since: "1.0.0"}}
			},
			wantErr: extension.ErrUndocumented,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			doc := complete()
			testCase.mutate(&doc)

			_, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc)
			if !errors.Is(err, testCase.wantErr) {
				t.Errorf("an entry with %s was registered: %v", testCase.name, err)
			}
		})
	}
}

// Every descriptor string is published, most of them into a markdown table or a
// heading, so a newline or a control character in one is a structural break in
// somebody else's section. It is refused where the value enters the registry,
// because that is the one place every publisher reads from.
func TestTextThatWouldBreakThePublishedPageIsRefused(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		mutate func(*extension.Doc)
	}{
		{
			name:   "a newline in the description forges a section of its own",
			mutate: func(d *extension.Doc) { d.Description = "fine.\n\n### core/filesystem\n\nnot fine." },
		},
		{
			name:   "a newline in the summary",
			mutate: func(d *extension.Doc) { d.Summary = "fine.\n| broken |" },
		},
		{
			name: "a newline in a config key breaks the table",
			mutate: func(d *extension.Doc) {
				d.Options = []extension.Option{{Key: "url\n| x |", Type: extension.OptionURL, Summary: "the broker"}}
			},
		},
		{
			name: "a carriage return in a config default",
			mutate: func(d *extension.Doc) {
				d.Options = []extension.Option{
					{Key: "url", Type: extension.OptionURL, Default: "a\rb", Summary: "the broker"},
				}
			},
		},
		{
			name: "a control character in an event type",
			mutate: func(d *extension.Doc) {
				d.Emits = []extension.Emission{{Type: "job\x00updated", Version: "1.0.0", Summary: "a job changed"}}
			},
		},
		{
			name: "a newline in a required capability",
			mutate: func(d *extension.Doc) {
				d.Requires = []extension.Requirement{{Capability: "core.event\n.stream", Since: "1.0.0"}}
			},
		},
		{
			name:   "a line separator, which ends a line without being a control character",
			mutate: func(d *extension.Doc) { d.Summary = "fine.\u2028### core/filesystem" },
		},
		{
			name: "a replacement that is not a name",
			mutate: func(d *extension.Doc) {
				d.Stability = extension.StabilityDeprecated
				d.ReplacedBy = "webhook-v2`\n\n### core/filesystem"
			},
		},
		{
			name: "a backtick in a config default, which is published inside a code span",
			mutate: func(d *extension.Doc) {
				d.Options = []extension.Option{
					{Key: "url", Type: extension.OptionURL, Default: "` <img src=x> `", Summary: "the broker"},
				}
			},
		},
		{
			name: "a backtick in an event type",
			mutate: func(d *extension.Doc) {
				d.Emits = []extension.Emission{{Type: "job`.updated", Version: "1.0.0", Summary: "a job changed"}}
			},
		},
		{
			name: "a backtick in a destination address, which is published inside a code span",
			mutate: func(d *extension.Doc) {
				d.Publishes = []extension.Destination{
					{Address: "v1/jobs/`x`/state", Version: "1.0.0", Summary: "a job changed"},
				}
			},
		},
		{
			name: "a newline in a destination address",
			mutate: func(d *extension.Doc) {
				d.Publishes = []extension.Destination{
					{Address: "v1/jobs\n### core/filesystem", Version: "1.0.0", Summary: "a job changed"},
				}
			},
		},
		{
			name: "a config key that is not lower_snake_case",
			mutate: func(d *extension.Doc) {
				d.Options = []extension.Option{{Key: "Queue-Depth", Type: extension.OptionInt, Summary: "how many"}}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			doc := complete()
			testCase.mutate(&doc)

			_, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc)
			if err == nil {
				t.Errorf("a descriptor with %s was accepted", testCase.name)
			}
		})
	}
}

// An empty string is refused because it would publish as documentation. A string
// of spaces publishes as the same empty cell.
func TestWhitespaceIsNotDocumentation(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		mutate func(*extension.Doc)
	}{
		{name: "a summary of three spaces", mutate: func(d *extension.Doc) { d.Summary = "   " }},
		{name: "a description of two spaces", mutate: func(d *extension.Doc) { d.Description = "  " }},
		{
			name: "a config key summary of spaces",
			mutate: func(d *extension.Doc) {
				d.Options = []extension.Option{{Key: "url", Type: extension.OptionURL, Summary: "  "}}
			},
		},
		{
			name: "an event summary of spaces",
			mutate: func(d *extension.Doc) {
				d.Emits = []extension.Emission{{Type: "job.updated", Version: "1.0.0", Summary: " "}}
			},
		},
		{
			name: "a destination summary of spaces",
			mutate: func(d *extension.Doc) {
				d.Publishes = []extension.Destination{
					{Address: "v1/jobs/{jobId}/state", Version: "1.0.0", Summary: " "},
				}
			},
		},
		{
			name: "a deprecation whose replacement is a space",
			mutate: func(d *extension.Doc) {
				d.Stability = extension.StabilityDeprecated
				d.ReplacedBy = " "
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			doc := complete()
			testCase.mutate(&doc)

			_, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc)
			if !errors.Is(err, extension.ErrUndocumented) {
				t.Errorf("an entry with %s was registered: %v", testCase.name, err)
			}
		})
	}
}

func TestACompleteDescriptorIsAccepted(t *testing.T) {
	t.Parallel()

	doc := complete()
	doc.Options = []extension.Option{
		{Key: "url", Type: extension.OptionURL, Required: true, Summary: "the broker"},
		{Key: "queue_depth", Type: extension.OptionInt, Default: "256", Summary: "how many wait"},
	}
	doc.Emits = []extension.Emission{{Type: "job.updated", Version: "1.0.0", Summary: "a job changed"}}
	doc.Publishes = []extension.Destination{
		{Address: "v1/jobs/{jobId}/state", Version: "1.0.0", Summary: "a job changed"},
	}
	doc.Requires = []extension.Requirement{{Capability: "core.event.stream", Since: "1.0.0"}}

	descriptor, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc)
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	if descriptor.Namespace().IsZero() {
		t.Error("the descriptor carries no namespace")
	}

	if descriptor.Module() == "" {
		t.Error("the descriptor names no module, so a collision cannot say where the other entry came from")
	}

	if descriptor.Qualified() != descriptor.Namespace().String()+"/broker" {
		t.Errorf("the qualified name %q is not the namespace and the name", descriptor.Qualified())
	}
}

func TestAContractTheHostDoesNotServeIsRefused(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		contract extension.Version
		wantErr  error
	}{
		{name: "a later major", contract: "2.0.0", wantErr: extension.ErrIncompatibleContract},
		{name: "an earlier major", contract: "0.9.0", wantErr: extension.ErrIncompatibleContract},
		{name: "a minor the host does not have", contract: "1.3.0", wantErr: extension.ErrIncompatibleContract},
		{name: "not a version at all", contract: "latest", wantErr: extension.ErrInvalidVersion},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			doc := complete()
			doc.Contract = testCase.contract

			_, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc)
			if !errors.Is(err, testCase.wantErr) {
				t.Errorf("an entry built against %s was registered on a %s host: %v",
					testCase.contract, hostContract, err)
			}
		})
	}
}

// An older minor still compiles against this host: everything it asks for is
// present. Refusing it would make every host release a breaking one.
func TestAnEarlierMinorOfTheSameMajorIsAccepted(t *testing.T) {
	t.Parallel()

	doc := complete()
	doc.Contract = "1.0.0"

	if _, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc); err != nil {
		t.Errorf("an entry built against 1.0.0 was refused on a 1.2.0 host: %v", err)
	}
}

func TestAnInvalidNameIsRefused(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"", "Uppercase", "under_score", "-leading", "trailing-", "with/slash", strings.Repeat("a", 64),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := extension.Declare(extension.KindEventSink, hostContract, name, complete())
			if !errors.Is(err, extension.ErrInvalidName) {
				t.Errorf("%q was accepted as a name: %v", name, err)
			}
		})
	}
}

func TestAnInvalidKindIsRefused(t *testing.T) {
	t.Parallel()

	_, err := extension.Declare("middleware", hostContract, "broker", complete())
	if !errors.Is(err, extension.ErrInvalidKind) {
		t.Errorf("an entry outside the closed set of extension points was registered: %v", err)
	}
}

func TestADescriptorRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()

	doc := complete()
	doc.Options = []extension.Option{{Key: "url", Type: extension.OptionURL, Required: true, Summary: "the broker"}}
	doc.Emits = []extension.Emission{{Type: "job.updated", Version: "1.0.0", Summary: "a job changed"}}
	doc.Publishes = []extension.Destination{
		{Address: "v1/jobs/{jobId}/state", Version: "1.0.0", Summary: "a job changed"},
	}

	descriptor, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc)
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	encoded, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded extension.Descriptor
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	again, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("Marshal again: %v", err)
	}

	if string(again) != string(encoded) {
		t.Errorf("a descriptor did not round-trip:\n%s\n%s", encoded, again)
	}
}
