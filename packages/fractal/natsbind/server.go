package natsbind

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// readyTimeout is how long startServer waits for the embedded NATS server to
// accept connections.
const readyTimeout = 30 * time.Second

// loopbackHost is the address a node that accepts nobody binds its client
// listener to. Only this process reaches it.
const loopbackHost = "127.0.0.1"

// jetStreamDir is the folder in the data directory where JetStream, the part
// of NATS that stores messages on disk, keeps the streams and the key-value
// buckets.
const jetStreamDir = "jetstream"

// ErrServerNotReady means the embedded NATS server did not accept connections
// within readyTimeout.
var ErrServerNotReady = errors.New("the embedded server did not become ready")

// serverParams is the input of startServer.
type serverParams struct {
	// cfg is the node's configuration after withDefaults.
	cfg Config
	// path is this node's Node Path. The JetStream domain is named after it.
	path fractal.NodePath
	// accounts has the operator key and the NATS accounts.
	accounts *accounts
	// sentinel is the user JWT the server uses for a client that connects with
	// no credentials. It is empty on a node that does not accept joins.
	sentinel string
	// leafRemote is the leafnode address of the node above, taken from its
	// answer to the join request. It is empty on a root node. A leafnode
	// connection is the NATS connection from one server to a server above it.
	leafRemote string
	// signLeaf authenticates the leafnode connection to the node above. It
	// returns this node's user JWT and its signature over the nonce, the random
	// value the server above sends.
	signLeaf func(nonce []byte) (string, []byte, error)
	// anchors are the CA certificates this node trusts when it connects to the
	// node above.
	anchors *x509.CertPool
}

// startServer builds and starts the embedded NATS server of one node.
//
// Each node runs one server with JetStream, and the JetStream domain is named
// after the Node Path. When
// p.leafRemote is set, the server opens a leafnode connection to the node
// above. It also returns the options the server runs with, which a reload of
// the server starts from.
func startServer(ctx context.Context, p serverParams) (*server.Server, *server.Options, error) {
	clientHost, clientPort, err := clientAddr(p.cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("client listener: %w", err)
	}
	store := storeDir(p.cfg.DataDir, p.path)
	if mkErr := os.MkdirAll(store, dirMode); mkErr != nil {
		return nil, nil, fmt.Errorf("make %s: %w", store, mkErr)
	}
	opts := &server.Options{
		Host:            clientHost,
		Port:            clientPort,
		ServerName:      serverName(p.path),
		NoLog:           true,
		NoSigs:          true,
		JetStream:       true,
		JetStreamDomain: Domain(p.path),
		StoreDir:        store,
		MaxPayload:      p.cfg.MaxPayload,
		DefaultSentinel: p.sentinel,
	}
	if applyErr := p.accounts.applyTo(opts); applyErr != nil {
		return nil, nil, applyErr
	}
	if p.cfg.TLS.Serves() {
		// Join requests arrive on the client listener from nodes that have no
		// certificate yet, so the listener asks for no client certificate. It
		// accepts no connection without TLS.
		opts.TLSConfig = serverTLS(p.cfg.TLS)
	}
	// A node that accepts nobody opens no leafnode listener. Port zero turns it
	// off in the NATS server. The connection to the node above is one this node
	// opens, in leaf.Remotes, and it needs no listener here.
	var leaf server.LeafNodeOpts
	if p.cfg.AcceptsChildren {
		leafHost, leafPort, leafErr := leafAddr(p.cfg, clientHost, clientPort)
		if leafErr != nil {
			return nil, nil, fmt.Errorf("leafnode listener: %w", leafErr)
		}
		leaf.Host, leaf.Port = leafHost, leafPort
		leaf.Advertise = advertisedLeafAddr(p.cfg.AdvertisedLeafRemote)
		if p.cfg.TLS.Serves() {
			leaf.TLSConfig = serverTLS(p.cfg.TLS)
		}
	}
	if p.leafRemote != "" {
		remote, remoteErr := leafRemoteOpts(p)
		if remoteErr != nil {
			return nil, nil, remoteErr
		}
		leaf.Remotes = []*server.RemoteLeafOpts{remote}
	}
	opts.LeafNode = leaf

	srv, err := server.NewServer(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("build the embedded server: %w", err)
	}
	srv.SetLoggerV2(newServerLogger(p.cfg.Logger), false, false, false)
	go srv.Start()
	if !srv.ReadyForConnections(readyTimeout) {
		srv.Shutdown()
		return nil, nil, ErrServerNotReady
	}
	p.accounts.attachServer(srv)
	if !p.cfg.TLS.Serves() {
		p.cfg.Logger.WarnContext(ctx,
			"tls is off on this node, so its connections are not encrypted and not protected against changes",
			"node", p.path)
	}
	return srv, opts, nil
}

// storeDir returns the JetStream store directory of one node.
//
// The JetStream domain is the last folder of the directory. A node that
// receives a different Node Path therefore starts with an empty store and
// creates its streams and buckets again.
func storeDir(dataDir string, path fractal.NodePath) string {
	return filepath.Join(dataDir, jetStreamDir, Domain(path))
}

// leafRemoteOpts builds the options of the leafnode connection to the node
// above.
//
// The address comes from that node's answer to the join request. This node
// has no configuration key for it, because the node above chooses its leafnode
// address and its client address does not show it.
func leafRemoteOpts(p serverParams) (*server.RemoteLeafOpts, error) {
	parsed, err := url.Parse(p.leafRemote)
	if err != nil {
		return nil, fmt.Errorf("parse the granted leafnode address %q: %w", p.leafRemote, err)
	}
	// In operator mode LocalAccount takes the account's public key. It does
	// not take the account's name.
	localPub, err := p.accounts.accountPub(accountLocal)
	if err != nil {
		return nil, err
	}
	remote := &server.RemoteLeafOpts{
		URLs:         []*url.URL{parsed},
		LocalAccount: localPub,
		SignatureCB:  p.signLeaf,
	}
	if p.cfg.TLS != nil {
		// With an empty server name the NATS server uses the host of the address
		// it connects to. So it checks the certificate of the node above
		// against the host in the leafnode address that node sent.
		remote.TLSConfig = ClientTLS(p.anchors, "")
	}
	return remote, nil
}

// advertisedLeafAddr returns the host and port of the granted leafnode
// address, or "" when it does not parse. The NATS server tells every node
// below its leafnode address, and that node redials it after a drop, so it
// must be the address the node below dials from the grant. Without a port
// that is the default leafnode port, where the server would fill in its
// listen port.
func advertisedLeafAddr(remote string) string {
	advertised, err := url.Parse(remote)
	if err != nil || advertised.Host == "" {
		return ""
	}
	if advertised.Port() != "" {
		return advertised.Host
	}
	return net.JoinHostPort(advertised.Hostname(), strconv.Itoa(server.DEFAULT_LEAFNODE_PORT))
}

// serverName returns the name of the NATS server: the Node Path with every
// slash replaced by a hyphen, because a server name cannot have a slash.
func serverName(path fractal.NodePath) string {
	name := strings.ReplaceAll(string(path), "/", "-")
	if name == "" {
		return "fractal-node"
	}
	return name
}

// splitAddr splits an address into a host and a port. With port zero the
// server picks a free port.
func splitAddr(addr string) (host string, port int, err error) {
	host, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("parse %q: %w", addr, err)
	}
	port, err = strconv.Atoi(rawPort)
	if err != nil {
		return "", 0, fmt.Errorf("parse the port of %q: %w", addr, err)
	}
	if port == 0 {
		port = server.RANDOM_PORT
	}
	return host, port, nil
}

// clientAddr returns the address of the client listener.
//
// A node that accepts nobody answers no join request, so nothing outside this
// process connects to that listener. It then binds to the loopback address on
// a free port. Two such nodes on one machine would otherwise both take the
// default port and the second one could not start. This node's own connections
// find the port through server.ClientURL.
func clientAddr(cfg Config) (host string, port int, err error) {
	if !cfg.AcceptsChildren {
		return loopbackHost, server.RANDOM_PORT, nil
	}
	return splitAddr(cfg.ClientListen)
}

// leafAddr returns the address of the leafnode listener. When LeafListen is
// empty it is the host of the client listener and the client port plus
// DefaultLeafPortOffset, or a free port when the client port is a free one.
func leafAddr(cfg Config, clientHost string, clientPort int) (host string, port int, err error) {
	if cfg.LeafListen != "" {
		return splitAddr(cfg.LeafListen)
	}
	if clientPort <= 0 {
		return clientHost, server.RANDOM_PORT, nil
	}
	return clientHost, clientPort + DefaultLeafPortOffset, nil
}

// listeners returns the addresses the NATS server listens on. Status shows
// them to an operator.
func listeners(srv *server.Server) []Listener {
	varz, err := srv.Varz(nil)
	if err != nil || varz == nil {
		return nil
	}
	out := []Listener{{Protocol: "nats", Address: varz.Host, Port: varz.Port}}
	if varz.LeafNode.Port != 0 {
		out = append(out, Listener{Protocol: "nats-leaf", Address: varz.LeafNode.Host, Port: varz.LeafNode.Port})
	}
	return out
}

// serverLogger writes the log lines of the embedded NATS server to the node's
// logger.
type serverLogger struct{ log *slog.Logger }

// newServerLogger returns a serverLogger that writes to log.
func newServerLogger(log *slog.Logger) server.Logger { return &serverLogger{log: log} }

// Noticef logs a notice.
func (l *serverLogger) Noticef(format string, v ...any) {
	l.log.Info(fmt.Sprintf(format, v...))
}

// Warnf logs a warning.
func (l *serverLogger) Warnf(format string, v ...any) {
	l.log.Warn(fmt.Sprintf(format, v...))
}

// Errorf logs an error.
func (l *serverLogger) Errorf(format string, v ...any) {
	l.log.Error(fmt.Sprintf(format, v...))
}

// Fatalf logs a fatal message of the NATS server as an error. It does not end
// the process: the code that owns the Binding decides whether to stop.
func (l *serverLogger) Fatalf(format string, v ...any) {
	l.log.Error(fmt.Sprintf(format, v...))
}

// Debugf logs a debug message.
func (l *serverLogger) Debugf(format string, v ...any) {
	l.log.Debug(fmt.Sprintf(format, v...))
}

// Tracef logs nothing. The server writes one trace line for every message.
func (l *serverLogger) Tracef(string, ...any) {}
