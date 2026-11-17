package resourcehost

import (
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// recordLimitOf is the byte limit of one declaration's record namespace: what
// the operator granted the instance, or the default for a declaration that
// grants nothing.
func recordLimitOf(declared resource.Declaration) int64 {
	if declared.Spec.Service == nil {
		return stateapi.DefaultNamespaceBytes
	}

	return declared.Spec.Service.RecordLimit()
}

// RecordLimit is the byte limit of the running instance that claims
// namespace, and the default when none claims it.
//
// Code that writes into a module's namespace for a person reads it, such as
// the module settings the browser keeps, so the person has the same limit as
// the module. claimNamespace lets only one running instance hold a namespace,
// so there is one answer.
func (h *Host) RecordLimit(namespace string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, running := range h.running {
		if held, ok := moduleNamespaceOf(running.declaration); ok && held == namespace {
			return recordLimitOf(running.declaration)
		}
	}

	return stateapi.DefaultNamespaceBytes
}
