package profile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// benchModel is one declared row, complete, so a case below changes the
// spelling or the dialect alone. The driver's qualified name is the dialect's
// under a bench module, which is enough to tell two drivers apart.
func benchModel(model, connector string) ModelDoc {
	return ModelDoc{
		Model:       model,
		DisplayName: "Bench Works " + model,
		Vendor:      "Bench Works",
		Icon:        IconGeneric,
		Connector:   connector,
		DeclaredBy:  "example.com/bench/" + connector,
		ToolChanger: true,
	}
}

// TestTheZeroCatalogueServesTheCoresRows is a build whose drivers declare no
// model. It serves what ModelDocs returns and nothing else.
func TestTheZeroCatalogueServesTheCoresRows(t *testing.T) {
	t.Parallel()

	require.Equal(t, ModelDocs(), Catalogue{}.Docs())
}

// TestADeclaredModelIsServedBesideTheCoresRows is the point of placing: one
// list, in the order of the machine's spelling, so a page reads the two halves
// as one.
func TestADeclaredModelIsServedBesideTheCoresRows(t *testing.T) {
	t.Parallel()

	catalogue, err := Place(benchModel("Bench_One", "bench"))
	require.NoError(t, err)
	require.Empty(t, catalogue.Refused())

	docs := catalogue.Docs()
	require.Len(t, docs, len(ModelDocs())+1)
	require.Contains(t, docs, benchModel("Bench_One", "bench"))
	require.IsIncreasing(t, modelsOf(docs), "the served rows are not ordered by spelling")
}

// TestADeclaredModelTheProfilesAnswerIsRefused holds a driver to the machines
// the core does not already name. The profile lookup resolves a model name, a
// device type code and an alias, and a device reporting any of them is that
// profiled model to the host, so each is a collision.
//
// Only the declared row goes. The Prusa row stays as it was, and a row the same
// driver declared beside it is still served (ADR-20261016-qrp rule 5).
func TestADeclaredModelTheProfilesAnswerIsRefused(t *testing.T) {
	t.Parallel()

	for name, spelling := range map[string]string{
		"the model name, spelled another way": "core one",
		"a device type code":                  deviceTypeOf(t, "MK4S"),
		"a device type code without its dots": strings.ReplaceAll(deviceTypeOf(t, "MK4S"), ".", ""),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			profiled, ok := LookupModel(deviceTypeOrModel(spelling))
			require.True(t, ok)

			catalogue, err := Place(benchModel(spelling, "bench"), benchModel("Bench_One", "bench"))
			require.NoError(t, err, "a collision refuses the row, never the start")

			// Only a code matched with its dots left out is named: a spelling
			// the lookup resolves is that model to the host as written.
			code := ""
			if spelling != deviceTypeOrModel(spelling) {
				code = deviceTypeOrModel(spelling)
			}

			require.Equal(t, []Refusal{{
				Model: spelling, Connector: "bench", DeclaredBy: "example.com/bench/bench",
				Profile: profiled.Model, Code: code,
			}}, catalogue.Refused())

			docs := catalogue.Docs()
			require.NotContains(t, docs, benchModel(spelling, "bench"))
			require.Contains(t, docs, profiled, "the Prusa row changed")
			require.Contains(t, docs, benchModel("Bench_One", "bench"), "the driver's other row went too")
		})
	}
}

// TestTwoDialectsDeclaringOneMachineAreRefused is the collision only a build
// can make, because each driver passed its own validation. Neither row is
// served: keeping the first would let the order of init calls pick the driver
// a page shows. Each refusal names the other driver, so the operator reads
// both ends from either line.
func TestTwoDialectsDeclaringOneMachineAreRefused(t *testing.T) {
	t.Parallel()

	catalogue, err := Place(
		benchModel("Bench_One", "bench"),
		benchModel("bench-one", "other-bench"),
		benchModel("Shelf_One", "bench"),
	)
	require.NoError(t, err, "a collision refuses the rows, never the start")

	require.Equal(t, []Refusal{
		{
			Model: "Bench_One", Connector: "bench", DeclaredBy: "example.com/bench/bench",
			Drivers: []string{"example.com/bench/other-bench"},
		},
		{
			Model: "bench-one", Connector: "other-bench", DeclaredBy: "example.com/bench/other-bench",
			Drivers: []string{"example.com/bench/bench"},
		},
	}, catalogue.Refused())

	docs := catalogue.Docs()
	require.NotContains(t, modelsOf(docs), "Bench_One")
	require.NotContains(t, modelsOf(docs), "bench-one")
	require.Contains(t, docs, benchModel("Shelf_One", "bench"), "a row nobody else declared went too")
}

// TestTheSameRowsAreRefusedInEitherOrder holds the outcome to the build and
// not to the order its drivers registered in.
func TestTheSameRowsAreRefusedInEitherOrder(t *testing.T) {
	t.Parallel()

	one, err := Place(benchModel("Bench_One", "bench"), benchModel("bench-one", "other-bench"))
	require.NoError(t, err)

	other, err := Place(benchModel("bench-one", "other-bench"), benchModel("Bench_One", "bench"))
	require.NoError(t, err)

	require.Equal(t, ModelDocs(), one.Docs())
	require.Equal(t, ModelDocs(), other.Docs())
}

// TestARefusalNamesWhatItCollidedWith is the line an operator reads.
func TestARefusalNamesWhatItCollidedWith(t *testing.T) {
	t.Parallel()

	require.Equal(t, `the Prusa model "MK4S"`, Refusal{Profile: "MK4S"}.CollidesWith())
	require.Equal(t, `the device type code "1.4.1" of the Prusa model "MK4S"`,
		Refusal{Profile: "MK4S", Code: "1.4.1"}.CollidesWith())
	require.Equal(t, "the driver example.com/a", Refusal{Drivers: []string{"example.com/a"}}.CollidesWith())
	require.Equal(t, "the drivers example.com/a, example.com/b",
		Refusal{Drivers: []string{"example.com/a", "example.com/b"}}.CollidesWith())
}

// TestARefusalSaysWhatToDo is the remedy an operator reads beside a refusal.
// A Prusa machine is the core's, so the remedy never asks for a rename there.
// A spelling that only reads as a Prusa code is a coincidence, so it does.
func TestARefusalSaysWhatToDo(t *testing.T) {
	t.Parallel()

	require.Contains(t, Refusal{Profile: "MK4S"}.Remedy(), "drop the model")
	require.Contains(t, Refusal{Profile: "MK4S", Code: "1.4.1"}.Remedy(), "another spelling")
	require.Contains(t, Refusal{Drivers: []string{"example.com/a"}}.Remedy(), "all but one of the drivers")
}

// TestTheRefusalsCannotBeWrittenThrough holds the catalogue read-only.
func TestTheRefusalsCannotBeWrittenThrough(t *testing.T) {
	t.Parallel()

	catalogue, err := Place(benchModel("Bench_One", "bench"), benchModel("bench-one", "other-bench"))
	require.NoError(t, err)

	catalogue.Refused()[0].Drivers[0] = "example.com/somebody-else"
	require.Equal(t, "example.com/bench/other-bench", catalogue.Refused()[0].Drivers[0])
}

// deviceTypeOrModel turns a device type code with its dots left out back into
// the code, so the lookup below finds the profiled row it collides with.
func deviceTypeOrModel(spelling string) string {
	for _, doc := range ModelDocs() {
		if strings.ReplaceAll(doc.DeviceTypeID, ".", "") == spelling {
			return doc.DeviceTypeID
		}
	}

	return spelling
}

func modelsOf(docs []ModelDoc) []string {
	out := make([]string, 0, len(docs))
	for _, doc := range docs {
		out = append(out, doc.Model)
	}

	return out
}

// deviceTypeOf reads a code out of the built-in profiles, so the test does not
// copy a value the generated file owns.
func deviceTypeOf(t *testing.T, model string) string {
	t.Helper()

	for _, doc := range ModelDocs() {
		if doc.Model == model {
			require.NotEmpty(t, doc.DeviceTypeID, "%s carries no device type code", model)

			return doc.DeviceTypeID
		}
	}

	t.Fatalf("no row for %s", model)

	return ""
}
