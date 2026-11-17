package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printfile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// The objects a sliced file labels reach the HTTP answer and a module's stat
// with their ids, names and outlines, and a module's own reading of a file
// comes back as the same objects.
func TestTheLabelledObjectsOfAFileReachAModule(t *testing.T) {
	t.Parallel()

	found := printfile.Details{Objects: []printfile.LabelledObject{
		{ID: 0, Name: "Cube", Polygon: []printfile.Point{{X: 10, Y: 10}, {X: 20, Y: 10}, {X: 20, Y: 20}}},
		{ID: 1, Name: "Cube (Instance 2)", Polygon: []printfile.Point{{X: 40, Y: 10}, {X: 50, Y: 10}, {X: 50, Y: 20}}},
	}}

	var details httpapi.PrintDetails

	fillNeeds(&details, found)
	require.Equal(t, []httpapi.PrintObject{
		{ID: 0, Name: "Cube", Outline: []httpapi.PrintPoint{{X: 10, Y: 10}, {X: 20, Y: 10}, {X: 20, Y: 20}}},
		{
			ID:      1,
			Name:    "Cube (Instance 2)",
			Outline: []httpapi.PrintPoint{{X: 40, Y: 10}, {X: 50, Y: 10}, {X: 50, Y: 20}},
		},
	}, details.Objects)

	info := printInfoOf(&details)
	require.Len(t, info.Objects, 2)
	require.Equal(t, storageapi.PrintObject{
		ID:      1,
		Name:    "Cube (Instance 2)",
		Outline: []storageapi.PrintPoint{{X: 40, Y: 10}, {X: 50, Y: 10}, {X: 50, Y: 20}},
	}, info.Objects[1])

	require.Equal(t, found.Objects, detailsFromModule(*info).Objects,
		"a module's answer did not come back as the objects the core read")
}
