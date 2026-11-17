package auth

// Kind is what a credential is for. The kind is a property of the stored row,
// never of the token text, so a holder cannot choose it.
type Kind uint8

// The credential kinds. Their lifetimes differ, and so do the transports that
// accept them.
const (
	// KindUnknown is the zero value and is accepted nowhere.
	KindUnknown Kind = iota
	// KindSession is the browser credential: an HttpOnly cookie with a sliding
	// idle window.
	KindSession
	// KindAPI is a Bearer credential for a script or an integration.
	KindAPI
	// KindMQTT is a broker credential and is refused on every other transport.
	KindMQTT
	// KindPrusaAccount is a token another issuer signed and an identity plugin
	// verified, resolved to a local principal through a pairing. No row is
	// stored for it and nothing here mints one. The kind exists so the rest of
	// the system reads the credential the way it reads an api one.
	KindPrusaAccount
)

// nameUnknown is what an out-of-range enum prints. Both enums use it, because
// neither has a meaning to report for a value it does not declare.
const nameUnknown = "unknown"

// Transport is where a credential was presented. The same token is accepted on
// one and refused on another, so the caller has to say which.
type Transport uint8

// The transports a credential can arrive on.
const (
	// TransportUnknown is the zero value and accepts nothing.
	TransportUnknown Transport = iota
	// TransportHTTP is a request-response call.
	TransportHTTP
	// TransportSSE is a long-lived event stream, which is HTTP on the wire and
	// a different acceptance rule here.
	TransportSSE
	// TransportMQTT is the broker.
	TransportMQTT
)

// String names the kind for a log line. It is not parsed anywhere.
func (k Kind) String() string {
	switch k {
	case KindSession:
		return "session"
	case KindAPI:
		return "api"
	case KindMQTT:
		return "mqtt"
	case KindPrusaAccount:
		return "prusa-account"
	case KindUnknown:
		return nameUnknown
	default:
		return nameUnknown
	}
}

// NamesGraphSubject reports whether the credential's Subject is the full graph
// subject, user:<id> or service_account:<id>, instead of the bare id a session
// carries. An api credential is minted for a graph subject and a bearer token
// is paired to one, so both may act for a service account.
func (k Kind) NamesGraphSubject() bool {
	return k == KindAPI || k == KindPrusaAccount
}

// AcceptedOn is the cross-transport rule. An mqtt credential is refused on HTTP
// and on SSE. It has a long life and no cookie or origin protections around it,
// so honouring it on a browser transport would give a broker credential the
// whole API.
//
// An api credential is accepted wherever a session is. The event stream serves
// what the holder may already read one call at a time, under the same principal
// and the same grants, so refusing it there would withhold nothing and would
// stop a script or a client SDK from subscribing.
//
// A prusa-account token is accepted where an api credential is, for the same
// reason: the compatibility client that presents it reads the same stream a
// script does, under the principal it was paired to.
func (k Kind) AcceptedOn(on Transport) bool {
	switch k {
	case KindSession, KindAPI, KindPrusaAccount:
		return on == TransportHTTP || on == TransportSSE
	case KindMQTT:
		return on == TransportMQTT
	case KindUnknown:
		return false
	default:
		return false
	}
}

// String names the transport for a log line.
func (t Transport) String() string {
	switch t {
	case TransportHTTP:
		return "http"
	case TransportSSE:
		return "sse"
	case TransportMQTT:
		return "mqtt"
	case TransportUnknown:
		return nameUnknown
	default:
		return nameUnknown
	}
}
