package resource

import "context"

// reportFailures logs which declared instances would not load. A bad block
// fails only its instance and the server serves the rest, so this returns no
// error. The log line tells the operator that the block is not running.
func (s *Store) reportFailures(ctx context.Context, doc Document) {
	if s.log == nil {
		return
	}

	for _, failed := range doc.Failures {
		s.log.WarnContext(ctx, "a declared resource instance will not load and is not running",
			"instance", failed.Key, "error", failed.Err.Error())
	}
}

// reportRefused logs which instances the supervision plane refused, one line
// each. A refusal applies to one instance and never to the whole process. It
// is a log line and a status on that instance. It does not fail the start of
// the server and it does not undo a write.
//
// The plane joins one error per instance, and the join is split again here.
// One line per instance is easier for an operator to act on than one line
// with every failure.
func (s *Store) reportRefused(ctx context.Context, err error) {
	for _, one := range refusals(err) {
		s.log.ErrorContext(ctx, "the supervision plane refused a declared resource instance and it is not running",
			"error", one.Error())
	}
}

// refusals splits an error made by errors.Join into one error per instance.
// An error that is not a join is one refusal.
func refusals(err error) []error {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []error{err}
	}

	var out []error
	for _, one := range joined.Unwrap() {
		out = append(out, refusals(one)...)
	}

	return out
}
