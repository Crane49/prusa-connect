package app

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// TestOnlyAManagerTagsADevice holds the permission in both directions. A tag
// is an administrative fact, so an operator who runs prints is refused, and a
// caller holding nothing is told the device is not there.
func TestOnlyAManagerTagsADevice(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-TAGS")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, operator)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	_, err := plane.SetTags(t.Context(), operator, id, []string{"workshop"})
	requireClassRefusal(t, err, true)

	_, err = plane.SetTags(t.Context(), "user:nobody", id, []string{"workshop"})
	requireClassRefusal(t, err, false)

	held, err := plane.SetTags(t.Context(), manager, id, []string{"workshop"})
	require.NoError(t, err)
	require.Equal(t, []string{"workshop"}, held)

	// Reading a tag takes what reading the device takes, so the operator who
	// could not write it reads it.
	seen, err := plane.Get(t.Context(), operator, id)
	require.NoError(t, err)
	require.Equal(t, []string{"workshop"}, seen.Tags)
}

// TestAWriteReplacesTheWholeTagList is what makes a tag removable, and it
// keeps the order the person gave.
func TestAWriteReplacesTheWholeTagList(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-TAG-REPLACE")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	_, err := plane.SetTags(t.Context(), manager, id, []string{"workshop", "night shift"})
	require.NoError(t, err)

	held, err := plane.SetTags(t.Context(), manager, id, []string{"PETG only", "workshop"})
	require.NoError(t, err)
	require.Equal(t, []string{"PETG only", "workshop"}, held,
		"the write merged, or it did not keep the order it was given")

	empty, err := plane.SetTags(t.Context(), manager, id, nil)
	require.NoError(t, err)
	require.NotNil(t, empty, "an empty list came back as null")
	require.Empty(t, empty)

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.NotNil(t, device.Tags, "a device carrying no tag answered null rather than an empty list")
	require.Empty(t, device.Tags)
}

// TestATagIsCleanedAndKeepsItsCase covers the rules that change a tag rather
// than refuse it.
func TestATagIsCleanedAndKeepsItsCase(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"spaces at the ends are trimmed":   {raw: "  workshop  ", want: "workshop"},
		"a run of spaces becomes one":      {raw: "night   shift", want: "night shift"},
		"a no-break space becomes a space": {raw: "night\u00a0shift", want: "night shift"},
		"the case is kept":                 {raw: "PETG Only", want: "PETG Only"},
		"a colon is a character like any":  {raw: "site: brno", want: "site: brno"},
		"a letter outside English":         {raw: "noční směna", want: "noční směna"},
		"a script outside Latin":           {raw: "工作室", want: "工作室"},
		"a combining accent is kept":       {raw: "Cafe\u0301", want: "Cafe\u0301"},
		"a red heart emoji":                {raw: "\u2764\ufe0f", want: "\u2764\ufe0f"},
		"a family emoji":                   {raw: "\U0001F468\u200d\U0001F469\u200d\U0001F467", want: "\U0001F468\u200d\U0001F469\u200d\U0001F467"},
		"a Persian word with a non-joiner": {raw: "\u0645\u06cc\u200c\u062e\u0648\u0627\u0647\u0645", want: "\u0645\u06cc\u200c\u062e\u0648\u0627\u0647\u0645"},
		"a text presentation selector":     {raw: "\u2764\ufe0e shop", want: "\u2764\ufe0e shop"},
		"a heart on fire":                  {raw: "\u2764\ufe0f\u200d\U0001F525", want: "\u2764\ufe0f\u200d\U0001F525"},
		"a keycap emoji":                   {raw: "1\ufe0f\u20e3 shelf", want: "1\ufe0f\u20e3 shelf"},
		"an accent and a joiner":           {raw: "e\u0301\u200dx", want: "e\u0301\u200dx"},
		"the England flag":                 {raw: "\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F", want: "\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F"},
		"a Hangul syllable is kept":        {raw: "작업실", want: "작업실"},
		"sixty-four characters":            {raw: strings.Repeat("ž", maxTagRunes), want: strings.Repeat("ž", maxTagRunes)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := cleanTag("body.tags[0]", tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestATagIsHeldToItsRules names the code for every rule a tag can break, so
// a screen can show the right message for each one.
func TestATagIsHeldToItsRules(t *testing.T) {
	t.Parallel()

	tooMany := make([]string, maxTagsPerDevice+1)
	for index := range tooMany {
		tooMany[index] = "tag " + strings.Repeat("x", index+1)
	}

	for name, tc := range map[string]struct {
		tags []string
		want fault.Code
	}{
		"an empty tag":    {tags: []string{""}, want: httpfault.CodeTagEmpty()},
		"a tag of spaces": {tags: []string{"   "}, want: httpfault.CodeTagEmpty()},
		"longer than any stored tag": {
			tags: []string{strings.Repeat("a", maxStoredTagRunes+1)}, want: httpfault.CodeTagTooLong(),
		},
		"a comma":                   {tags: []string{"PETG, PLA"}, want: httpfault.CodeTagCharacter()},
		"a line break":              {tags: []string{"night\nshift"}, want: httpfault.CodeTagCharacter()},
		"a tab":                     {tags: []string{"night\tshift"}, want: httpfault.CodeTagCharacter()},
		"a right-to-left override":  {tags: []string{"shop\u202eabc"}, want: httpfault.CodeTagCharacter()},
		"a zero-width space":        {tags: []string{"work\u200bshop"}, want: httpfault.CodeTagCharacter()},
		"bytes that are not UTF-8":  {tags: []string{"work\xffshop"}, want: httpfault.CodeTagCharacter()},
		"a variation selector":      {tags: []string{"heart\ufe00"}, want: httpfault.CodeTagCharacter()},
		"a joiner at the start":     {tags: []string{"\u200dworkshop"}, want: httpfault.CodeTagCharacter()},
		"a non-joiner at the start": {tags: []string{"\u200cworkshop"}, want: httpfault.CodeTagCharacter()},
		"a presentation selector at the start": {
			tags: []string{"\ufe0fworkshop"}, want: httpfault.CodeTagCharacter(),
		},
		"a joiner after a space":    {tags: []string{"night \u200dshift"}, want: httpfault.CodeTagCharacter()},
		"a run of joiners":          {tags: []string{"a" + strings.Repeat("\u200d", 63)}, want: httpfault.CodeTagCharacter()},
		"two joiners in a row":      {tags: []string{"a\u200d\u200cb"}, want: httpfault.CodeTagCharacter()},
		"a joiner at the end":       {tags: []string{"shop\u200d"}, want: httpfault.CodeTagCharacter()},
		"a joiner before a space":   {tags: []string{"night\u200d shift"}, want: httpfault.CodeTagCharacter()},
		"two selectors in a row":    {tags: []string{"shop\ufe0f\ufe0f"}, want: httpfault.CodeTagCharacter()},
		"a selector after a joiner": {tags: []string{"shop\u200d\ufe0f"}, want: httpfault.CodeTagCharacter()},
		"a selector and a joiner at the end": {
			tags: []string{"shop\ufe0f\u200d"}, want: httpfault.CodeTagCharacter(),
		},
		"flag letters without a flag": {tags: []string{"a\U000E0067\U000E0062\U000E007F"}, want: httpfault.CodeTagCharacter()},
		"flag letters left open":      {tags: []string{"\U0001F3F4\U000E0067\U000E0062"}, want: httpfault.CodeTagCharacter()},
		"a flag end with no letters":  {tags: []string{"\U0001F3F4\U000E007F"}, want: httpfault.CodeTagCharacter()},
		"a mark at the start":         {tags: []string{"\u0301shop"}, want: httpfault.CodeTagCharacter()},
		"a mark after a space":        {tags: []string{"night \u0301shift"}, want: httpfault.CodeTagCharacter()},
		"a mark after a joiner":       {tags: []string{"a\u200c\u0301b"}, want: httpfault.CodeTagCharacter()},
		"a mark, a joiner, a word":    {tags: []string{"\u0301\u200d x"}, want: httpfault.CodeTagCharacter()},
		"a tag and the same tag with an emoji selector": {
			tags: []string{"PETG", "PETG\ufe0f"}, want: httpfault.CodeTagDuplicate(),
		},
		"a tag and the same tag with a joiner": {
			tags: []string{"PETG", "pe\u200dtg"}, want: httpfault.CodeTagDuplicate(),
		},
		"a supplementary variation selector": {
			tags: []string{"work\U000E0100shop"}, want: httpfault.CodeTagCharacter(),
		},
		"a combining grapheme joiner":    {tags: []string{"work\u034fshop"}, want: httpfault.CodeTagCharacter()},
		"a Hangul choseong filler":       {tags: []string{"\u115f"}, want: httpfault.CodeTagCharacter()},
		"a Hangul jungseong filler":      {tags: []string{"\u1160"}, want: httpfault.CodeTagCharacter()},
		"a Hangul filler":                {tags: []string{"\u3164\u3164"}, want: httpfault.CodeTagCharacter()},
		"a half-width Hangul filler":     {tags: []string{"\uffa0"}, want: httpfault.CodeTagCharacter()},
		"a blank braille pattern":        {tags: []string{"\u2800"}, want: httpfault.CodeTagCharacter()},
		"a Mongolian variation selector": {tags: []string{"work\u180bshop"}, want: httpfault.CodeTagCharacter()},
		"the fourth Mongolian selector":  {tags: []string{"work\u180fshop"}, want: httpfault.CodeTagCharacter()},
		"a Khmer inherent vowel":         {tags: []string{"work\u17b4shop"}, want: httpfault.CodeTagCharacter()},
		"three look-alike chips": {
			tags: []string{"workshop", "work\u180cshop", "work\u17b5shop"}, want: httpfault.CodeTagCharacter(),
		},
		"only combining marks":         {tags: []string{"\u0301\u0308"}, want: httpfault.CodeTagEmpty()},
		"the same tag twice":           {tags: []string{"workshop", "workshop"}, want: httpfault.CodeTagDuplicate()},
		"two tags that differ in case": {tags: []string{"PETG only", "petg ONLY"}, want: httpfault.CodeTagDuplicate()},
		"two tags that differ in space": {
			tags: []string{"night shift", " night  shift"}, want: httpfault.CodeTagDuplicate(),
		},
		"thirty-three tags": {tags: tooMany, want: httpfault.CodeTagsTooMany()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := normaliseTags("body.tags", tc.tags)
			requireFaultCode(t, err, tc.want)
		})
	}

	// The upper limit on the count is reachable, so the rule above is not
	// refusing every long list.
	held, err := normaliseTags("body.tags", tooMany[:maxTagsPerDevice])
	require.NoError(t, err)
	require.Len(t, held, maxTagsPerDevice)
}

// TestARefusedTagIsRepeatedWithoutItsRefusedCharacters keeps a line break out
// of the error a log line or a screen shows. The refused characters become the
// replacement character.
func TestARefusedTagIsRepeatedWithoutItsRefusedCharacters(t *testing.T) {
	t.Parallel()

	_, err := cleanTag("body.tags[0]", "night\nshift\u202e")

	var refused *fault.Fault

	require.ErrorAs(t, err, &refused)

	shown, found := refused.Params().Get("tag")
	require.True(t, found, "the error does not name the tag it refused")
	require.Equal(t, "night\uFFFDshift\uFFFD", shown.String())
}

// TestABrokenTagIsRefusedBeforeTheWrite holds the rules on the write itself,
// so a refused list leaves the device carrying what it carried.
func TestABrokenTagIsRefusedBeforeTheWrite(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-TAG-REFUSE")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	_, err := plane.SetTags(t.Context(), manager, id, []string{"workshop"})
	require.NoError(t, err)

	_, err = plane.SetTags(t.Context(), manager, id, []string{"night shift", "a,b"})
	requireFaultCode(t, err, httpfault.CodeTagCharacter())

	_, err = plane.SetTags(t.Context(), manager, id, []string{"night shift", strings.Repeat("a", maxTagRunes+1)})
	requireFaultCode(t, err, httpfault.CodeTagTooLong())

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.Equal(t, []string{"workshop"}, device.Tags, "a refused write changed the device")
}

// TestTheListingNarrowsToTheTagsTheCallerNamed is the filter. It narrows the
// page and the total, ignores case, and needs every named tag.
func TestTheListingNarrowsToTheTagsTheCallerNamed(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	workshop := arrive(t, plane, "SIM-WORKSHOP")
	night := arrive(t, plane, "SIM-NIGHT")
	plain := arrive(t, plane, "SIM-UNTAGGED")

	for _, id := range []string{workshop, night, plain} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	}

	_, err := plane.SetTags(t.Context(), manager, workshop, []string{"workshop", "PETG only"})
	require.NoError(t, err)

	_, err = plane.SetTags(t.Context(), manager, night, []string{"workshop", "night shift"})
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		filter []string
		want   []string
	}{
		"no filter takes every device": {want: []string{workshop, night, plain}},
		"one tag":                      {filter: []string{"workshop"}, want: []string{workshop, night}},
		"the case is ignored":          {filter: []string{"petg ONLY"}, want: []string{workshop}},
		"spaces are cleaned first":     {filter: []string{" night   shift "}, want: []string{night}},
		"two tags are both required":   {filter: []string{"workshop", "night shift"}, want: []string{night}},
		"two tags no device carries":   {filter: []string{"PETG only", "night shift"}},
		"a tag nothing carries":        {filter: []string{"garage"}},
		"part of a tag does not match": {filter: []string{"PETG"}},
		"an empty value is dropped":    {filter: []string{"", "workshop"}, want: []string{workshop, night}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page, listErr := plane.List(t.Context(), manager, httpapi.DeviceQuery{Tags: tc.filter})
			require.NoError(t, listErr)

			require.Len(t, page.Items, len(tc.want))
			require.Equal(t, len(tc.want), page.Total, "the total counts rows the filter took out")

			found := make([]string, 0, len(page.Items))
			for _, item := range page.Items {
				found = append(found, item.ID)
			}

			require.ElementsMatch(t, tc.want, found)
		})
	}
}

// TestABrokenTagFilterIsRefused keeps a filter that could never match from
// reading as an empty fleet.
func TestABrokenTagFilterIsRefused(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	id := arrive(t, plane, "SIM-TAG-FILTER")
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	_, err := plane.List(t.Context(), manager,
		httpapi.DeviceQuery{Tags: []string{strings.Repeat("a", maxStoredTagRunes+1)}})
	requireFaultCode(t, err, httpfault.CodeTagTooLong())

	_, err = plane.List(t.Context(), manager, httpapi.DeviceQuery{Tags: []string{"night\nshift"}})
	requireFaultCode(t, err, httpfault.CodeTagCharacter())
}

// TestATagSurvivesARestart drives the published API across a real stop and
// start. A tag is something a person typed, so losing it loses their work.
func TestATagSurvivesARestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first := start(t, dir, "core:\n  log:\n    level: error\n")

	dialIn(t, first, "SIM-TAG-RESTART")

	before := printers(t, first)
	require.Len(t, before, 1)
	require.Empty(t, before[0].Tags, "a machine nobody has tagged arrived carrying tags")

	id := before[0].ID

	written := first.call(t, http.MethodPut, "/api/v1/devices/"+id+"/tags",
		`{"tags":["workshop"," PETG  only "]}`)
	require.Equal(t, http.StatusOK, written.status, written.body)
	require.JSONEq(t, `{"tags":["workshop","PETG only"]}`, written.body)

	refused := first.call(t, http.MethodPut, "/api/v1/devices/"+id+"/tags",
		`{"tags":["workshop","WORKSHOP"]}`)
	require.Equal(t, http.StatusUnprocessableEntity, refused.status, refused.body)
	require.Contains(t, refused.body, "TAG_DUPLICATE")

	narrowed := first.call(t, http.MethodGet, "/api/v1/devices?tag="+url.QueryEscape("petg only"), "")
	require.Equal(t, http.StatusOK, narrowed.status, narrowed.body)
	require.Contains(t, narrowed.body, id)

	missed := first.call(t, http.MethodGet, "/api/v1/devices?tag=garage", "")
	require.Equal(t, http.StatusOK, missed.status, missed.body)
	require.NotContains(t, missed.body, id, "the filter answered with a device carrying other tags")

	first.stopAndWait(t)

	second := start(t, dir, "core:\n  log:\n    level: error\n")

	after := printers(t, second)
	require.Len(t, after, 1, "the fleet was forgotten by a restart")
	require.Equal(t, []string{"workshop", "PETG only"}, after[0].Tags,
		"the restart lost the tags somebody typed")
}

// longestStoredTag is the longest tag the upgrade from labels can produce. It
// is a key of the longest prefix and name, then ": ", then the longest value.
func longestStoredTag() string {
	prefix := strings.Join([]string{
		strings.Repeat("a", 63), strings.Repeat("b", 63), strings.Repeat("c", 63), strings.Repeat("d", 61),
	}, ".")

	return prefix + "/" + strings.Repeat("n", 63) + ": " + strings.Repeat("v", 63)
}

// storeTags puts a list on a device the way the upgrade from labels does, with
// no rule applied to it.
func storeTags(t *testing.T, plane *devices, id string, tags ...string) {
	t.Helper()

	entry, seen := plane.known(id)
	require.True(t, seen, "the device is not in the plane")

	entry.mu.Lock()
	entry.tags = tags
	entry.mu.Unlock()
}

// TestATagTheUpgradeStoredIsKeptOnAWrite covers a tag longer than the rules
// allow that the upgrade from labels stored. A screen sends the whole list
// back, so the device must still take a new tag beside the old one.
func TestATagTheUpgradeStoredIsKeptOnAWrite(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-TAG-UPGRADED")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	window := "fleet.workshop.brno.example.com/maintenance-window: weekday-evenings"
	longest := longestStoredTag()
	require.Len(t, longest, maxStoredTagRunes, "the test tag is not the longest the upgrade makes")

	storeTags(t, plane, id, window, longest)

	held, err := plane.SetTags(t.Context(), manager, id, []string{window, longest, "night shift"})
	require.NoError(t, err, "a tag the device already carries was refused")
	require.Equal(t, []string{window, longest, "night shift"}, held)

	// The labels route reads the list and writes it back, and that works too.
	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)

	labels := device.Labels
	labels["spare"] = ""

	_, err = plane.SetLabels(t.Context(), manager, id, labels)
	require.NoError(t, err, "a labels write that keeps a stored tag was refused")

	// The filter finds a device by the long tag.
	page, err := plane.List(t.Context(), manager, httpapi.DeviceQuery{Tags: []string{longest}})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)

	_, err = plane.List(t.Context(), manager, httpapi.DeviceQuery{Tags: []string{longest + "x"}})
	requireFaultCode(t, err, httpfault.CodeTagTooLong())

	// The error names the limit the filter holds, and not the one for a new tag.
	var refused *fault.Fault

	require.ErrorAs(t, err, &refused)

	limit, found := refused.Params().Get("limit_characters")
	require.True(t, found, "the error does not name the limit")
	require.Equal(t, "382", limit.String())

	// A long tag the device does not carry is still new, so the rules apply.
	_, err = plane.SetTags(t.Context(), manager, id, []string{window + " again"})
	requireFaultCode(t, err, httpfault.CodeTagTooLong())

	// The stored tag is kept only with exactly the stored text. A spelling that
	// is the same tag but reads differently is new, so the rules apply.
	for name, spelling := range map[string]string{
		"upper case":         strings.ToUpper(window),
		"a long s":           strings.Replace(window, "s", "\u017f", 1),
		"a Kelvin sign":      strings.Replace(window, "k", "\u212a", 1),
		"a joiner inside it": strings.Replace(window, "workshop", "work\u200dshop", 1),
	} {
		_, err = plane.SetTags(t.Context(), manager, id, []string{spelling})
		require.Errorf(t, err, "%s of the stored tag was kept", name)
		requireFaultCode(t, err, httpfault.CodeTagTooLong())
	}

	// A caller who may not see the device learns nothing from a long tag.
	_, err = plane.SetTags(t.Context(), "user:nobody", id, []string{window + " again"})
	requireClassRefusal(t, err, false)
}

// TestATagFilterDoesNotRevealAHiddenDevice holds the filter to what the caller
// may read. A device the caller cannot see is left out of the page and of the
// total, even when it carries the tag.
func TestATagFilterDoesNotRevealAHiddenDevice(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	visible := arrive(t, plane, "SIM-TAG-VISIBLE")
	hidden := arrive(t, plane, "SIM-TAG-HIDDEN")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, visible, authz.RelOperator, operator)))

	for _, id := range []string{visible, hidden} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	}

	_, err := plane.SetTags(t.Context(), manager, visible, []string{"workshop"})
	require.NoError(t, err)

	_, err = plane.SetTags(t.Context(), manager, hidden, []string{"workshop", "secret room"})
	require.NoError(t, err)

	for name, filter := range map[string][]string{
		"a tag both devices carry":      {"workshop"},
		"a tag only the hidden carries": {"secret room"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page, listErr := plane.List(t.Context(), operator, httpapi.DeviceQuery{Tags: filter})
			require.NoError(t, listErr)

			for _, item := range page.Items {
				require.NotEqual(t, hidden, item.ID, "the filter answered with a device the caller cannot read")
			}

			if filter[0] == "workshop" {
				require.Len(t, page.Items, 1)
				require.Equal(t, 1, page.Total, "the total counts a device the caller cannot read")

				return
			}

			require.Empty(t, page.Items)
			require.Zero(t, page.Total, "the total counts a device the caller cannot read")
		})
	}
}

// TestATagWithAPresentationSelectorIsTheSameTag keeps look-alike tags from
// splitting a filter. A tag with an emoji selector or a joiner matches the
// same tag without one, both ways.
func TestATagWithAPresentationSelectorIsTheSameTag(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	plain := arrive(t, plane, "SIM-TAG-PLAIN")
	selected := arrive(t, plane, "SIM-TAG-SELECTED")

	for _, id := range []string{plain, selected} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	}

	_, err := plane.SetTags(t.Context(), manager, plain, []string{"PETG"})
	require.NoError(t, err)

	held, err := plane.SetTags(t.Context(), manager, selected, []string{"PETG\ufe0f"})
	require.NoError(t, err)
	require.Equal(t, []string{"PETG\ufe0f"}, held, "the tag was not stored as it was typed")

	for name, filter := range map[string]string{
		"the plain spelling":         "PETG",
		"the spelling with selector": "petg\ufe0f",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page, listErr := plane.List(t.Context(), manager, httpapi.DeviceQuery{Tags: []string{filter}})
			require.NoError(t, listErr)

			found := make([]string, 0, len(page.Items))
			for _, item := range page.Items {
				found = append(found, item.ID)
			}

			require.ElementsMatch(t, []string{plain, selected}, found)
		})
	}
}

// TestALongStoredTagIsKeptOnlyOnItsOwnDevice holds the stored-tag exception to
// one device. A long tag on one printer does not let another printer take it.
func TestALongStoredTagIsKeptOnlyOnItsOwnDevice(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	holder := arrive(t, plane, "SIM-TAG-HOLDER")
	other := arrive(t, plane, "SIM-TAG-OTHER")

	for _, id := range []string{holder, other} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	}

	long := "fleet.workshop.brno.example.com/maintenance-window: weekday-evening-s"
	require.Len(t, long, 69)

	storeTags(t, plane, holder, long)

	_, err := plane.SetTags(t.Context(), manager, holder, []string{long, "workshop"})
	require.NoError(t, err, "the device that carries the tag could not keep it")

	_, err = plane.SetTags(t.Context(), manager, other, []string{long})
	requireFaultCode(t, err, httpfault.CodeTagTooLong())
}

// TestTwoRegionFlagsAreTwoTags keeps the letters of a region flag in the
// comparison, so England and Scotland are two different tags.
func TestTwoRegionFlagsAreTwoTags(t *testing.T) {
	t.Parallel()

	england := "\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F"
	scotland := "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"

	held, err := normaliseTags("body.tags", []string{england, scotland})
	require.NoError(t, err)
	require.Len(t, held, 2)
}

// TestAnEmptyTagSaysItHasNoVisibleCharacter keeps the message in a log line in
// step with the error's title.
func TestAnEmptyTagSaysItHasNoVisibleCharacter(t *testing.T) {
	t.Parallel()

	_, err := cleanTag("body.tags[0]", "   ")
	require.ErrorContains(t, err, "a tag has no visible character")
}
