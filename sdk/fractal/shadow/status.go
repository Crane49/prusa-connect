package shadow

// Status says whether the property values of a shadow interface are current.
type Status uint8

// The values of Status.
const (
	// StatusEmpty means no REP has arrived for the interface. It has no
	// property values and no update time.
	StatusEmpty Status = iota + 1
	// StatusOK means the interface has the property values the node last
	// reported. Its update time says when the node had them.
	StatusOK
	// StatusStale means the connection to the node is lost. The interface
	// still has the last reported property values and their update time, and
	// the node may have changed since.
	StatusStale
)

// statusNames has the name the protocol gives each status.
var statusNames = [...]string{
	StatusEmpty: "EMPTY",
	StatusOK:    "OK",
	StatusStale: "STALE",
}

// Valid reports whether s is StatusEmpty, StatusOK or StatusStale.
func (s Status) Valid() bool { return int(s) > 0 && int(s) < len(statusNames) }

// String returns "EMPTY", "OK" or "STALE", and "unknown" for any other value.
func (s Status) String() string {
	if !s.Valid() {
		return "unknown"
	}
	return statusNames[s]
}

// Statuses returns every status: EMPTY, OK, STALE. The caller owns the
// returned slice.
func Statuses() []Status {
	out := make([]Status, 0, len(statusNames)-1)
	for i := 1; i < len(statusNames); i++ {
		out = append(out, Status(i))
	}
	return out
}

// transition is one row of the status table: a change from one status to
// another, and whether it is allowed.
type transition struct {
	from    Status
	to      Status
	allowed bool
	// when is the event that causes an allowed change, or the reason the
	// change is not allowed.
	when string
}

// transitions is the status table of the Fractal specification. A row from a
// status to the same status is allowed and changes nothing.
var transitions = [...]transition{
	{StatusEmpty, StatusEmpty, true, "connectivity is lost while the copy is empty"},
	{StatusEmpty, StatusOK, true, "state information arrives from the node"},
	{StatusEmpty, StatusStale, false, "an empty copy has no property values that could be stale"},
	{StatusOK, StatusEmpty, false, "an interface that has property values does not become empty again"},
	{StatusOK, StatusOK, true, "state information arrives from the node"},
	{StatusOK, StatusStale, true, "connectivity to the node is lost"},
	{StatusStale, StatusEmpty, false, "an interface that has property values does not become empty again"},
	{StatusStale, StatusOK, true, "state information arrives from the node"},
	{StatusStale, StatusStale, true, "connectivity is lost again"},
}

// allowedTable has the allowed column of transitions, indexed by from and to.
var allowedTable = buildAllowedTable()

// buildAllowedTable copies transitions into the array.
func buildAllowedTable() [len(statusNames)][len(statusNames)]bool {
	var table [len(statusNames)][len(statusNames)]bool
	for _, t := range transitions {
		table[t.from][t.to] = t.allowed
	}
	return table
}

// Allowed reports whether a shadow interface may change from the status from
// to the status to. EMPTY may not change to STALE, and OK and STALE may not
// change to EMPTY. It returns false when either value is not a valid Status.
func Allowed(from, to Status) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	return allowedTable[from][to]
}
