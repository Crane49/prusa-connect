package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// listen binds every listener this core serves on, serves them, and drains them
// when ctx ends. The API listener is always one of them. Beside it is a socket
// for every mounted dialect that asked for one (see xbuddy.Listener for the
// reason), and the metrics socket core.observability.addr asks for. A
// deployment that declares neither serves everything on the API listener, and
// that is the default.
//
// httpapi.Server.Handler is the whole origin, and DeviceHandler and
// MetricsHandler are routers of the same sealed package built from the same
// Config. Nothing is mounted beside any of them. chi.Walk cannot enumerate a
// route added outside the sealed package, so the OpenAPI document would not
// publish it. TestNothingOutsideTheSealedPackageMountsAServeMux refuses such a
// route. The routes a printer dials go in through httpapi.Config.Mounts.
//
// Binding happens before anything is announced and before the group starts, so
// a port already in use is an error Run returns and not a goroutine that dies
// alone.
func listen(
	ctx context.Context, core config.Core, devices []deviceListener,
	logger *slog.Logger, api *httpapi.Server, probes readiness, nearby *localNetwork,
	bound func(),
) error {
	sockets, err := bind(ctx, core, devices, logger, api)
	if err != nil {
		return err
	}

	// Every port is open, so a start that got this far will serve. bound runs
	// here and not earlier, so a start whose port cannot be opened can still
	// roll the wizard's answers back. The setup code is printed here too, so a
	// copy that cannot serve prints none.
	if bound != nil {
		bound()
	}

	// The announcement names a port that is open, so it goes out after bind.
	// A failure to announce is a warning and does not refuse the start. The
	// listener starts with it and reads the same port, so this server does not
	// list itself.
	nearby.start(ctx, apiPort(sockets))

	group, groupCtx := errgroup.WithContext(ctx)

	for _, one := range sockets {
		group.Go(func() error { return accept(one) })
	}

	group.Go(func() error { return probes.watch(groupCtx, logger) })
	group.Go(func() error {
		<-groupCtx.Done()

		// The announcement is withdrawn before the listeners drain. A record
		// that outlives its port sends the next client to a closed socket.
		nearby.stop(ctx)

		return drain(ctx, logger, core.ShutdownTimeout, sockets)
	})

	return group.Wait()
}

// apiSocket is what the API listener is called in a log line and in the socket
// list. The announcement reads the same name to find the port it carries.
const apiSocket = "api"

// apiPort is the port the API listener was granted.
//
// It is read off the bound socket and not off core.api.addr, because ":0" is a
// valid value there. A deployment that asked the operating system for a port
// has to announce the port it got.
func apiPort(bound []socket) int {
	for _, one := range bound {
		if one.name != apiSocket {
			continue
		}

		if addr, ok := one.listener.Addr().(*net.TCPAddr); ok {
			return addr.Port
		}
	}

	return 0
}

// socket is one bound listener and the server that answers on it.
type socket struct {
	name     string
	server   *http.Server
	listener net.Listener
}

// bind takes every port this core serves on before anything is announced. A
// failure on the second closes the first, so a refused start leaves nothing
// listening.
func bind(
	ctx context.Context, core config.Core, devices []deviceListener,
	logger *slog.Logger, api *httpapi.Server,
) ([]socket, error) {
	apiCert, err := apiTLS(ctx, logger, core.API.TLS)
	if err != nil {
		return nil, err
	}

	bound, err := open(ctx, apiSocket, api.HTTPServer(core.API.Addr), core.API.Addr, apiCert)
	if err != nil {
		return nil, err
	}

	sockets := []socket{bound}

	serving := []any{"api", bound.listener.Addr().String()}

	// A build that leaves the reference page out does not advertise it.
	if page, ok := httpapi.ReferencePage(); ok {
		serving = append(serving, "reference", reference(apiCert, bound.listener, page))
	}

	serving = append(serving,
		"openapi", reference(apiCert, bound.listener, "/openapi.json"),
		"health", reference(apiCert, bound.listener, "/api/v1/health"))

	logger.InfoContext(ctx, "serving", serving...)

	if len(devices) == 0 {
		logger.InfoContext(ctx, "no connector asked for a listener of its own, so devices dial the "+
			"API listener", "routes", api.DeviceRoutes())
	}

	for _, asked := range devices {
		bound, err = bindDevices(ctx, core, asked, logger, api)
		if err != nil {
			closeAll(sockets)

			return nil, err
		}

		sockets = append(sockets, bound)
	}

	if core.Observability.Addr == "" {
		return sockets, nil
	}

	bound, err = bindMetrics(ctx, core, devices, logger, api)
	if err != nil {
		closeAll(sockets)

		return nil, err
	}

	return append(sockets, bound), nil
}

// bindDevices takes the socket one dialect asked for.
//
// Every one of them serves Server.DeviceHandler, which is the device routes of
// the whole origin and not one connector's. A printer on a device socket
// reaches the routes its own dialect mounted. A path another dialect mounted
// refuses it, because the printer has no credential for that path.
func bindDevices(
	ctx context.Context, core config.Core, asked deviceListener,
	logger *slog.Logger, api *httpapi.Server,
) (socket, error) {
	if err := listenersAreDistinct(core.API.Addr, asked); err != nil {
		return socket{}, err
	}

	server := api.DeviceServer(asked.spec.Addr, asked.spec.HandshakeTimeout)

	bound, err := open(ctx, "devices", server, asked.spec.Addr, asked.spec.TLS)
	if err != nil {
		return socket{}, err
	}

	logger.InfoContext(ctx, "serving devices",
		"connector", asked.connector.String(),
		"addr", bound.listener.Addr().String(),
		"tls", asked.spec.TLS != nil,
		"handshake_timeout", asked.spec.HandshakeTimeout.String(),
		"routes", api.DeviceRoutes())

	return bound, nil
}

// listenersAreDistinct refuses two listeners that would contend for one socket.
//
// Whether the second bind fails depends on the platform. Linux refuses every
// overlap. Windows lets both sockets bind, and then the machine's LAN address
// answers the whole API without TLS on the port the device listener was given
// a certificate for. The two listeners face different networks, so an address
// that could serve both is refused before either is opened.
func listenersAreDistinct(api string, asked deviceListener) error {
	if !config.SameSocket(api, asked.spec.Addr) {
		return nil
	}

	return fmt.Errorf("%w: core.api.addr is %q and the %s connector's listener.addr is %q, which are "+
		"one socket. The two listeners serve different networks and cannot share a port",
		config.ErrInvalid, api, asked.connector.String(), asked.spec.Addr)
}

// bindMetrics takes the socket core.observability.addr asks for.
//
// It serves the Prometheus exposition to whoever reaches it, and the API
// listener serves none while it is bound. An operator publishes this address
// to a collector, and it asks for no credential: the network is the boundary.
// See httpapi.metricsRouter for the reason.
//
// It is bound last of the three, so a metrics address that collides with
// another listener is checked against every listener of the deployment.
func bindMetrics(
	ctx context.Context, core config.Core, devices []deviceListener,
	logger *slog.Logger, api *httpapi.Server,
) (socket, error) {
	addr := core.Observability.Addr
	if err := metricsListenerIsDistinct(core, devices); err != nil {
		return socket{}, err
	}

	bound, err := open(ctx, "metrics", api.MetricsServer(addr), addr, nil)
	if err != nil {
		return socket{}, err
	}

	logger.InfoContext(ctx, "serving metrics",
		"addr", bound.listener.Addr().String(),
		"path", "/metrics",
		"credential", false)

	return bound, nil
}

// metricsListenerIsDistinct refuses a metrics address another listener already
// answers on.
//
// A shared socket would publish the exposition wherever that other listener is
// published. A browser and often an ingress reach the API listener, and every
// printer that dials in reaches a device listener. Neither is a network that
// only a collector sits on.
func metricsListenerIsDistinct(core config.Core, devices []deviceListener) error {
	addr := core.Observability.Addr

	if config.SameSocket(core.API.Addr, addr) {
		return fmt.Errorf("%w: core.observability.addr is %q and core.api.addr is %q, which are one "+
			"socket. The metrics listener asks for no credential and the API listener does, so the "+
			"two cannot share a port", config.ErrInvalid, addr, core.API.Addr)
	}

	for _, asked := range devices {
		if config.SameSocket(asked.spec.Addr, addr) {
			return fmt.Errorf("%w: core.observability.addr is %q and the %s connector's listener.addr "+
				"is %q, which are one socket. The two listeners serve different networks and cannot "+
				"share a port", config.ErrInvalid, addr, asked.connector.String(), asked.spec.Addr)
		}
	}

	return nil
}

// open binds one address and wraps it in TLS when a certificate was configured.
//
// The listener is wrapped, and the server is not told to load a keypair. So
// the two paths differ only by the handshake: everything above it, including
// the drain, is one code path whether or not TLS is on.
func open(
	ctx context.Context, name string, server *http.Server, addr string, certificate *tls.Config,
) (socket, error) {
	// ListenConfig and not net.Listen: the bind honours the caller's context,
	// so a shutdown that arrives while the port is being taken does not have
	// to wait for the resolver.
	var binder net.ListenConfig

	listener, err := binder.Listen(ctx, "tcp", addr)
	if err != nil {
		return socket{}, fmt.Errorf("bind the %s listener on %s: %w", name, addr, err)
	}

	if certificate != nil {
		listener = tls.NewListener(listener, certificate)
	}

	return socket{name: name, server: server, listener: listener}, nil
}

// reference is a URL an operator can paste, on whichever scheme the listener
// answers.
func reference(certificate *tls.Config, listener net.Listener, path string) string {
	scheme := "http://"
	if certificate != nil {
		scheme = "https://"
	}

	return scheme + listener.Addr().String() + path
}

// accept serves one listener until it is shut down. A closed server is how a
// shutdown ends, not a failure to report.
func accept(one socket) error {
	if err := one.server.Serve(one.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve the %s listener: %w", one.name, err)
	}

	return nil
}

// drain stops every listener within one budget, in parallel, because the
// budget is for the whole shutdown and not for each socket. It runs before the
// supervision plane is cancelled, so an in-flight write finishes against
// instances that are still up.
//
// The budget runs on a detached context because the signal that started the
// shutdown has already cancelled the caller's.
func drain(ctx context.Context, logger *slog.Logger, budget time.Duration, sockets []socket) error {
	logger.InfoContext(ctx, "draining the listeners", "budget", budget.String())

	stopping, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancel()

	group := errgroup.Group{}

	for _, one := range sockets {
		group.Go(func() error {
			if err := one.server.Shutdown(stopping); err != nil {
				return fmt.Errorf("drain the %s listener: %w", one.name, err)
			}

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return err
	}

	logger.InfoContext(ctx, "listeners drained")

	return nil
}

// closeAll gives back every port already taken when a later bind fails, so a
// refused start leaves nothing listening.
func closeAll(sockets []socket) {
	for _, one := range sockets {
		_ = one.listener.Close()
	}
}
