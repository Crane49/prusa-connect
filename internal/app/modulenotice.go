package app

// What a module command says once it has written its record: whether a
// running core reads it at once or at its next start. Both add and remove in
// modulecli.go and moduleremove.go end here.

import (
	"context"
	"fmt"
	"io"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// runningCoreNotice says what this command did and did not reach.
//
// It wrote a record. This command is not the running core and cannot make that
// core read the file again, so the answer an operator needs is whether one is
// up: if it is, the same install over the API takes effect at once and this one
// waits for a restart, and if it is not, the next start brings the module up.
// The probe is the same one the container healthcheck makes, against the
// address this configuration names.
//
// sameThroughAPI names the call that does the same at once, because an install
// and a removal reach the API through different operations.
func runningCoreNotice(
	ctx context.Context, out io.Writer, core config.Core, configPath string, movedGrants bool,
	sameThroughAPI string,
) error {
	if !coreAnswers(ctx, configPath) {
		fmt.Fprintf(out, "\nThe record is written to %s. No core is answering on %s, so the next start "+
			"reads it.\n", core.Overlay, core.API.Addr)

		return nil
	}

	fmt.Fprintf(out, "\nThe record is written to %s. A core is answering on %s and this command cannot "+
		"make it read that file again, so restart it so that it reads the change. %s\n",
		core.Overlay, core.API.Addr, sameThroughAPI)

	// The grants are not the record, and they do not wait for the same thing. A
	// running core answers every request against the projection in this same
	// database, so it reads a grant written here on its next request. What it
	// does not reread is an event stream that is already open: a subscription
	// holds the access it was opened with until the generation that core keeps
	// in memory moves, and a write from another process does not move it.
	if movedGrants {
		fmt.Fprint(out, "The grants are in the database that core is reading, so its next request "+
			"answers by them. An event stream already open keeps the access it was opened with until "+
			"that core restarts.\n")
	}

	return nil
}

// coreAnswers reports whether a core is serving on the address this
// configuration names.
//
// A probe that does not answer is a fact about the deployment and never a
// failure of this command: the record is written either way, and which of the
// two happened is the whole of what the notice reports.
func coreAnswers(ctx context.Context, configPath string) bool {
	return Probe(ctx, Options{ConfigPath: configPath}) == nil
}
