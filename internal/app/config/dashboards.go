package config

// Dashboards is the home page as pages of cards a person lays out.
//
// Enabled is a pointer for the reason core.frontend.enabled is one: an operator
// who wrote `enabled: false` is told apart from one who wrote no block, and
// only the first means off. `make build` reads the same key: a file that
// switches dashboards off builds a binary without them.
type Dashboards struct {
	// Enabled serves the dashboards: their routes, their rows and the events
	// that say one changed. The default is on. Off, the home page is the
	// printer list, and the stored rows stay in the database.
	Enabled *bool `yaml:"enabled"`
}

// On reports whether this server serves dashboards. An absent key is on.
func (d Dashboards) On() bool { return d.Enabled == nil || *d.Enabled }
