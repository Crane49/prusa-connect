package script

// The schemes an operator may use in a script's endpoint address.
//
// An address whose scheme is eventScheme names this server's own event stream.
// The rest of the address is the event type the host publishes, so the operator
// chooses both the destination and the type, and the script chooses neither.
const (
	eventScheme = "event"
	httpScheme  = "http"
	httpsScheme = "https"
)

// destination is one entry of the operator's endpoint table, already resolved.
//
// Exactly one field is set. address is an absolute HTTP address the host posts
// to. eventType is the type the host publishes on the local bus. Both come from
// the operator, and a script picks one by naming the table key.
type destination struct {
	address   string
	eventType string
}

// onTheBus reports whether this destination is the local event stream.
func (d destination) onTheBus() bool { return d.eventType != "" }
