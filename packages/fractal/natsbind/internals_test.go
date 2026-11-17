package natsbind

import (
	"crypto/tls"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
)

// testLifetime is the value the tests in this file use as the credential
// lifetime and as Config.AckWait.
const testLifetime = 30 * time.Second

// TestTheLinkPermissionsCarryTheDirection checks the permissions on
// "fractal.up.>" and "fractal.down.>". The node below may publish only upward
// and subscribe only downward, and this node's connection in the link account
// has the opposite (fractal-nats.md, Control scoping).
func TestTheLinkPermissionsCarryTheDirection(t *testing.T) {
	t.Parallel()
	leaf := linkLeaf("printer-1", "hq", "hq_printer-1")
	if !slices.Contains(leaf.Pub.Allow, "fractal.up.>") {
		t.Error("linkLeaf does not allow the node below to publish on fractal.up.>")
	}
	if !slices.Contains(leaf.Sub.Allow, "fractal.down.>") {
		t.Error("linkLeaf does not allow the node below to subscribe to fractal.down.>")
	}
	if slices.Contains(leaf.Pub.Allow, "fractal.down.>") {
		t.Error("linkLeaf allows the node below to publish on fractal.down.>")
	}
	if slices.Contains(leaf.Sub.Allow, "fractal.up.>") {
		t.Error("linkLeaf allows the node below to subscribe to fractal.up.>")
	}

	client := linkClient("printer-1", "hq_printer-1")
	if !slices.Contains(client.Pub.Allow, "fractal.down.>") {
		t.Error("linkClient does not allow this node to publish on fractal.down.>")
	}
	if !slices.Contains(client.Sub.Allow, "fractal.up.>") {
		t.Error("linkClient does not allow this node to subscribe to fractal.up.>")
	}
	if slices.Contains(client.Pub.Allow, "fractal.up.>") {
		t.Error("linkClient allows this node to publish on fractal.up.>")
	}
	// Only this node uses the stream API of its own domain. The node below
	// must not have it.
	if slices.Contains(leaf.Pub.Allow, "$JS.API.STREAM.>") {
		t.Error("linkLeaf allows the node below to use the stream API of this node")
	}
}

// TestTheBootstrapRolesAreTheOnesInTheSpecification checks the permissions of
// the joining-node user and of the join-service user in the bootstrap account.
// The joining-node user must not be allowed to publish on a reply subject
// (fractal-nats.md, Bootstrap Account and Join Endpoint).
func TestTheBootstrapRolesAreTheOnesInTheSpecification(t *testing.T) {
	t.Parallel()
	joining := bootstrapJoining()
	if !slices.Equal([]string(joining.Pub.Allow), []string{joinwire.SubjectJoin}) {
		t.Errorf("the joining-node user may publish on %v, want only %s", joining.Pub.Allow, joinwire.SubjectJoin)
	}
	if !slices.Equal([]string(joining.Sub.Allow), []string{inbox}) {
		t.Errorf("the joining-node user may subscribe to %v, want only %s", joining.Sub.Allow, inbox)
	}
	service := bootstrapService()
	if !slices.Equal([]string(service.Sub.Allow), []string{joinwire.SubjectJoin}) {
		t.Errorf("the join-service user may subscribe to %v, want only %s", service.Sub.Allow, joinwire.SubjectJoin)
	}
	if !slices.Equal([]string(service.Pub.Allow), []string{inbox}) {
		t.Errorf("the join-service user may publish on %v, want only %s", service.Pub.Allow, inbox)
	}
}

// TestTheSentinelIsRestrictedToClientConnections checks that the sentinel, the
// user JWT for a client with no credentials, allows standard client
// connections only. The server uses the sentinel for every kind of connection
// with no credentials, so without the restriction a leafnode connection with
// no credentials would enter the bootstrap account (fractal-nats.md, Bootstrap
// Account and Join Endpoint).
func TestTheSentinelIsRestrictedToClientConnections(t *testing.T) {
	t.Parallel()
	held, err := newAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("build the accounts: %v", err)
	}
	if mintErr := held.mint(accountBootstrap, accountSpec{}); mintErr != nil {
		t.Fatalf("mint the bootstrap account: %v", mintErr)
	}
	token, err := held.issueSentinel(accountBootstrap, bootstrapJoining())
	if err != nil {
		t.Fatalf("issue the sentinel: %v", err)
	}
	claims, err := jwt.DecodeUserClaims(token)
	if err != nil {
		t.Fatalf("decode the sentinel: %v", err)
	}
	if !slices.Equal([]string(claims.AllowedConnectionTypes), []string{jwt.ConnectionTypeStandard}) {
		t.Errorf("the sentinel allows %v, want only a standard client connection", claims.AllowedConnectionTypes)
	}
	if !claims.BearerToken {
		t.Error("the sentinel is not a bearer token, so a client with no key cannot use it")
	}
}

// TestTheConnectionCredentialsAreLeafOnlyAndExpire checks the connection
// credentials a node issues to a node that joined. Their subject is the public
// key of that node, they expire, they allow leafnode connections only, and
// they are not a bearer token (fractal-nats.md, Provisioning the Link).
func TestTheConnectionCredentialsAreLeafOnlyAndExpire(t *testing.T) {
	t.Parallel()
	held, err := newAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("build the accounts: %v", err)
	}
	account := linkAccount("printer-1")
	if mintErr := held.mint(account, accountSpec{jetStream: true}); mintErr != nil {
		t.Fatalf("mint the account of the link: %v", mintErr)
	}
	pair, err := nkeys.CreateUser()
	if err != nil {
		t.Fatalf("create a key: %v", err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		t.Fatalf("read the public key: %v", err)
	}
	token, expires, err := held.issueLeafUser(
		account, pub, linkLeaf("printer-1", "hq", "hq_printer-1"), testLifetime, time.Now())
	if err != nil {
		t.Fatalf("issueLeafUser: %v", err)
	}
	claims, err := jwt.DecodeUserClaims(token)
	if err != nil {
		t.Fatalf("decode the credentials: %v", err)
	}
	if claims.Subject != pub {
		t.Errorf("the subject of the credentials is %q, want the public key %q", claims.Subject, pub)
	}
	if claims.Expires == 0 || claims.Expires != expires.Unix() {
		t.Errorf("the credentials expire at %d, want %d", claims.Expires, expires.Unix())
	}
	if !slices.Equal([]string(claims.AllowedConnectionTypes), []string{jwt.ConnectionTypeLeafnode}) {
		t.Errorf("the credentials allow %v, want only a leafnode connection", claims.AllowedConnectionTypes)
	}
	if claims.BearerToken {
		t.Error("the credentials are a bearer token, so they work without the private key")
	}
}

// TestAnAccountThatWasNotMintedIssuesNothing checks that issueUser returns an
// error for an account that mint has not signed. The server could not find
// that account, so it would refuse the credential.
func TestAnAccountThatWasNotMintedIssuesNothing(t *testing.T) {
	t.Parallel()
	held, err := newAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("build the accounts: %v", err)
	}
	if _, pubErr := held.accountPub("never-minted"); pubErr != nil {
		t.Fatalf("read the account key: %v", pubErr)
	}
	if _, issueErr := held.issueUser("never-minted", "UAAAA", jwt.UserPermissionLimits{}); issueErr == nil {
		t.Error("issueUser issued a credential for an account that was never minted")
	}
}

// TestTheConsumersTakeOneMessageAtATime checks that a consumer has
// MaxAckPending 1. fractal-nats.md fixes that value, and Config has no key for
// it (fractal-nats.md, Configuration Parameters).
func TestTheConsumersTakeOneMessageAtATime(t *testing.T) {
	t.Parallel()
	b := &Binding{cfg: Config{AckWait: testLifetime}}
	cfg := b.consumerConfig("fractal-rpc-printer-1", SubjectRPC("printer-1"))
	if cfg.MaxAckPending != 1 {
		t.Errorf("MaxAckPending is %d, want 1", cfg.MaxAckPending)
	}
	if cfg.AckPolicy != nats.AckExplicitPolicy {
		t.Errorf("AckPolicy is %v, want an acknowledgement for each message", cfg.AckPolicy)
	}
	if cfg.Durable == "" {
		t.Error("the consumer is not durable, so a restart would lose its position in the stream")
	}
	if cfg.AckWait != testLifetime {
		t.Errorf("AckWait is %s, want Config.AckWait %s", cfg.AckWait, testLifetime)
	}
}

// TestTheListenersAreTls13AndAskForNoCertificate checks that a listener and an
// outgoing connection require TLS 1.3, that a listener asks for no client
// certificate, and that an outgoing connection verifies the server
// (fractal-nats.md, Security).
func TestTheListenersAreTls13AndAskForNoCertificate(t *testing.T) {
	t.Parallel()
	material := &TLSMaterial{CertFile: "node.pem", KeyFile: "node.key"}
	listener := serverTLS(material)
	if listener.MinVersion != tls.VersionTLS13 {
		t.Errorf("the listener's MinVersion is %x, want TLS 1.3", listener.MinVersion)
	}
	if listener.ClientAuth != tls.NoClientCert {
		t.Error("the listener asks the client for a certificate")
	}
	if listener.GetCertificate == nil {
		t.Error("the listener has no GetCertificate, so it cannot read the certificate at every handshake")
	}
	if _, err := listener.GetCertificate(&tls.ClientHelloInfo{}); err == nil {
		t.Error("GetCertificate returned a certificate from files that do not exist")
	}
	if serverTLS(nil) != nil {
		t.Error("serverTLS returned a configuration for a nil TLSMaterial")
	}
	dial := ClientTLS(nil, "hq.example")
	if dial.MinVersion != tls.VersionTLS13 {
		t.Errorf("the client's MinVersion is %x, want TLS 1.3", dial.MinVersion)
	}
	if dial.InsecureSkipVerify {
		t.Error("the client does not verify the server's certificate")
	}
}

// TestTheStoreOfANodeCarriesItsDomain checks that the store directory has the
// JetStream domain in it. A node that receives a different Node Path then
// starts with an empty store and creates its streams again (fractal-nats.md,
// Provisioning the Link).
func TestTheStoreOfANodeCarriesItsDomain(t *testing.T) {
	t.Parallel()
	first := storeDir("data", "hq/brno")
	second := storeDir("data", "hq/praha")
	if first == second {
		t.Errorf("hq/brno and hq/praha have the same store directory %q", first)
	}
	if !strings.Contains(first, Domain("hq/brno")) {
		t.Errorf("the store directory %q does not have the domain in it", first)
	}
}

// TestALeafAddressFollowsTheClientOne checks the address of the leafnode
// listener when Config.LeafListen is empty.
func TestALeafAddressFollowsTheClientOne(t *testing.T) {
	t.Parallel()
	host, port, err := leafAddr(Config{}, "127.0.0.1", 4222)
	if err != nil {
		t.Fatalf("leafAddr: %v", err)
	}
	if host != "127.0.0.1" || port != 4222+DefaultLeafPortOffset {
		t.Errorf("the leafnode listener is at %s:%d, want the client port plus DefaultLeafPortOffset", host, port)
	}
	if _, _, named := leafAddr(Config{LeafListen: "0.0.0.0:7422"}, "127.0.0.1", 4222); named != nil {
		t.Errorf("leafAddr refused the LeafListen address: %v", named)
	}
	if _, _, bad := splitAddr("nonsense"); bad == nil {
		t.Error("splitAddr accepted an address with no port")
	}
}

// TestTheServerNameCarriesThePath checks that the server name is the Node Path
// with hyphens in place of slashes.
func TestTheServerNameCarriesThePath(t *testing.T) {
	t.Parallel()
	if got := serverName("hq/brno/printer-1"); got != "hq-brno-printer-1" {
		t.Errorf("the server name is %q, want hq-brno-printer-1", got)
	}
	if got := serverName(""); got == "" {
		t.Error("serverName returned an empty name for an empty Node Path")
	}
}

// TestASilentNodeIsLostOnceAndStaysLost checks the node below whose socket
// stays open while its heartbeats stop. The poll of the leafnode connections
// still sees that socket, so without the silence flag the node would be
// reported connected and lost again on every poll. It must be reported lost
// once and stay lost until a message from it arrives (fractal-nats.md,
// Connectivity Detection).
func TestASilentNodeIsLostOnceAndStaysLost(t *testing.T) {
	t.Parallel()
	engine := fractaltest.NewRecorder(true)
	b := &Binding{
		cfg: Config{
			LivenessMultiple:     2,
			MaxHeartbeatInterval: time.Second,
			DedupWindow:          testLifetime,
		},
		engine: engine,
		log:    slog.New(slog.DiscardHandler),
		links:  map[fractal.NodeID]*link{},
	}
	l := &link{id: "printer-1", path: "hq/printer-1", admission: AdmissionAdmitted, ever: true}
	b.links[l.id] = l
	ctx := t.Context()

	b.markConnected(ctx, l)
	l.touch(time.Now())
	if got := engine.Connected(); len(got) != 1 {
		t.Fatalf("the engine saw %v connections, want one", got)
	}

	// The node below has sent no heartbeat for longer than the liveness timeout and
	// its socket is still open. Three rounds of the two watchers follow.
	l.mu.Lock()
	l.lastHB = time.Now().Add(-time.Hour)
	l.mu.Unlock()
	for range 3 {
		b.markSilentLinks(ctx)
		b.markConnected(ctx, l) // What pollLeaves does while the socket is open.
	}
	if got := engine.Lost(); len(got) != 1 {
		t.Errorf("the engine saw %d losses, want one", len(got))
	}
	if got := engine.Connected(); len(got) != 1 {
		t.Errorf("the engine saw %d connections, want the one before the silence", len(got))
	}

	// A heartbeat from the node below ends the silence, and the next poll
	// reports it connected again.
	l.touch(time.Now())
	b.markConnected(ctx, l)
	if got := engine.Connected(); len(got) != 2 {
		t.Errorf("the engine saw %d connections, want a second one after the message", len(got))
	}
}
