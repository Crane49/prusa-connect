package natsbind

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// The values a Config uses when a field is zero.
const (
	// DefaultClientListen is the address where the embedded NATS server accepts
	// client connections. A node that wants to join sends its join request to
	// this address.
	DefaultClientListen = "127.0.0.1:4222"
	// DefaultAckWait is how long JetStream, the part of NATS that stores
	// messages on disk, waits for an acknowledgement before it delivers a
	// message again.
	DefaultAckWait = 30 * time.Second
	// DefaultCredentialLifetime is how long the connection credentials that a
	// node issues stay valid. A node that joined sends its join request again
	// before they expire, and the answer has new credentials.
	DefaultCredentialLifetime = 24 * time.Hour
	// DefaultHeartbeatInterval is how often a node sends a heartbeat to the
	// node above it.
	DefaultHeartbeatInterval = 5 * time.Second
	// DefaultMaxHeartbeatInterval is the longest heartbeat interval a node
	// allows the nodes below it.
	DefaultMaxHeartbeatInterval = 10 * time.Second
	// DefaultLivenessMultiple is how many heartbeat intervals may pass with no
	// heartbeat before a node treats a node below it as lost. The timeout has
	// to be a multiple of the interval the node allowed.
	DefaultLivenessMultiple = 3
	// DefaultStreamMaxAge is how long a stream keeps a message for a node that
	// cannot be reached. A stream is a JetStream queue stored on disk.
	DefaultStreamMaxAge = 7 * 24 * time.Hour
	// DefaultStreamMaxBytes is the most bytes one stream stores.
	DefaultStreamMaxBytes = 256 << 20
	// DefaultMaxPayload is the largest NATS message the server accepts, in
	// bytes.
	DefaultMaxPayload = 8 << 20
	// DefaultMaxJoinsInFlight is how many join requests a node checks at the
	// same time.
	DefaultMaxJoinsInFlight = 8
	// DefaultJoinsPerSecond is how many join requests a node accepts each
	// second.
	DefaultJoinsPerSecond = 4
	// DefaultLeafPortOffset is added to the client port to get the leafnode
	// port when LeafListen is empty. A leafnode connection is the NATS
	// connection from one NATS server to another server above it.
	DefaultLeafPortOffset = 1000
	// DefaultPollInterval is how often a node asks its NATS server which
	// leafnode connections are open.
	DefaultPollInterval = time.Second
)

// Mode says how a node checks a join request before it lets the sender use the
// Node ID in the request. A node uses one mode.
type Mode string

// The modes.
const (
	// ModeTOFU is trust on first use. The join request needs no join
	// credential. The node accepts a public key the first time that key asks
	// for a Node ID. Use it only on a network where no other machine can
	// reach the join endpoint.
	ModeTOFU Mode = "tofu"
	// ModeJWT requires a join credential in the join request. The credential
	// is a JWT, a signed token, and the node verifies its signature with a
	// public key from Config.KeySource.
	ModeJWT Mode = "jwt"
)

// Valid reports whether m is ModeTOFU or ModeJWT.
func (m Mode) Valid() bool { return m == ModeTOFU || m == ModeJWT }

// String returns the mode as the configuration file writes it.
func (m Mode) String() string { return string(m) }

// The errors Config.Validate returns.
var (
	// ErrNoDedupWindow means DedupWindow is zero. It has no default. It has to
	// cover the longest expiry time of a method invocation plus the longest
	// outage the deployment expects.
	ErrNoDedupWindow = errors.New("dedupWindow is required")
	// ErrNoDataDir means DataDir is empty. A node stores its streams, its
	// key-value buckets and its keys in that directory.
	ErrNoDataDir = errors.New("a data directory is required")
	// ErrNoLeafRemote means AcceptsChildren is set and AdvertisedLeafRemote is
	// empty. A node sends that address to every node that joins it, and it
	// cannot find its own public address.
	ErrNoLeafRemote = errors.New("a node that accepts children must advertise a leafnode address")
	// ErrNoKeySource means the node accepts joins in ModeJWT and KeySource is
	// nil, so it has no public key to verify a join credential with.
	ErrNoKeySource = errors.New("jwt mode needs a key source")
	// ErrBadMode means Verification is neither "tofu" nor "jwt".
	ErrBadMode = errors.New("the verification mode is neither tofu nor jwt")
	// ErrBadHeartbeat means HeartbeatInterval is longer than
	// MaxHeartbeatInterval, or LivenessMultiple is under two.
	ErrBadHeartbeat = errors.New("the heartbeat interval is longer than the one granted")
)

// TLSMaterial names the files with this node's TLS certificate and with the
// CA certificates it trusts.
//
// A nil *TLSMaterial turns TLS off, and the node logs a warning at every
// start. Do that only where something else protects the network.
type TLSMaterial struct {
	// CertFile is this node's certificate. The node presents it on its client
	// listener and on its leafnode listener.
	CertFile string
	// KeyFile is the private key of that certificate. The node reads both
	// files again at every handshake, so it serves a renewed certificate
	// without a restart.
	KeyFile string
	// CAFiles are the CA certificates this node trusts when it connects to
	// the node above it. When the list is empty the node uses the CA
	// certificates of the operating system.
	CAFiles []string
}

// Serves reports whether both CertFile and KeyFile are set, so the node's
// listeners use TLS.
func (m *TLSMaterial) Serves() bool {
	return m != nil && m.CertFile != "" && m.KeyFile != ""
}

// Config is the configuration of a Binding. NodeID, DataDir and DedupWindow
// are required. Every other field has a default or applies to one role only.
//
// It has no Node Path field: the node above assigns the Node Path when this
// node joins.
type Config struct {
	// NodeID is this node's Node ID. A root node, which joins no other node,
	// uses it as its Node Path.
	NodeID fractal.NodeID

	// ParentAddr is the client address of the NATS server of the node this one
	// joins. This node sends its join request there. A root node leaves it
	// empty. The leafnode address of that server is not configured: the answer
	// to the join request has it.
	ParentAddr string

	// AdvertisedLeafRemote is the leafnode address this node sends to every
	// node that joins it, for example "tls://hq.example:7422". It is required
	// when AcceptsChildren is set.
	AdvertisedLeafRemote string

	// ClientListen is the address where this node's NATS server accepts client
	// connections. Join requests arrive there. It defaults to
	// DefaultClientListen.
	ClientListen string

	// LeafListen is the address where this node's NATS server accepts leafnode
	// connections from the nodes below. It defaults to the host of
	// ClientListen with the port plus DefaultLeafPortOffset.
	LeafListen string

	// TLS names this node's certificate and the CA certificates it trusts. Nil
	// turns TLS off, and the node logs a warning at every start.
	TLS *TLSMaterial

	// Identity signs with this node's NKEY, the Ed25519 key pair NATS uses to
	// authenticate. The node sends the public key in its join request, and the
	// credentials it receives work only with this key. The private seed never
	// leaves the node. When Identity is nil the node reads the seed from
	// DataDir, and creates one on the first start.
	Identity Signer

	// JoinCredential is the token this node sends in its join request. The
	// node above needs it when it runs ModeJWT.
	JoinCredential fractal.JoinCredential

	// AcceptsChildren makes this node answer join requests. It then also
	// creates the bootstrap account, the NATS account a node connects to
	// before it has joined.
	AcceptsChildren bool

	// Verification is how this node checks a join request. It defaults to
	// ModeTOFU. The node reads it only when AcceptsChildren is set.
	Verification Mode

	// AskFirst makes the node keep a join request from an unknown public key
	// waiting until an operator approves or denies it. It works with both
	// modes. With ModeTOFU the operator's approval replaces trust on first
	// use. With ModeJWT the node needs a valid join credential and the
	// approval.
	AskFirst bool

	// KeySource returns the public keys that verify a join credential. The
	// standard source is a JWKS endpoint, a URL that serves a JSON list of
	// public keys. It is required in ModeJWT.
	KeySource joinwire.KeySource

	// CredentialLifetime is how long the connection credentials this node
	// issues stay valid. Zero or less means DefaultCredentialLifetime.
	CredentialLifetime time.Duration

	// Shadow selects the interfaces this node keeps a local copy of, and for
	// which nodes below it. The binding does not read it. The code that
	// builds the engine reads it from here.
	Shadow shadow.Config

	// DedupWindow is how long the node recognizes a method invocation it has
	// already received. The streams use it as their duplicates window, and the
	// entries in both key-value buckets expire after it. It is required.
	DedupWindow time.Duration

	// AckWait is how long JetStream waits for an acknowledgement before it
	// delivers a message again. Zero or less means DefaultAckWait.
	AckWait time.Duration

	// StreamMaxAge is how long a stream keeps a message. Set it longer than
	// the longest outage the deployment expects. Zero or less means
	// DefaultStreamMaxAge.
	StreamMaxAge time.Duration

	// StreamMaxBytes is the most bytes one stream stores. Zero or less means
	// DefaultStreamMaxBytes.
	StreamMaxBytes int64

	// HeartbeatInterval is how often this node sends a heartbeat to the node
	// above it. When the node above allows only a shorter interval, the node
	// uses that one. Zero or less means DefaultHeartbeatInterval.
	HeartbeatInterval time.Duration

	// MaxHeartbeatInterval is the longest heartbeat interval this node allows
	// the nodes below it. Zero or less means DefaultMaxHeartbeatInterval.
	MaxHeartbeatInterval time.Duration

	// LivenessMultiple is how many heartbeat intervals may pass with no
	// heartbeat before this node treats a node below it as lost. Zero means
	// DefaultLivenessMultiple. Validate refuses any other value under two.
	LivenessMultiple int

	// DataDir is the directory where this node stores its streams, its
	// key-value buckets and its keys. The node creates it with permission
	// 0700. It is required.
	DataDir string

	// MaxPayload is the largest NATS message the server accepts, in bytes.
	// Zero or less means DefaultMaxPayload.
	MaxPayload int32

	// MaxJoinsInFlight is how many join requests this node checks at the same
	// time. Zero or less means DefaultMaxJoinsInFlight.
	MaxJoinsInFlight int

	// JoinsPerSecond is how many join requests this node accepts each second.
	// Zero or less means DefaultJoinsPerSecond.
	JoinsPerSecond float64

	// MaxPendingNodes is the most Node IDs AskFirst keeps a waiting record of.
	// A record an operator approved does not count. When the bound is reached,
	// a join request for a new Node ID is denied and not recorded, and an
	// operator clears a name with Binding.Reject. Zero or less means
	// joinwire.DefaultMaxPending.
	MaxPendingNodes int

	// PendingExpires is how long a waiting record of AskFirst stays. A record
	// no operator decided about is removed after it, so a machine that filled
	// the waiting list cannot keep a real node out for good. A record an
	// operator approved never expires. Zero or less means
	// joinwire.DefaultPendingExpiry.
	PendingExpires time.Duration

	// PollInterval is how often this node asks its NATS server which leafnode
	// connections are open. Zero or less means DefaultPollInterval.
	PollInterval time.Duration

	// Extensions is the join data of the protocol extensions this node offers
	// the nodes below it, keyed by the name of the extension. The node puts
	// it in every grant it issues.
	Extensions map[fractal.Identifier]json.RawMessage

	// JoinExtensions names the protocol extensions this node runs. The node
	// keeps their join data from the grant of the node above and drops the
	// rest.
	JoinExtensions []fractal.Identifier

	// CheckExtensions receives the join data kept from a grant before the
	// node accepts it. An error refuses the grant: the node saves nothing and
	// does not bring up the connection. Nil accepts every grant.
	CheckExtensions func(map[fractal.Identifier]json.RawMessage) error

	// Logger receives the node's log records. Nil means the node logs nothing.
	Logger *slog.Logger

	// OnChange receives the new Status after each change to it: a join, a
	// denied join, a lost node, a connection that opens or closes, and a
	// change to the list of waiting join requests. It may be nil.
	OnChange func(s Status)
}

// Validate returns an error for a Config a node cannot run with. It does not
// fill in defaults.
func (c Config) Validate() error {
	if err := c.NodeID.Validate(); err != nil {
		return fmt.Errorf("node id: %w", err)
	}
	switch {
	case c.DataDir == "":
		return ErrNoDataDir
	case c.DedupWindow <= 0:
		return ErrNoDedupWindow
	case c.AcceptsChildren && c.AdvertisedLeafRemote == "":
		return ErrNoLeafRemote
	case c.Verification != "" && !c.Verification.Valid():
		return fmt.Errorf("%q: %w", c.Verification, ErrBadMode)
	case c.AcceptsChildren && c.Verification == ModeJWT && c.KeySource == nil:
		return ErrNoKeySource
	case c.LivenessMultiple != 0 && c.LivenessMultiple < 2:
		return fmt.Errorf("liveness multiple %d: %w", c.LivenessMultiple, ErrBadHeartbeat)
	case c.HeartbeatInterval > 0 && c.MaxHeartbeatInterval > 0 &&
		c.HeartbeatInterval > c.MaxHeartbeatInterval:
		return fmt.Errorf("%s over %s: %w", c.HeartbeatInterval, c.MaxHeartbeatInterval, ErrBadHeartbeat)
	}
	if c.TLS != nil && (c.TLS.CertFile == "") != (c.TLS.KeyFile == "") {
		return errors.New("tls material: a certificate needs its key and a key needs its certificate")
	}
	if err := c.Shadow.Selection.Validate(); err != nil {
		return fmt.Errorf("shadow selection: %w", err)
	}
	return nil
}

// withDefaults returns a copy of the configuration with a default in every
// field that was zero.
func (c Config) withDefaults() Config {
	if c.ClientListen == "" {
		c.ClientListen = DefaultClientListen
	}
	if c.Verification == "" {
		c.Verification = ModeTOFU
	}
	if c.CredentialLifetime <= 0 {
		c.CredentialLifetime = DefaultCredentialLifetime
	}
	if c.AckWait <= 0 {
		c.AckWait = DefaultAckWait
	}
	if c.StreamMaxAge <= 0 {
		c.StreamMaxAge = DefaultStreamMaxAge
	}
	if c.StreamMaxBytes <= 0 {
		c.StreamMaxBytes = DefaultStreamMaxBytes
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if c.MaxHeartbeatInterval <= 0 {
		c.MaxHeartbeatInterval = DefaultMaxHeartbeatInterval
	}
	if c.LivenessMultiple <= 0 {
		c.LivenessMultiple = DefaultLivenessMultiple
	}
	if c.MaxPayload <= 0 {
		c.MaxPayload = DefaultMaxPayload
	}
	if c.MaxJoinsInFlight <= 0 {
		c.MaxJoinsInFlight = DefaultMaxJoinsInFlight
	}
	if c.JoinsPerSecond <= 0 {
		c.JoinsPerSecond = DefaultJoinsPerSecond
	}
	if c.MaxPendingNodes <= 0 {
		c.MaxPendingNodes = joinwire.DefaultMaxPending
	}
	if c.PendingExpires <= 0 {
		c.PendingExpires = joinwire.DefaultPendingExpiry
	}
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.DiscardHandler)
	}
	return c
}

// livenessTimeout is how long a node waits for a heartbeat from a node below
// it before it treats that node as lost. It is LivenessMultiple times
// MaxHeartbeatInterval.
func (c Config) livenessTimeout() time.Duration {
	return time.Duration(c.LivenessMultiple) * c.MaxHeartbeatInterval
}
