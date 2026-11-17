package xbuddy

import "slices"

// BuildVolumeModels is the models this driver reports a volume for, sorted,
// for the test that holds the table to the profiled models.
func BuildVolumeModels() []string {
	out := make([]string, 0, len(buildVolumes))
	for model := range buildVolumes {
		out = append(out, model)
	}

	slices.Sort(out)

	return out
}
