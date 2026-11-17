package resourcehost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fileapi"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A module can read files of some types for the core. The host finds the
// serving module for a file and makes the call. The caller decides what the
// call carries and what happens to the answer.

// Analyst is one serving module that reads a file type for the core.
type Analyst struct {
	// Instance is the module's instance. A read handle is made for it.
	Instance ref.Ref
	// Namespace and Release name the module and its release. An answer is
	// kept under both.
	Namespace string
	Release   string

	runner *supervisor.Instance[any]
}

// AnalystFor answers the serving module whose file types match a file's
// name. The install refuses two enabled modules that share a type. When a
// configuration file the operator wrote declares two anyway, neither is asked
// and the core reads the file itself.
func (h *Host) AnalystFor(name string) (Analyst, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var (
		chosen Analyst
		found  int
	)

	for instance, running := range h.running {
		module := running.declaration.Spec.Module
		if module == nil || len(module.FileTypes) == 0 ||
			!manifest.MatchesFileType(module.FileTypes, name) || !running.runner.Serving() {
			continue
		}

		namespace, isModule := moduleNamespaceOf(running.declaration)
		if !isModule {
			continue
		}

		found++
		chosen = Analyst{Instance: instance, Namespace: namespace, Release: module.Version, runner: running.runner}
	}

	if found > 1 {
		h.cfg.Logger.Warn("two modules read one file type, so the core reads the file itself",
			"file", name)

		return Analyst{}, false
	}

	return chosen, found == 1
}

// Analyze asks one module to describe one file. The answer must be within the
// bounds sdk/fileapi publishes. Any other answer is an error, and the caller
// treats it as no answer.
func (h *Host) Analyze(ctx context.Context, analyst Analyst, params fileapi.Params) (fileapi.Analysis, error) {
	if analyst.runner == nil {
		return fileapi.Analysis{}, fmt.Errorf("resourcehost: %s: no module", fileapi.MethodAnalyze)
	}

	answered, err := supervisor.Call(ctx, analyst.runner, string(fileapi.MethodAnalyze),
		func(callCtx context.Context, held any) (fileapi.Analysis, error) {
			child, ok := held.(moduleCaller)
			if !ok {
				return fileapi.Analysis{}, fmt.Errorf("%w: %T answers no wire call", errNoRoutes, held)
			}

			return fileapi.Ask(callCtx, wireCaller{child: child}, params)
		})
	if err != nil {
		return fileapi.Analysis{}, fmt.Errorf(
			"resourcehost: %s on %s: %w",
			fileapi.MethodAnalyze,
			analyst.Instance,
			err,
		)
	}

	return answered, nil
}

// wireCaller adapts a child's Call to take a wirerpc.Method.
type wireCaller struct{ child moduleCaller }

func (w wireCaller) Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error) {
	return w.child.Call(ctx, string(method), params) //nolint:wrapcheck // fileapi.Ask wraps it with the method.
}
