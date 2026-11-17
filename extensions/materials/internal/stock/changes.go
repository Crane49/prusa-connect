package stock

// Changes hears what the store committed: an assignment of a tool, and a
// spool. It is told after the commit, and must not block or call back into
// the store.
type Changes interface {
	// Tool is a tool whose assignment was written or dropped.
	Tool(deviceID, toolID string)
	// Spool is a spool added, changed, put on or taken off a tool, or
	// deleted. weightOnly is a change of nothing but its weight, as a tool
	// feeding filament makes.
	Spool(id int64, weightOnly bool)
}

// Watch tells c of every change from now on.
func (s *Store) Watch(c Changes) { s.changes = c }

func (s *Store) toolChanged(deviceID, toolID string) {
	if s.changes != nil {
		s.changes.Tool(deviceID, toolID)
	}
}

func (s *Store) spoolChanged(weightOnly bool, ids ...int64) {
	if s.changes == nil {
		return
	}

	for _, id := range ids {
		s.changes.Spool(id, weightOnly)
	}
}
