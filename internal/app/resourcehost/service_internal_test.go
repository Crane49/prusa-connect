package resourcehost

import (
	"errors"
	"fmt"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// TestAModuleWithUndescribedRoutesFailsWithItsCode is the plane's half of
// ADR-20260909-c7x rule 9: the wire host's refusal becomes the code an operator reads
// off the instance's conditions, and the code is one the supervisor does not
// retry, because waiting does not write a fragment.
func TestAModuleWithUndescribedRoutesFailsWithItsCode(t *testing.T) {
	t.Parallel()

	instance, err := ref.Parse("sink:example")
	if err != nil {
		t.Fatal(err)
	}

	refused := fmt.Errorf("sink:example: the module example-blob names routes.serve: %w", wirehost.ErrRoutesUndescribed)

	got := launchFailure(instance, "plugins/example", refused)

	var occurrence *fault.Fault
	if !errors.As(got, &occurrence) {
		t.Fatalf("the refusal %v carries no fault", got)
	}

	if occurrence.Code() != CodePluginUndocumented() {
		t.Errorf("the refusal carries %s, and undescribed routes are %s", occurrence.Code(), CodePluginUndocumented())
	}

	if !errors.Is(got, wirehost.ErrRoutesUndescribed) {
		t.Error("the fault lost the wire host's reason")
	}
}
