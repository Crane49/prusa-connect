package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTaggingADeviceWritesTheListAndAnswersIt holds the tags route to what a
// page needs: the list reaches the fleet in the order sent, and the answer is
// the list the device then carries.
func TestTaggingADeviceWritesTheListAndAnswersIt(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodPut,
		"/api/v1/devices/"+examplePrinterID+"/tags", `{"tags":["workshop","night shift"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var answer struct {
		Tags []string `json:"tags"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	require.Equal(t, []string{"workshop", "night shift"}, answer.Tags)
	require.Equal(t, [][]string{{"workshop", "night shift"}}, fleet.tagged,
		"the handler bound the body and did not hand it to the fleet")
}

// TestTaggingADeviceThisServerDoesNotHoldIsNotFound is the refusal every device
// operation gives for a device the caller reaches nothing on.
func TestTaggingADeviceThisServerDoesNotHoldIsNotFound(t *testing.T) {
	t.Parallel()

	handler := serverWithFleet(t, &fakeFleet{printer: examplePrinter()}).Handler()

	rec := call(t, handler, http.MethodPut, "/api/v1/devices/nobody/tags", `{"tags":["workshop"]}`)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestTaggingADeviceRefusesMoreTagsThanTheSchemaAllows keeps an oversized list
// out before the fleet sees it.
func TestTaggingADeviceRefusesMoreTagsThanTheSchemaAllows(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter()}
	handler := serverWithFleet(t, fleet).Handler()

	tags := make([]string, 33)
	for index := range tags {
		tags[index] = "tag"
	}

	body, err := json.Marshal(map[string][]string{"tags": tags})
	require.NoError(t, err)

	rec := call(t, handler, http.MethodPut, "/api/v1/devices/"+examplePrinterID+"/tags", string(body))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Empty(t, fleet.tagged, "the fleet was handed a list the schema refuses")
}

// TestTheListingBindsTheTagFilter reads the query the fleet was handed, so a
// handler that publishes the parameter and drops it fails here.
func TestTheListingBindsTheTagFilter(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/devices?tag=workshop,night%20shift", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, fleet.queries, 1)
	require.Equal(t, []string{"workshop", "night shift"}, fleet.queries[0].Tags)
}
