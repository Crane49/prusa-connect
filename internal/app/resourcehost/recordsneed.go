package resourcehost

import (
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// recordCapability reports whether a required capability is a method of the
// record store. Such a method is served only while a record backend serves,
// so the build alone does not decide whether the requirement is met.
func recordCapability(capability string) bool {
	switch wirerpc.Method(capability) {
	case stateapi.MethodGet, stateapi.MethodPut, stateapi.MethodCompareAndPut, stateapi.MethodDelete,
		stateapi.MethodList, stateapi.MethodVersion, stateapi.MethodUsage:
		return true
	}

	return false
}

// recordBackendDeclared reports whether this host runs an enabled instance of
// kind db. Each such instance is a record backend.
//
// It reads the instances this host holds when a module starts. A module that
// starts before the backend is declared gets no state method until its next
// start.
func (h *Host) recordBackendDeclared() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	for instance, running := range h.running {
		if instance.Kind() == ref.KindDB && running.declaration.Spec.Enabled {
			return true
		}
	}

	return false
}

// meetsRecords checks a requirement on a method of the record store. It is met
// only while a record backend serves the module's own namespace.
//
// Without that check a module whose every call reads the record store starts,
// reports ready, and then fails every request. The start is retried, because
// an operator can declare a backend, and a backend that is restarting serves
// again by itself.
func (h *Host) meetsRecords(declared resource.Declaration, required install.Requirement) error {
	namespace, isModule := moduleNamespaceOf(declared)
	if !isModule || declared.Ref.Kind() == ref.KindDB {
		return nil
	}

	var (
		held stateapi.Records
		err  error
	)

	if h.cfg.State != nil {
		held, err = h.cfg.State(namespace, recordLimitOf(declared))
	}

	if err == nil && held != nil {
		return nil
	}

	detail := "the module keeps its data in the record store, and no record backend is serving. " +
		"Declare one under resources.db, such as one of type sqlite"
	if err != nil {
		detail += ": " + err.Error()
	}

	return dependencyError{
		reason: reasonDependencyMissing,
		raised: recordsUnserved.New(detail).
			With(
				fault.StringParam("resource", declared.Ref.String()),
				fault.StringParam("requires", required.Describe()),
			),
	}
}
