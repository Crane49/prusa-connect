package event

import (
	"encoding/json"
	"fmt"
	"testing"
)

const epoch = "01K2A0S3P4"

var (
	printerA = mustOrigin(KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0PDA")
	printerB = mustOrigin(KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0PDB")
	quiet    = mustOrigin(KindDevice, "01PRINTERWITHNOEVENTSYET00")
	absent   = mustOrigin(KindDevice, "01PRINTERTHATISNOTNAMED000")
	s3Main   = mustOrigin("storage", "s3-main")
)

func mustOrigin(kind Kind, id string) Origin {
	origin, err := NewOrigin(kind, id)
	if err != nil {
		panic(err)
	}

	return origin
}

func TestNewGenerationIsCanonical(t *testing.T) {
	t.Parallel()

	generation := NewGeneration(epoch, []Position{
		{Origin: printerB, Offset: 7},
		{Origin: quiet, Offset: 0},
		{Origin: s3Main, Offset: 3},
		{Origin: printerA, Offset: 914233},
	})

	equal(t, []Position{
		{Origin: printerA, Offset: 914233},
		{Origin: printerB, Offset: 7},
		{Origin: s3Main, Offset: 3},
	}, generation.Positions,
		"origin order is canonical, and an origin at offset zero is not named")
}

func TestCursorRoundTripsThroughItsWireForm(t *testing.T) {
	t.Parallel()

	cursor := NewCursor(NewGeneration(epoch, []Position{
		{Origin: printerA, Offset: 914233},
		{Origin: printerB, Offset: 7},
	}))

	equal(t,
		"01K2A0S3P4:device/01JB8XN2K7QW9ZR4MV3T6H0PDA=914233,device/01JB8XN2K7QW9ZR4MV3T6H0PDB=7",
		cursor.String())

	parsed, err := ParseCursor(cursor.String())
	noError(t, err)
	equal(t, cursor, parsed)

	generation, held := cursor.Generation(epoch)
	isTrue(t, held)

	offset, named := generation.Offset(printerB)
	isTrue(t, named)
	equal(t, uint64(7), offset)

	_, named = generation.Offset(absent)
	isFalse(t, named)

	_, held = cursor.Generation("01ANOTHERGENERATION0000000")
	isFalse(t, held, "a generation this cursor does not name holds no place")

	live, err := ParseCursor(epoch)
	noError(t, err)
	empty(t, live.Generations[0].Positions,
		"an epoch alone is a reader that has spoken to that node and holds no place in it")

	isTrue(t, Cursor{}.IsZero())
	empty(t, Cursor{}.String())
}

// TestOneOriginsEpochDoesNotInvalidateAnother is what scoping an epoch per
// generation buys: an epoch is minted by the process that assigns the offsets
// under it, so a parent reading two children keeps one child's places when the
// other restarts. A single epoch spanning the whole cursor makes every restart
// a fleet-wide resync.
func TestOneOriginsEpochDoesNotInvalidateAnother(t *testing.T) {
	t.Parallel()

	const (
		childA = "01CHILDAAAAAAAAAAAAAAAAAAA"
		childB = "01CHILDBBBBBBBBBBBBBBBBBBB"
		// What childA mints when its log is recreated.
		childARestarted = "01CHILDAZZZZZZZZZZZZZZZZZZ"
	)

	parent := NewCursor(
		NewGeneration(childA, []Position{{Origin: printerA, Offset: 100}}),
		NewGeneration(childB, []Position{{Origin: printerB, Offset: 200}, {Origin: s3Main, Offset: 5}}),
	)

	equal(t,
		"01CHILDAAAAAAAAAAAAAAAAAAA:device/01JB8XN2K7QW9ZR4MV3T6H0PDA=100;"+
			"01CHILDBBBBBBBBBBBBBBBBBBB:device/01JB8XN2K7QW9ZR4MV3T6H0PDB=200,storage/s3-main=5",
		parent.String(), "one epoch per node, not one epoch for the cursor")

	round, err := ParseCursor(parent.String())
	noError(t, err)
	equal(t, parent, round)

	// childA restarts. Its generation is gone.
	_, held := parent.Generation(childARestarted)
	isFalse(t, held, "childA's offsets belong to a log that no longer exists")

	// childB never restarted, and still holds every place it held.
	survived, held := parent.Generation(childB)
	isTrue(t, held, "childA restarting invalidated childB's positions")

	offset, named := survived.Offset(printerB)
	isTrue(t, named)
	equal(t, uint64(200), offset)

	offset, named = survived.Offset(s3Main)
	isTrue(t, named)
	equal(t, uint64(5), offset)
}

// TestFleetCursorStaysLinear is the size budget. A cursor renders one line per
// position and nothing per pair, so a fleet costs a constant times its size.
func TestFleetCursorStaysLinear(t *testing.T) {
	t.Parallel()

	render := func(printers int) int {
		positions := make([]Position, 0, printers)
		for i := range printers {
			positions = append(positions, Position{
				Origin: mustOrigin(KindDevice, fmt.Sprintf("01JB8XN2K7QW9ZR4MV3T6H0%03d", i)),
				Offset: 914233 + uint64(i),
			})
		}

		return len(NewCursor(NewGeneration("01K2A0S3P4ZZZZZZZZZZZZZZZZ", positions)).String())
	}

	small, large := render(100), render(400)
	t.Logf("cursor bytes: 100 printers %d, 400 printers %d", small, large)

	// Four times the fleet, no more than four times the bytes plus the one
	// epoch every rendering carries.
	isTrue(t, large <= 4*small, "%d printers render %d bytes and %d render %d, which is worse than linear",
		100, small, 400, large)
}

func TestParseCursorRejectsMalformedText(t *testing.T) {
	t.Parallel()

	malformed := map[string]string{
		"empty":                    "",
		"no epoch":                 ":12",
		"no offset separator":      "epoch:device/a",
		"offset is not a number":   "epoch:device/a=not-a-number",
		"no origin":                "epoch:=12",
		"origin has no kind":       "epoch:a=12",
		"trailing separator":       "epoch:",
		"epoch named twice":        "epoch:device/a=1;epoch:device/b=2",
		"origin named twice":       "epoch:device/a=1,device/a=2",
		"generation with no epoch": "epoch:device/a=1;",
	}

	for name, text := range malformed {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseCursor(text)
			errorIs(t, err, ErrInvalidCursor, "cursor %q", text)
		})
	}
}

func TestEventIDRoundTrip(t *testing.T) {
	t.Parallel()

	position := Position{Origin: printerA, Offset: 914233}

	id := position.ID(epoch)
	equal(t, "01K2A0S3P4:device/01JB8XN2K7QW9ZR4MV3T6H0PDA:914233", id)

	resumed, err := ParseID(id)
	noError(t, err)
	equal(t, NewCursor(NewGeneration(epoch, []Position{position})), resumed,
		"the id of the last event received is the cursor that resumes after it")

	// A resource that is not a printer resumes the same way, which is what
	// makes an instance state transition resumable at all (R5).
	instance := Position{Origin: s3Main, Offset: 4}
	equal(t, "01K2A0S3P4:storage/s3-main:4", instance.ID(epoch))

	empty(t, Position{Origin: printerA}.ID(epoch),
		"a position with no offset holds no place, which is what telemetry is")
	empty(t, position.ID(""), "an offset without an epoch names nothing")
	empty(t, Position{Offset: 1}.ID(epoch), "an offset without an origin names nothing")
}

func TestParseIDRejectsMalformedText(t *testing.T) {
	t.Parallel()

	for _, malformed := range []string{
		"", "epoch", "epoch:device/a", ":device/a:1", "epoch::1", "epoch:device/a:x", "epoch:a:1",
	} {
		_, err := ParseID(malformed)
		errorIs(t, err, ErrInvalidCursor, "id %q", malformed)
	}
}

func TestCursorSurvivesJSON(t *testing.T) {
	t.Parallel()

	cursor := NewCursor(NewGeneration(epoch, []Position{{Origin: s3Main, Offset: 9}}))

	encoded, err := json.Marshal(cursor)
	noError(t, err)
	jsonEq(t,
		`{"generations":[{"epoch":"01K2A0S3P4","positions":[{"origin":"storage:s3-main","offset":9}]}]}`,
		string(encoded))

	var decoded Cursor
	noError(t, json.Unmarshal(encoded, &decoded))
	equal(t, cursor, decoded)
}

// TestAppendTextRendersWhatStringDoes keeps the two forms of one rendering from
// drifting. The append form exists so a transport can reuse one buffer; a
// transport that wrote a different cursor than String reports would hand a
// reader a place it cannot resume from.
func TestAppendTextRendersWhatStringDoes(t *testing.T) {
	t.Parallel()

	cursor := NewCursor(
		NewGeneration("01EPOCHB", []Position{{Origin: mustOrigin("printer", "b"), Offset: 2}}),
		NewGeneration("01EPOCHA", []Position{
			{Origin: mustOrigin("printer", "a"), Offset: 7},
			{Origin: mustOrigin("camera", "front"), Offset: 1},
		}),
	)

	appended, err := cursor.AppendText([]byte("id: "))
	noError(t, err)
	equal(t, "id: "+cursor.String(), string(appended))

	round, err := ParseCursor(cursor.String())
	noError(t, err)
	equal(t, cursor, round)
}

// TestPlacesCountsEveryStreamTheCursorNames is what an SSE transport branches on:
// a cursor naming one place is already what that event's own id says, and one
// naming more is not.
func TestPlacesCountsEveryStreamTheCursorNames(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cursor Cursor
		want   int
	}{
		"the zero value": {want: 0},
		"one place": {want: 1, cursor: NewCursor(NewGeneration("01EPOCHA",
			[]Position{{Origin: mustOrigin("printer", "a"), Offset: 1}}))},
		"two in one generation": {want: 2, cursor: NewCursor(NewGeneration("01EPOCHA", []Position{
			{Origin: mustOrigin("printer", "a"), Offset: 1},
			{Origin: mustOrigin("printer", "b"), Offset: 4},
		}))},
		"one in each of two generations": {want: 2, cursor: NewCursor(
			NewGeneration("01EPOCHA", []Position{{Origin: mustOrigin("printer", "a"), Offset: 1}}),
			NewGeneration("01EPOCHB", []Position{{Origin: mustOrigin("printer", "b"), Offset: 4}}),
		)},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			equal(t, test.want, test.cursor.Places())
		})
	}
}
