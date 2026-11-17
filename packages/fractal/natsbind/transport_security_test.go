package natsbind_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// settle is how long a test watches for an event that must not happen.
const settle = 2 * time.Second

// TestAnUnauthenticatedLeafConnectionIsRefused starts a second NATS server
// with no credentials and points its leafnode remote at the node. A peer with
// no credentials may send a join request as a client. It must not open a
// leafnode connection (fractal-nats.md, Bootstrap Account and Join Endpoint).
func TestAnUnauthenticatedLeafConnectionIsRefused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, leaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	cfg.AdvertisedLeafRemote = fmt.Sprintf("nats://127.0.0.1:%d", leaf)
	startNode(ctx, t, cfg)

	target, err := url.Parse(fmt.Sprintf("nats://127.0.0.1:%d", leaf))
	if err != nil {
		t.Fatalf("parse the leafnode address: %v", err)
	}
	probe, err := server.NewServer(&server.Options{
		Host:       "127.0.0.1",
		Port:       server.RANDOM_PORT,
		ServerName: "probe",
		NoLog:      true,
		NoSigs:     true,
		LeafNode: server.LeafNodeOpts{
			Host:    "127.0.0.1",
			Port:    server.RANDOM_PORT,
			Remotes: []*server.RemoteLeafOpts{{URLs: []*url.URL{target}}},
		},
	})
	if err != nil {
		t.Fatalf("build the probe: %v", err)
	}
	go probe.Start()
	t.Cleanup(func() {
		probe.Shutdown()
		probe.WaitForShutdown()
	})
	if !probe.ReadyForConnections(testWait) {
		t.Fatal("the probe did not start")
	}
	deadline := time.Now().Add(settle)
	for time.Now().Before(deadline) {
		if got := probe.NumLeafNodes(); got != 0 {
			t.Fatalf("a server with no credentials opened %d leafnode connections, want 0", got)
		}
		time.Sleep(testPoll)
	}
}

// TestALinkRunsOverTls checks two nodes with TLS on. Both listeners of hq
// serve a certificate. The node that joins verifies it with the CA file from
// its configuration. The join request, the leafnode connection and a state
// report all go over TLS (fractal-nats.md, Security).
func TestALinkRunsOverTls(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	material := writeMaterial(t)

	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	aboveCfg.TLS = material
	aboveCfg.AdvertisedLeafRemote = fmt.Sprintf("tls://127.0.0.1:%d", aboveLeaf)
	hq := startNode(ctx, t, aboveCfg)

	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	belowCfg.TLS = &natsbind.TLSMaterial{CAFiles: material.CAFiles}
	below := startNode(ctx, t, belowCfg)

	waitUntil(t, "the link to come up", func() bool {
		return len(hq.engine.Connected()) == 1 && below.binding.Status().Uplink.State == natsbind.UplinkConnected
	})
	if got := below.binding.Path(); got != "hq/printer-1" {
		t.Fatalf("the Path of the node that joined is %q, want hq/printer-1", got)
	}
	send(t, below, fractalnode.Dispatch{
		Kind: fractal.KindREP, Payload: repPayload(t, "hq/printer-1", 1),
	})
	awaitKind(t, hq, fractal.KindREP, 1)
}

// writeMaterial creates a self-signed certificate for 127.0.0.1 and writes the
// certificate and its key to files. The certificate is also the CA file, so
// one node can serve TLS with it and another can verify it.
func writeMaterial(t *testing.T) *natsbind.TLSMaterial {
	t.Helper()
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("create a key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fractal test node"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create a certificate: %v", err)
	}
	certPath := filepath.Join(dir, "node.pem")
	keyPath := filepath.Join(dir, "node.key")
	write(t, certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	write(t, keyPath, pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	return &natsbind.TLSMaterial{CertFile: certPath, KeyFile: keyPath, CAFiles: []string{certPath}}
}

// write writes data to the file at path.
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
