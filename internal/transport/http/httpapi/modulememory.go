package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

const moduleMemoryPath = modulesPathPrefix + "/memory"

// ModuleMemoryPool is the memory the service modules share, and how much of it
// their budgets promise. It is a figure to warn with: the server refuses no
// module for it.
type ModuleMemoryPool struct {
	PoolBytes      *int64 `json:"pool_bytes,omitempty" doc:"The memory the budgets of the enabled service modules are meant to fit in together. Absent when the server cannot learn it: the operator set none and the machine's memory could not be read."`
	CommittedBytes int64  `json:"committed_bytes"      doc:"The sum of the memory budgets of the enabled service modules."`
	Source         string `json:"source,omitempty"     doc:"Who set the pool: configured is core.modules_memory, detected is the machine's memory less a reserve for the server and the system. Absent when the server cannot learn the pool."             enum:"detected,configured"`
}

type moduleMemoryOutput struct {
	Body ModuleMemoryPool
}

func registerModuleMemory(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "get-module-memory",
		Method:      http.MethodGet,
		Path:        moduleMemoryPath,
		Summary:     "Memory the modules share",
		Description: "The pool of memory the service modules' budgets are meant to fit in, and the sum of " +
			"the budgets of the enabled ones. When the sum is above the pool the server still runs every " +
			"module: the operator is warned, nothing is refused. It takes configure_resource on a " +
			"resource kind, as the install does.",
		Tags:   []string{"resources"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*moduleMemoryOutput, error) {
		if err := requireModuleAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		return &moduleMemoryOutput{Body: cfg.Resources.ModuleMemory()}, nil
	})
}
