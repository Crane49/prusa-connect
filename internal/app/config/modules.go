package config

// ModulesOn reports whether the server may install modules, and start and
// serve the ones it installed. An absent key is on.
func (c Core) ModulesOn() bool { return c.ModulesEnabled == nil || *c.ModulesEnabled }
