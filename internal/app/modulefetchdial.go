package app

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// Where a release download may connect. The address is checked after the name
// is resolved, at the moment of the dial. A name that resolves to this machine
// or to the local network is refused however it is spelled. Every redirect
// dials through the same hook, so it is held to the same check.

// The bounds one dial runs under, the ones Go's default transport uses.
const (
	downloadDialTimeout = 30 * time.Second
	downloadKeepAlive   = 30 * time.Second
	downloadTLSTimeout  = 10 * time.Second
	downloadIdleTimeout = 90 * time.Second
)

// errAddressRefused is a download that would connect to this machine, the
// local network, or an address that is not one host.
var errAddressRefused = errors.New("a release is not downloaded from this machine or the local network")

// downloadClient is the client a release is downloaded with. It reads no
// proxy from the environment, because a proxy is a dial to the local network
// that would carry the request past the address check.
func downloadClient(market config.Marketplace) *http.Client {
	dialer := &net.Dialer{
		Timeout:   downloadDialTimeout,
		KeepAlive: downloadKeepAlive,
		Control:   dialGuard(localMarketplace(market)),
	}

	return &http.Client{
		Timeout:       fetchTimeout,
		CheckRedirect: checkRemoteRedirect,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			ForceAttemptHTTP2:   true,
			TLSHandshakeTimeout: downloadTLSTimeout,
			IdleConnTimeout:     downloadIdleTimeout,
		},
	}
}

// localMarketplace reports a marketplace configured on this machine over http,
// which is local development. Its releases are then served from this machine
// too, by the storage the marketplace runs beside it on another port, so a
// download may reach loopback. Every other marketplace keeps loopback shut.
func localMarketplace(market config.Marketplace) bool {
	if !market.Configured() {
		return false
	}

	base, err := url.Parse(market.Base())

	return err == nil && base.Scheme == "http" && config.IsLoopbackHost(base.Hostname())
}

// dialGuard refuses a connection to any address that is not a public one. With
// a local marketplace, loopback is allowed too, and the local network still
// is not.
func dialGuard(allowLoopback bool) func(network, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: %w", errAddressRefused, err)
		}

		ip, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("%w: %w", errAddressRefused, err)
		}

		ip = ip.Unmap()

		if ip.IsLoopback() && allowLoopback {
			return nil
		}

		if !publicAddress(ip) {
			return fmt.Errorf("%w: %s", errAddressRefused, ip)
		}

		return nil
	}
}

// publicAddress reports an address of one host somewhere else: not this
// machine, not the local network, not link-local, and not a group.
func publicAddress(ip netip.Addr) bool {
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() && !ip.IsMulticast() &&
		!ip.IsUnspecified()
}
