package resourcehost

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/diagnosticsapi"
)

// errNoDiagnostics reports a target that is not a module process. Only a
// module process answers diagnostics.collect.
var errNoDiagnostics = errors.New("the instance is not a module process")

// DiagnosticsSource is one running module that adds files to a diagnostics
// bundle.
type DiagnosticsSource struct {
	Instance ref.Ref
	// Title is the module's name as a person reads it, from its manifest, or
	// the instance's name where the manifest gives none.
	Title string
	// Collect asks the module for one page, through the supervised call, so
	// the call deadline, the breaker and the panic recovery apply.
	Collect func(ctx context.Context, cursor string) (diagnosticsapi.Page, error)
}

// DiagnosticsSources is every running module that serves
// diagnostics.collect, in instance order.
func (h *Host) DiagnosticsSources() []DiagnosticsSource {
	h.mu.Lock()
	defer h.mu.Unlock()

	var out []DiagnosticsSource

	for instance, running := range h.running {
		serves, ok := supervisor.Peek(running.runner, func(target any) bool {
			child, isChild := target.(*serviceChild)

			return isChild && child.child.Info().Answers(diagnosticsapi.MethodCollect)
		})
		if !ok || !serves {
			continue
		}

		title := instance.ID()
		if module := running.declaration.Spec.Module; module != nil && module.Title != "" {
			title = module.Title
		}

		supervised := running.runner

		out = append(out, DiagnosticsSource{
			Instance: instance,
			Title:    title,
			Collect: func(ctx context.Context, cursor string) (diagnosticsapi.Page, error) {
				return supervisor.Call(ctx, supervised, string(diagnosticsapi.MethodCollect),
					func(callCtx context.Context, target any) (diagnosticsapi.Page, error) {
						return collectThroughChild(callCtx, target, cursor)
					})
			},
		})
	}

	slices.SortFunc(out, func(a, b DiagnosticsSource) int {
		return cmp.Compare(a.Instance.String(), b.Instance.String())
	})

	return out
}

// collectThroughChild makes one diagnostics.collect call and validates the
// page before the caller reads it.
func collectThroughChild(ctx context.Context, target any, cursor string) (diagnosticsapi.Page, error) {
	child, ok := target.(moduleCaller)
	if !ok {
		return diagnosticsapi.Page{}, fmt.Errorf("%w: %T", errNoDiagnostics, target)
	}

	raw, err := child.Call(ctx, string(diagnosticsapi.MethodCollect), diagnosticsapi.Request{Cursor: cursor})
	if err != nil {
		return diagnosticsapi.Page{}, fmt.Errorf("call the module: %w", err)
	}

	var page diagnosticsapi.Page
	if err = json.Unmarshal(raw, &page); err != nil {
		return diagnosticsapi.Page{}, fmt.Errorf("read the module's page: %w", err)
	}

	if err = page.Validate(); err != nil {
		return diagnosticsapi.Page{}, err
	}

	return page, nil
}
