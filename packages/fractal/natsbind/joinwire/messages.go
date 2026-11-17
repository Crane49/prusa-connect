package joinwire

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// SubjectJoin is the NATS subject a node sends its join request to. A node
// sends it before it has a link, so the subject has no Node ID in it.
const SubjectJoin = "fractal.join"

// CodeAwaitingApproval is a denial code. It means the node above recorded the
// join request and an operator has not approved it yet. The node that asked
// sends the request again later. It does not need a different join
// credential.
//
// The code is a sub-code of fractal.CodeJoinUnauthorized. A binding may
// define one by setting a non-zero sub-code on the same base.
const CodeAwaitingApproval fractal.Code = 0x2031

// The errors Verify returns for a join request.
var (
	// ErrNotCompact means the join request is not a compact JWS: it does not
	// have a header, a body and a signature separated by dots.
	ErrNotCompact = errors.New("the join request is not a compact jws")
	// ErrBadAlgorithm means the header's alg is not EdDSA. Verify checks it
	// before it reads the key or the body.
	ErrBadAlgorithm = errors.New("the join request does not declare EdDSA")
	// ErrBadType means the typ of the protected header is missing or names
	// another purpose than the one the verifier expects, JoinTyp for a join
	// request. Verify checks it before anything else in the header.
	ErrBadType = errors.New("the jws does not name the expected purpose in typ")
	// ErrNoNKey means the protected header has no public key in its nkey
	// member.
	ErrNoNKey = errors.New("the join request header carries no nkey")
	// ErrBadSignature means the signature does not match the public key in
	// the header.
	ErrBadSignature = errors.New("the join request signature does not verify")
	// ErrEnvelopeTooLarge means the join request has more bytes than
	// ProofVerifier.MaxEnvelopeBytes. ProofVerifier checks the size before it
	// parses the request.
	ErrEnvelopeTooLarge = errors.New("the join request is over the size bound")
	// ErrNotAUserKey means the public key in the header is not a user NKEY.
	// NATS accepts only a user key as the key of a connection.
	ErrNotAUserKey = errors.New("the identity key is not a user nkey")
)

// AlgEdDSA is the only algorithm a join request may name. An NKEY is an
// Ed25519 key pair, so a node signs the NATS nonce and the join request with
// the same key.
const AlgEdDSA = "EdDSA"

// Header is the protected header of the join request JWS. The signature covers
// it.
type Header struct {
	// Typ names the purpose of the signature. It is JoinTyp on a join
	// request.
	Typ string `json:"typ"`
	// Alg is the signature algorithm. It is always EdDSA.
	Alg string `json:"alg"`
	// NKey is the node's public identity key, in the text encoding of an
	// NKEY.
	NKey string `json:"nkey"`
}

// Request is the body of the join request JWS. It is not a Fractal message
// type, so it has no sender, no receiver and no id. The public identity key
// is in the protected header and not here.
type Request struct {
	// Version is the Fractal protocol version of the node that asks.
	Version uint `json:"version"`
	// NodeID is the Node ID the node asks for.
	NodeID fractal.NodeID `json:"nodeId"`
	// JoinToken is the join credential. JWT mode needs it. It is empty
	// otherwise.
	JoinToken fractal.JoinCredential `json:"joinToken,omitempty"`
}

// Reply is the answer to a join request: a grant or a denial. A receiver has
// to ignore a member it does not know, and encoding/json does that.
type Reply struct {
	// Version is the protocol version of the node that answers.
	Version uint `json:"version"`
	// Granted is true for a grant and false for a denial.
	Granted bool `json:"granted"`
	// NodePath is the Node Path the node above assigns to the node that
	// joined.
	NodePath fractal.NodePath `json:"nodePath,omitempty"`
	// ParentIdentityKey is the public identity key of the node above.
	ParentIdentityKey fractal.Identity `json:"parentIdentityKey,omitempty"`
	// LeafCredentials is a NATS user JWT in the account of this link. It
	// works only with the identity key that signed the request.
	LeafCredentials fractal.ConnectionCredentials `json:"leafCredentials,omitempty"`
	// CredentialsExpireAt is when LeafCredentials expire, in unix
	// milliseconds. A grant always sets it.
	CredentialsExpireAt fractal.Timestamp `json:"credentialsExpireAt,omitempty"`
	// LeafRemote is the leafnode address of the node above. The NATS server
	// of the node that joined connects to it.
	LeafRemote string `json:"leafRemote,omitempty"`
	// MaxHeartbeatInterval is the longest heartbeat interval the node above
	// allows, in milliseconds.
	MaxHeartbeatInterval uint32 `json:"maxHeartbeatInterval,omitempty"`
	// Extensions is the join data of the protocol extensions the node above
	// offers, keyed by the name of the extension.
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
	// Code is the denial code. It is zero on a grant.
	Code fractal.Code `json:"code,omitempty"`
	// Reason is text for a person. It may be empty.
	Reason string `json:"reason,omitempty"`
}

// Extra has the members of a grant that the NATS binding adds to the Fractal
// grant. A node saves them with its Node Path and credentials, because the
// node above sends them in no other message.
type Extra struct {
	// LeafRemote is the leafnode address of the node above.
	LeafRemote string `json:"leafRemote"`
	// MaxHeartbeatIntervalMS is the longest heartbeat interval the node above
	// allows, in milliseconds.
	MaxHeartbeatIntervalMS uint32 `json:"maxHeartbeatIntervalMs"`
}

// HeartbeatInterval returns MaxHeartbeatIntervalMS as a duration.
func (e Extra) HeartbeatInterval() time.Duration {
	return time.Duration(e.MaxHeartbeatIntervalMS) * time.Millisecond
}

// Grant builds the answer to a join request that passed every check.
func Grant(g fractal.JoinGrant, leafRemote string, heartbeat time.Duration) Reply {
	return Reply{
		Version:             g.Version,
		Granted:             true,
		NodePath:            g.NodePath,
		ParentIdentityKey:   g.ParentIdentity,
		LeafCredentials:     g.Credentials,
		CredentialsExpireAt: g.ExpiresAt,
		LeafRemote:          leafRemote,
		Extensions:          rekey[string](g.Extensions),

		MaxHeartbeatInterval: uint32(min(max(heartbeat.Milliseconds(), 0), maxIntervalMS)),
	}
}

// maxIntervalMS is the largest value of Reply.MaxHeartbeatInterval, which is
// an unsigned 32-bit number of milliseconds.
const maxIntervalMS = int64(^uint32(0))

// Deny builds the answer to a join request that failed a check.
func Deny(d fractal.JoinDenial) Reply {
	return Reply{Version: d.Version, Granted: false, Code: d.Code, Reason: d.Reason}
}

// ToGrant splits a granted reply into the Fractal grant and the Extra
// members. It returns ErrDenied for a denial and ErrNoLeafRemote for a grant
// with no leafnode address.
func (r Reply) ToGrant() (fractal.JoinGrant, Extra, error) {
	if !r.Granted {
		return fractal.JoinGrant{}, Extra{}, fmt.Errorf("code %s: %w", r.Code, ErrDenied)
	}
	g := fractal.JoinGrant{
		Version:        r.Version,
		NodePath:       r.NodePath,
		ParentIdentity: r.ParentIdentityKey,
		Credentials:    r.LeafCredentials,
		ExpiresAt:      r.CredentialsExpireAt,
		Extensions:     rekey[fractal.Identifier](r.Extensions),
	}
	if r.LeafRemote == "" {
		return fractal.JoinGrant{}, Extra{}, ErrNoLeafRemote
	}
	extra := Extra{LeafRemote: r.LeafRemote, MaxHeartbeatIntervalMS: r.MaxHeartbeatInterval}
	return g, extra, nil
}

// rekey copies join data under another string type of key. It returns nil
// for an empty map, so a grant with no extensions has no extensions member.
func rekey[To, From ~string](in map[From]json.RawMessage) map[To]json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(map[To]json.RawMessage, len(in))
	for name, data := range in {
		out[To(name)] = data
	}
	return out
}

// ToDenial returns the version, the code and the reason of a reply as a
// fractal.JoinDenial.
func (r Reply) ToDenial() fractal.JoinDenial {
	return fractal.JoinDenial{Version: r.Version, Code: r.Code, Reason: r.Reason}
}

// The errors ToGrant returns.
var (
	// ErrDenied means the reply is a denial, so it has no grant.
	ErrDenied = errors.New("the answer is a denial")
	// ErrNoLeafRemote means the grant has no leafnode address. The node that
	// joined cannot connect without it, and no other message has the address.
	ErrNoLeafRemote = errors.New("the grant names no leafnode address")
)

// EncodeReply encodes a Reply as JSON.
func EncodeReply(r Reply) ([]byte, error) {
	payload, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode the join reply: %w", err)
	}
	return payload, nil
}

// DecodeReply decodes a Reply from JSON. It ignores a member it does not know.
func DecodeReply(payload []byte) (Reply, error) {
	var r Reply
	if err := json.Unmarshal(payload, &r); err != nil {
		return Reply{}, fmt.Errorf("parse the join reply: %w", err)
	}
	return r, nil
}
