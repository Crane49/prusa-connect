package app

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// TestOnlyAManagerLabelsADevice holds the labels route to the permission the
// tags route takes, in both directions.
func TestOnlyAManagerLabelsADevice(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-LABELS")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, operator)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	_, err := plane.SetLabels(t.Context(), operator, id, map[string]string{"site": "brno"})
	requireClassRefusal(t, err, true)

	_, err = plane.SetLabels(t.Context(), "user:nobody", id, map[string]string{"site": "brno"})
	requireClassRefusal(t, err, false)

	held, err := plane.SetLabels(t.Context(), manager, id, map[string]string{"site": "brno"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"site": "brno"}, held)
}

// TestALabelIsWrittenAsATagAndReadBack is the mapping both ways. A label with
// a value is the tag "key: value", a label with no value is its key, and the
// labels view reads the same set back.
func TestALabelIsWrittenAsATagAndReadBack(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-LABEL-MAP")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	held, err := plane.SetLabels(t.Context(), manager, id,
		map[string]string{"site": "brno", "spare": ""})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"site": "brno", "spare": ""}, held)

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.Equal(t, []string{"site: brno", "spare"}, device.Tags)
	require.Equal(t, map[string]string{"site": "brno", "spare": ""}, device.Labels)

	// A tag written through the tags route reads as a label with no value.
	_, err = plane.SetTags(t.Context(), manager, id, []string{"night shift"})
	require.NoError(t, err)

	device, err = plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"night shift": ""}, device.Labels)
	require.NotNil(t, device.Labels)

	// A label set is held to the tag rules, because it is stored as tags.
	_, err = plane.SetLabels(t.Context(), manager, id, map[string]string{"a,b": ""})
	requireFaultCode(t, err, httpfault.CodeTagCharacter())
}

// TestTheLabelsFilterFindsTheTagsItSpells keeps the labels parameter working
// over tags. A key and a value find "key: value", and a bare key finds the key
// alone or any "key: " tag.
func TestTheLabelsFilterFindsTheTagsItSpells(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	brno := arrive(t, plane, "SIM-BRNO")
	prague := arrive(t, plane, "SIM-PRAGUE")
	spare := arrive(t, plane, "SIM-SPARE")

	for _, id := range []string{brno, prague, spare} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	}

	_, err := plane.SetTags(t.Context(), manager, brno, []string{"site: brno", "line: prototyping"})
	require.NoError(t, err)

	_, err = plane.SetTags(t.Context(), manager, prague, []string{"site: prague"})
	require.NoError(t, err)

	_, err = plane.SetTags(t.Context(), manager, spare, []string{"site"})
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		selector []string
		want     []string
	}{
		"a key and a value":           {selector: []string{"site=brno"}, want: []string{brno}},
		"a bare key takes any value":  {selector: []string{"site"}, want: []string{brno, prague, spare}},
		"two terms are both required": {selector: []string{"site=brno", "line=prototyping"}, want: []string{brno}},
		"a value nothing carries":     {selector: []string{"site=ostrava"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page, listErr := plane.List(t.Context(), manager, httpapi.DeviceQuery{Labels: tc.selector})
			require.NoError(t, listErr)

			found := make([]string, 0, len(page.Items))
			for _, item := range page.Items {
				found = append(found, item.ID)
			}

			require.ElementsMatch(t, tc.want, found)
			require.Equal(t, len(tc.want), page.Total)
		})
	}
}

// TestAMalformedSelectorIsRefusedRatherThanIgnored keeps a typo from reading as
// an empty fleet. The labels filter still takes only what a label could spell.
func TestAMalformedSelectorIsRefusedRatherThanIgnored(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	id := arrive(t, plane, "SIM-SELECTOR")
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	for name, clause := range map[string]string{
		"a key with a space and capitals": "THE SITE=brno",
		"an empty key":                    "=brno",
		"a value with a space":            "site=north hall",
		"two prefix separators":           "a/b/c",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := plane.List(t.Context(), manager, httpapi.DeviceQuery{Labels: []string{clause}})
			requireFaultCode(t, err, httpfault.CodeRequestInvalid())
		})
	}
}

// TestTheLabelsRouteWritesTags drives the published route, so a client that
// still sends labels is proved to store tags.
func TestTheLabelsRouteWritesTags(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialIn(t, running, "SIM-LABEL-ROUTE")

	id := printers(t, running)[0].ID

	written := running.call(t, http.MethodPut, "/api/v1/devices/"+id+"/labels",
		`{"labels":{"site":"brno"}}`)
	require.Equal(t, http.StatusOK, written.status, written.body)
	require.JSONEq(t, `{"labels":{"site":"brno"}}`, written.body)

	require.Equal(t, []string{"site: brno"}, printers(t, running)[0].Tags)
}

// TestTheLaterTagWinsWhenTwoShareAKey pins what the labels route documents. A
// label set holds one value per key, so the later tag in the list is the one
// a labels reader sees.
func TestTheLaterTagWinsWhenTwoShareAKey(t *testing.T) {
	t.Parallel()

	require.Equal(t, map[string]string{"site": "prague"}, labelsOfTags([]string{"site: brno", "site: prague"}))
	require.Equal(t, map[string]string{"site": ""}, labelsOfTags([]string{"site: brno", "site"}))
	require.Equal(t, map[string]string{"site": "brno"}, labelsOfTags([]string{"site", "site: brno"}))
}

// TestABareLabelKeyUsesTagSameness holds the bare key clause to the sameness
// the key=value clause uses. A joiner inside the key does not hide the tag.
func TestABareLabelKeyUsesTagSameness(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		tags []string
		want bool
	}{
		"the key alone":               {tags: []string{"site"}, want: true},
		"the key in other case":       {tags: []string{"SITE: brno"}, want: true},
		"the key alone with a joiner": {tags: []string{"si\u200dte"}, want: true},
		"the key and value, a joiner": {tags: []string{"si\u200dte: brno"}, want: true},
		"another key":                 {tags: []string{"sites: brno"}},
		"the key inside another tag":  {tags: []string{"website"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			clause := labelClause{key: "site"}
			require.Equal(t, tc.want, clause.matchesOne(tc.tags))
		})
	}
}
