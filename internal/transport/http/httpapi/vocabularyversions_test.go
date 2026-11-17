package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The two gates ADR-20260827-qvk rule 2 owes the served index.
//
// tools/vocabcompat holds the four artefacts under docs/vocabularies to their
// versions. It cannot see the three vocabularies that publish no version,
// because a vocabulary with no version writes no artefact there. These two
// cases are the other half: the index is where the empty string is served, so
// the index is where the debt is counted.
//
// The list can only shrink. Removing a name means giving that vocabulary a
// version; adding one means a reviewer reads a line in a committed baseline
// saying which published set promises a consumer nothing.

// vocabularyBaseline is the committed baseline, relative to this package.
const vocabularyBaseline = "../../../../tools/vocabcompat/baseline.json"

// vocabularyArtefacts is where a versioned vocabulary writes its set.
const vocabularyArtefacts = "../../../../docs/vocabularies"

func TestEveryVocabularyRowWithNoVersionIsNamedInTheBaseline(t *testing.T) {
	t.Parallel()

	rows := vocabularyIndex()
	if len(rows) == 0 {
		t.Fatal("the index serves no vocabulary, so this case would pass over nothing")
	}

	allowed := unversionedNames(t)
	if len(allowed) == 0 {
		t.Fatal("the baseline names no unversioned vocabulary, and three rows publish an empty version")
	}

	for _, row := range rows {
		if row.Version != "" && row.Format != "" {
			continue
		}

		if allowed[row.Name] {
			continue
		}

		t.Errorf("%s publishes version %q and format %q, and the baseline does not allow it.\n"+
			"ADR-20260825-bhe rule 3 gives every vocabulary a version and ADR-20260827-qvk rule 2 refuses one that\n"+
			"ships without. Declare the two constants, or add the name to \"unversioned\" in\n"+
			"tools/vocabcompat/baseline.json with the reason a reviewer needs.",
			row.Name, row.Version, row.Format)
	}
}

func TestEveryVersionedVocabularyRowHasABaselinedArtefact(t *testing.T) {
	t.Parallel()

	rows := vocabularyIndex()
	if len(rows) == 0 {
		t.Fatal("the index serves no vocabulary, so this case would pass over nothing")
	}

	baselined := baselinedNames(t)
	if len(baselined) == 0 {
		t.Fatal("the baseline holds no vocabulary, so this case would pass over nothing")
	}

	judged := 0

	for _, row := range rows {
		if row.Version == "" {
			continue
		}

		judged++

		if !baselined[row.Name] {
			t.Errorf("%s publishes version %s and tools/vocabcompat/baseline.json does not hold it, "+
				"so nothing refuses that version standing still while the set moves. Run `make vocab-accept`.",
				row.Name, row.Version)

			continue
		}

		path := filepath.Join(vocabularyArtefacts, row.Name+".json")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s publishes version %s and %s does not exist: %v",
				row.Name, row.Version, path, err)
		}
	}

	if judged == 0 {
		t.Fatal("no row carries a version, so this case judged nothing")
	}
}

// unversionedNames is the debt list in the committed baseline.
func unversionedNames(t *testing.T) map[string]bool {
	t.Helper()

	var file struct {
		Unversioned []struct {
			Vocabulary string `json:"vocabulary"`
		} `json:"unversioned"`
	}

	readVocabularyBaseline(t, &file)

	out := make(map[string]bool, len(file.Unversioned))
	for _, entry := range file.Unversioned {
		out[entry.Vocabulary] = true
	}

	return out
}

// baselinedNames is every vocabulary the baseline holds a set for.
func baselinedNames(t *testing.T) map[string]bool {
	t.Helper()

	var file struct {
		Vocabularies map[string]json.RawMessage `json:"vocabularies"`
	}

	readVocabularyBaseline(t, &file)

	out := make(map[string]bool, len(file.Vocabularies))
	for name := range file.Vocabularies {
		out[name] = true
	}

	return out
}

func readVocabularyBaseline(t *testing.T, into any) {
	t.Helper()

	data, err := os.ReadFile(vocabularyBaseline)
	if err != nil {
		t.Fatalf("read %s: %v", vocabularyBaseline, err)
	}

	if err = json.Unmarshal(data, into); err != nil {
		t.Fatalf("parse %s: %v", vocabularyBaseline, err)
	}
}
