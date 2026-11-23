package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticPeople []Person

func (p staticPeople) People(context.Context) ([]Person, error) { return p, nil }

type failingPeople struct{}

func (failingPeople) People(context.Context) ([]Person, error) {
	return nil, errors.New("the host failed")
}

func me(t *testing.T, principal string, people People) (int, map[string]any) {
	t.Helper()

	recorder := httptest.NewRecorder()
	Me(recorder, httptest.NewRequest(http.MethodGet, "/account/api/v1/me/", nil), principal, people)

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	return recorder.Code, body
}

func TestMeAnswersTheFieldsPrusaSlicerReads(t *testing.T) {
	t.Parallel()

	status, body := me(t, "user:alice", staticPeople{{Subject: "user:alice", Name: "Alice"}})

	if status != http.StatusOK || body["public_username"] != "Alice" {
		t.Fatalf("answered %d: %v", status, body)
	}

	if _, isNumber := body["id"].(float64); !isNumber {
		t.Fatalf("id is %T, and Prusa Account answers a number", body["id"])
	}
}

// A person the host names nothing for, or a host that did not answer, is
// named by the principal's own id, so the slicer still shows somebody.
func TestMeFallsBackToThePrincipalsID(t *testing.T) {
	t.Parallel()

	for name, people := range map[string]People{
		"no name":       staticPeople{{Subject: "user:alice"}},
		"a failed host": failingPeople{},
		"no directory":  nil,
	} {
		if _, body := me(t, "user:alice", people); body["public_username"] != "alice" {
			t.Errorf("%s: public_username is %v", name, body["public_username"])
		}
	}
}

// The me endpoint answers Prusa Account's 401 body when the host resolved
// nobody.
func TestMeWithNobodyIsUnauthorized(t *testing.T) {
	t.Parallel()

	status, body := me(t, "", nil)
	if status != http.StatusUnauthorized || body["detail"] == nil {
		t.Fatalf("answered %d: %v", status, body)
	}
}
