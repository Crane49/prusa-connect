package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

const (
	resourceMemoryPath = resourcePath + "/memory"
	resourceLogPath    = resourcePath + "/log"
)

// MemoryPoint is one reading of a module's memory.
type MemoryPoint struct {
	At    time.Time `json:"at"              doc:"When the host read the memory."`
	Value *int64    `json:"value,omitempty" doc:"The memory the process used, in bytes. Absent when the host could not read it, which is not zero."`
}

// ResourceMemoryHistory is what a module's process used over the last hour and
// the stops inside the same window.
type ResourceMemoryHistory struct {
	Items []MemoryPoint  `json:"items" doc:"The readings, oldest first. One every two seconds while the module has run."              nullable:"false"`
	Stops []ResourceStop `json:"stops" doc:"The failed stops the host still counts, oldest first, inside the window of the readings." nullable:"false"`
}

// ResourceLogLine is one line a module wrote to standard error.
type ResourceLogLine struct {
	At    time.Time `json:"at"              doc:"When the host read the line."`
	Text  string    `json:"text"            doc:"The line as the module wrote it."`
	Split bool      `json:"split,omitempty" doc:"Whether the line continues in the next entry, because it was too long to keep whole."`
}

// ResourceLog is the newest standard error of a module, oldest line first.
type ResourceLog struct {
	Lines []ResourceLogLine `json:"lines" doc:"The newest lines, oldest first." nullable:"false"`
}

type resourceMemoryInput struct {
	ResourceRef

	Since time.Time `doc:"Start of the window, as RFC 3339. Never earlier than one hour ago, which is also the default." query:"since" example:"2026-10-30T09:00:00Z"`
}

type resourceMemoryOutput struct {
	Body ResourceMemoryHistory
}

type resourceLogInput struct {
	ResourceRef
}

type resourceLogOutput struct {
	Body ResourceLog
}

func registerResourceOperations(api huma.API, store ResourceStore) {
	register(api, huma.Operation{
		OperationID: "get-resource-memory",
		Method:      http.MethodGet,
		Path:        resourceMemoryPath,
		Summary:     "A service module's memory over the last hour",
		Description: "The memory the module's process used, read every two seconds, oldest first, and the " +
			"stops the host still counts against it. The history outlives a restart of the process. A module " +
			"nobody supervises answers empty lists. It takes view_resource on the instance.",
		Tags: []string{"resources"},
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *resourceMemoryInput) (*resourceMemoryOutput, error) {
		instance, err := in.parse()
		if err != nil {
			return nil, err
		}

		history, err := store.MemoryHistory(ctx, principalFrom(ctx), instance, in.Since)
		if err != nil {
			return nil, err
		}

		return &resourceMemoryOutput{Body: history}, nil
	})

	register(api, huma.Operation{
		OperationID: "get-resource-log",
		Method:      http.MethodGet,
		Path:        resourceLogPath,
		Summary:     "What a service module wrote to standard error",
		Description: "The newest 200 lines or 64 KiB, whichever is smaller, as the module wrote them and " +
			"without redaction. The host keeps them in memory, so a server restart empties them. " +
			"It takes configure_resource on the kind: a caller who may see the instance and holds no such " +
			"grant gets 403 naming it, and a caller who may not see the instance gets the answer for one " +
			"that is not there.",
		Tags: []string{"resources"},
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *resourceLogInput) (*resourceLogOutput, error) {
		instance, err := in.parse()
		if err != nil {
			return nil, err
		}

		log, err := store.Log(ctx, principalFrom(ctx), instance)
		if err != nil {
			return nil, err
		}

		return &resourceLogOutput{Body: log}, nil
	})
}
