package resourcehost

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// A module the operator approved with list_people may list the people this
// server holds. The method is installed only for such a module. A listing
// spends a token from the same bucket as a question about a person the module
// is not serving, and every listing writes one audit line.

// listedPeople is the access.people a module is served.
type listedPeople struct {
	instance ref.Ref
	people   People
	budget   *absentBudgets
	quiet    *quietLog
	log      *slog.Logger
}

// DirectoryFor is the access.people the host serves one module, and nil for a
// module whose operator did not approve list_people. A nil directory installs
// no method, so such a module reads CodeMethodNotFound.
func (h *Host) DirectoryFor(module Module) accessapi.Directory {
	if !module.ListPeople {
		return nil
	}

	return listedPeople{
		instance: module.Instance,
		people:   h.cfg.People,
		budget:   &h.absent,
		quiet:    &h.quiet,
		log:      h.cfg.Logger,
	}
}

// People answers the people the server keeps. A server that keeps no list of
// people answers nobody: one with an OpenID Connect login, or one in open
// mode. The pairings such a server holds name a credential and the label an
// administrator wrote. Neither is a person's name, so they are not listed.
func (l listedPeople) People(ctx context.Context) ([]accessapi.Person, error) {
	if !l.budget.take(l.instance, time.Now()) {
		if write, missed := l.quiet.write(quietKey{instance: l.instance, what: "bound"}, time.Now()); write {
			l.log.WarnContext(ctx, "a module passed its bound on questions about people it is not serving",
				"burst", absentBurst, "per_second", absentPerSecond,
				"resource", l.instance.String(), "left_out", missed)
		}

		return nil, fmt.Errorf("%w: %s may ask %d a second",
			accessapi.ErrTooManyQuestions, l.instance, absentPerSecond)
	}

	if l.people == nil || !l.people.Kept() {
		l.audit(ctx, 0, "this server keeps no list of people")

		return nil, nil
	}

	listed, err := l.people.List(ctx)
	if err != nil {
		l.audit(ctx, 0, "failed: the people could not be read")

		return nil, fmt.Errorf("list the people for %s: %w", l.instance, err)
	}

	l.audit(ctx, len(listed), "listed")

	return listed, nil
}

// audit writes one line per listing.
func (l listedPeople) audit(ctx context.Context, rows int, answer string) {
	l.log.InfoContext(ctx, "a module listed the people this server holds",
		"resource", l.instance.String(), "rows", rows, "answer", answer)
}
