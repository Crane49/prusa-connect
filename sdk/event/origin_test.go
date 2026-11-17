package event

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewOriginValidatesBothHalves(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		kind Kind
		id   string
		ok   bool
	}{
		"a device, whose id is a ULID":   {kind: KindDevice, id: "01JB8XN2K7QW9ZR4MV3T6H0PDA", ok: true},
		"a storage instance, DNS-1123":   {kind: "storage", id: "s3-main", ok: true},
		"a camera":                       {kind: "camera", id: "front", ok: true},
		"a node":                         {kind: "node", id: "prague-node-7", ok: true},
		"at the length limit":            {kind: "storage", id: strings.Repeat("a", maxOriginPart), ok: true},
		"no kind":                        {kind: "", id: "s3-main", ok: false},
		"no id":                          {kind: "storage", id: "", ok: false},
		"one past the length limit":      {kind: "storage", id: strings.Repeat("a", maxOriginPart+1), ok: false},
		"a kind carrying the path split": {kind: "stor/age", id: "s3-main", ok: false},
		"an id carrying the kind split":  {kind: "storage", id: "s3:main", ok: false},
		"an id carrying the list split":  {kind: "storage", id: "s3,main", ok: false},
		"an id carrying the offset sign": {kind: "storage", id: "s3=main", ok: false},
		"an id carrying the group split": {kind: "storage", id: "s3;main", ok: false},
		"an id carrying a newline":       {kind: "storage", id: "s3\nmain", ok: false},
		"an id carrying a carriage ret":  {kind: "storage", id: "s3\rmain", ok: false},
		"an id carrying a space":         {kind: "storage", id: "s3 main", ok: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			origin, err := NewOrigin(tc.kind, tc.id)

			if tc.ok {
				noError(t, err)
				equal(t, tc.kind, origin.Kind())
				equal(t, tc.id, origin.ID())
				isFalse(t, origin.IsZero())

				return
			}

			errorIs(t, err, ErrInvalidOrigin)
			isTrue(t, origin.IsZero(), "a refused origin is the zero value, not a half-built one")
		})
	}
}

// A newline in an id would end the SSE frame early and let the rest of the id
// be read as a field of its own, so it is refused at construction rather than
// escaped at every rendering.
func TestAnOriginNeverBreaksTheFramesItIsRenderedInto(t *testing.T) {
	t.Parallel()

	_, err := NewOrigin(KindDevice, "01JB8X\ndata: injected")
	errorIs(t, err, ErrInvalidOrigin)
}

func TestOriginRenderings(t *testing.T) {
	t.Parallel()

	origin := mustOrigin("storage", "s3-main")

	equal(t, "storage:s3-main", origin.String(), "the subject and the authorization key")
	equal(t, "storage/s3-main", origin.Path(), "the source URN segment and the cursor token")

	parsed, err := ParseOrigin(origin.String())
	noError(t, err)
	equal(t, origin, parsed)

	fromPath, err := ParsePath(origin.Path())
	noError(t, err)
	equal(t, origin, fromPath)

	empty(t, Origin{}.String())
	empty(t, Origin{}.Path())

	for _, malformed := range []string{"", "storage", "storage:", ":s3-main"} {
		_, parseErr := ParseOrigin(malformed)
		errorIs(t, parseErr, ErrInvalidOrigin, "origin %q", malformed)
	}
}

func TestOriginSurvivesJSON(t *testing.T) {
	t.Parallel()

	origin := mustOrigin("camera", "front")

	encoded, err := json.Marshal(struct {
		Origin Origin `json:"origin"`
	}{Origin: origin})
	noError(t, err)
	jsonEq(t, `{"origin":"camera:front"}`, string(encoded))

	var decoded struct {
		Origin Origin `json:"origin"`
	}

	noError(t, json.Unmarshal(encoded, &decoded))
	equal(t, origin, decoded.Origin)
}

// TestSourceURNAddressesAnyResource asserts that a device, a camera, a storage
// instance and a node are all addressable, the kind travelling in the URN as a
// value and not as a fixed segment.
func TestSourceURNAddressesAnyResource(t *testing.T) {
	t.Parallel()

	const site = "01K2A0SPRAGUEFARM07"

	tests := map[string]struct {
		origin Origin
		want   string
	}{
		"a device, whatever machine it is": {
			origin: mustOrigin(KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0PDC"),
			want:   "urn:prusa:site:01K2A0SPRAGUEFARM07/device/01JB8XN2K7QW9ZR4MV3T6H0PDC",
		},
		"a camera, which has no printer": {
			origin: mustOrigin("camera", "front"),
			want:   "urn:prusa:site:01K2A0SPRAGUEFARM07/camera/front",
		},
		"a storage instance, whose transitions are R5 events": {
			origin: mustOrigin("storage", "s3-main"),
			want:   "urn:prusa:site:01K2A0SPRAGUEFARM07/storage/s3-main",
		},
		"a node, which reports about itself": {
			origin: mustOrigin("node", "prague-node-7"),
			want:   "urn:prusa:site:01K2A0SPRAGUEFARM07/node/prague-node-7",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rendered := SourceURN(site, tc.origin)
			equal(t, tc.want, rendered)

			gotSite, gotOrigin, err := ParseSourceURN(rendered)
			noError(t, err)
			equal(t, site, gotSite)
			equal(t, tc.origin, gotOrigin)
		})
	}

	empty(t, SourceURN("", mustOrigin(KindDevice, "a")), "a source with no site names nothing")
	empty(t, SourceURN(site, Origin{}), "a source with no origin names nothing")
}

// TestSourceSurvivesARelayHop is the dedup property: source plus id is the key,
// a relay never rewrites the source, and the kind travels inside it rather than
// being re-derived at the far end.
func TestSourceSurvivesARelayHop(t *testing.T) {
	t.Parallel()

	origin := mustOrigin("storage", "s3-main")

	stamped := Envelope{
		SpecVersion: SpecVersion,
		ID:          "01K3E7T0RES0000000000000AA",
		Source:      SourceURN("01K2A0SPRAGUEFARM07", origin),
		Type:        "com.prusa.connect.resource.degraded",
		Class:       ClassState,
	}
	noError(t, stamped.Validate())

	forwarded, err := stamped.Forward(0)
	noError(t, err)

	equal(t, stamped.Source, forwarded.Source, "a relay rewrote the source")
	equal(t, stamped.ID, forwarded.ID)
	equal(t, uint8(1), forwarded.Hops)

	site, hopped, err := ParseSourceURN(forwarded.Source)
	noError(t, err)
	equal(t, "01K2A0SPRAGUEFARM07", site)
	equal(t, origin, hopped, "the kind did not survive the hop")
}

func TestParseSourceURNRejectsMalformedText(t *testing.T) {
	t.Parallel()

	for _, malformed := range []string{
		"",
		"urn:prusa:site:",
		"urn:prusa:site:site",
		"urn:prusa:site:site/printer",
		"urn:prusa:site:/device/a",
		"https://example.invalid/device/a",
	} {
		_, _, err := ParseSourceURN(malformed)
		errorIs(t, err, ErrInvalidEnvelope, "source %q", malformed)
	}
}
