package event

import "time"

// The payload types of the decisions and the changes about one server in a
// tree of servers. Only the host publishes them, so a module cannot forge the
// person who approved a server.

// The payload bounds, in bytes. A payload over its bound is not published.
const (
	// LinkNodeDecidedMaxBytes bounds the approved and revoked payloads.
	LinkNodeDecidedMaxBytes = 1024
	// LinkNodeJoinedMaxBytes bounds the joined payload, which includes a
	// public key.
	LinkNodeJoinedMaxBytes = 2048
	// LinkNodePresenceMaxBytes bounds the lost and returned payloads.
	LinkNodePresenceMaxBytes = 512
	// LinkJoinDeniedMaxBytes bounds the denied payload, which includes the
	// sentence the server above wrote.
	LinkJoinDeniedMaxBytes = 2048
)

// LinkNodeDecided is the data member of com.prusa.connect.link.node.approved
// and of com.prusa.connect.link.node.revoked.
type LinkNodeDecided struct {
	// Link is the name of the link instance the decision was made on.
	Link string `json:"link"`
	// Node is the id of the server the decision is about.
	Node string `json:"node"`
	// AuthType and AuthID name the person who decided, split as the
	// envelope's audit attributes split it. Both are empty on a server that
	// requires no sign-in.
	AuthType string `json:"auth_type,omitempty"`
	AuthID   string `json:"auth_id,omitempty"`
}

// LinkNodeJoined is the data member of com.prusa.connect.link.node.joined. It
// reports a server this site had no record of.
type LinkNodeJoined struct {
	// Link is the name of the link instance the server joined under.
	Link string `json:"link"`
	// Node is the id the server joined under.
	Node string `json:"node"`
	// Path is where the server sits in the tree, from the root down to it. It
	// is empty for a protocol that has no paths.
	Path string `json:"path,omitempty"`
	// Admission says whether this site lets the server act here: pending,
	// admitted or revoked.
	Admission string `json:"admission"`
	// PublicKey is the server's public identity key as text. It is empty when
	// the link has not learned the key.
	PublicKey string `json:"public_key,omitempty"`
}

// LinkNodePresence is the data member of com.prusa.connect.link.node.lost and
// of com.prusa.connect.link.node.returned. It reports the connection to one
// server under this site opening or closing.
type LinkNodePresence struct {
	// Link is the name of the link instance the server is under.
	Link string `json:"link"`
	// Node is the id of the server.
	Node string `json:"node"`
	// Connected says whether the connection is open. It is false on lost and
	// true on returned.
	Connected bool `json:"connected"`
	// LastSeen is when something last arrived from the server, written as RFC
	// 3339 text. It is absent until something has.
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

// LinkJoinDenied is the data member of com.prusa.connect.link.node.denied. It
// reports the server above refusing this site's request to join.
type LinkJoinDenied struct {
	// Link is the name of the link instance whose request was refused.
	Link string `json:"link"`
	// Address is the address of the server above, in the protocol's format.
	Address string `json:"address,omitempty"`
	// Code is the protocol's number for the refusal, chosen by the server
	// above. It is zero for a protocol that gives none.
	Code int `json:"code,omitempty"`
	// FaultCode is the link's registered code for the refusal, written as
	// domain/REASON. It is empty for a link that registers none.
	FaultCode string `json:"fault_code,omitempty"`
	// Reason is the sentence the server above wrote. Show it to a person. Do
	// not branch on it.
	Reason string `json:"reason,omitempty"`
}
