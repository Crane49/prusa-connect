package resourcehost

import (
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// A request reads the headers the operator granted while a reconcile replaces
// the declaration they come from. Under the race detector this fails when the
// read leaves mu before it reads the declaration.
func TestTheGrantedHeadersAreReadWhileAReconcileReplacesThem(t *testing.T) {
	t.Parallel()

	declared := declaredAs(t, ref.KindSink, "slicer", "com.github.acme.slicer/slicer", install.RuntimeService)
	declared.Spec.Service = &install.Service{RequestHeaders: []string{"Print-After-Upload"}}
	declared.Spec.Module = &install.ModuleEntry{}

	host := &Host{
		cfg:     Config{Logger: slog.New(slog.DiscardHandler)},
		running: map[ref.Ref]*instance{declared.Ref: {declaration: declared}},
	}

	var group sync.WaitGroup

	group.Go(func() {
		for range 200 {
			replaced := declared
			replaced.Spec.Service = &install.Service{RequestHeaders: []string{"Overwrite"}}

			host.mu.Lock()
			host.running[declared.Ref].declaration = replaced
			host.mu.Unlock()
		}
	})

	group.Go(func() {
		for range 200 {
			// assert, not require: FailNow must run on the test's own goroutine.
			granted := host.headersFor(declared.Ref)
			assert.Len(t, granted, 1)
		}
	})

	group.Wait()
}
