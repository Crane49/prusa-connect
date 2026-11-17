package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	pfiles "github.com/prusa3d/fractal-link/files"
	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

const (
	// cacheFolder is the folder of the data directory the core prints
	// module:data from.
	cacheFolder = "print"
	// readHeaderTimeout bounds how long a caller may take to send its headers.
	readHeaderTimeout = 10 * time.Second
)

// filesPart is the file distribution extension of a running node. It is nil
// when the settings have no files block.
type filesPart struct {
	files    *pfiles.Files
	cache    *pfiles.Cache
	client   *pfiles.Client
	server   *http.Server
	listener net.Listener
	deadline time.Duration
}

// buildFiles builds the cache, the client, the server of /objects/ and the
// facade the bridge prints through, and opens the port files.
func buildFiles(
	ctx context.Context,
	cfg Config,
	binding *natsbind.Binding,
	publisher *pfiles.Publisher,
	st pfiles.Storage,
	log *slog.Logger,
) (*filesPart, error) {
	settings := cfg.Files

	base, err := url.Parse(settings.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("read files.base_url: %w", err)
	}

	cache, err := pfiles.OpenCache(filepath.Join(cfg.Grants.Data, cacheFolder),
		orDefault(settings.CacheMaxBytes, DefaultCacheMaxBytes), log)
	if err != nil {
		return nil, fmt.Errorf("open the copies of files from above: %w", err)
	}

	// A start can come up on a saved Node Path before any grant, so the
	// copies follow that path before the node connects.
	if path := binding.Path(); path != "" {
		if err = cache.Follow(path); err != nil {
			return nil, fmt.Errorf("drop the copies of another node path: %w", err)
		}
	}

	var caFiles []string
	if cfg.TLS != nil {
		caFiles = cfg.TLS.CAFiles
	}

	anchors, err := natsbind.LoadAnchors(caFiles)
	if err != nil {
		return nil, fmt.Errorf("read the trust anchors of the files client: %w", err)
	}

	relayTimeout := orDefault(settings.RelayTimeout, DefaultRelayTimeout)

	client := pfiles.NewClient(pfiles.ClientConfig{
		Signer: binding.Signer(), Anchors: anchors, AllowCleartext: settings.AllowCleartextParent,
		MaxResumes: settings.MaxResumes, Timeout: relayTimeout,
		Rejoin: binding.Join, Tolerance: settings.ProofTolerance, Log: log,
	})

	parent := func() (string, bool) {
		raw, ok := binding.ParentExtension(sdkfiles.ExtensionName)
		if !ok {
			return "", false
		}

		entry, entryErr := sdkfiles.ParseJoinEntry(raw, settings.AllowCleartextParent)
		if entryErr != nil {
			return "", false
		}

		return entry.HTTP.BaseURL, true
	}

	server := pfiles.NewServer(pfiles.ServerConfig{
		Publisher: publisher, Storage: st, Cache: cache,
		Children: func(id fractal.Identity) bool {
			_, ok := binding.ChildByIdentity(id)

			return ok
		},
		Parent: parent, Client: client, BasePath: base.Path,
		Tolerance: settings.ProofTolerance, WriteTimeout: relayTimeout, Log: log,
	})

	// The files server reads the raw request target, so its handler is mounted
	// with no ServeMux in front.
	served := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       relayTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// The scheme of base_url decides, because every grant tells it to the
	// children. checkGrants has refused https without a certificate.
	if base.Scheme == "https" && cfg.TLS != nil {
		served.TLSConfig, err = natsbind.ServerTLS(
			natsbind.TLSMaterial{CertFile: cfg.TLS.CertFile, KeyFile: cfg.TLS.KeyFile},
		)
		if err != nil {
			return nil, fmt.Errorf("serve the files over https: %w", err)
		}
	}

	var listen net.ListenConfig

	listener, err := listen.Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(cfg.Grants.Listen[filesPort].Port)))
	if err != nil {
		return nil, fmt.Errorf("listen on the port %s: %w", filesPort, err)
	}

	return &filesPart{
		files: pfiles.NewFiles(pfiles.FilesConfig{
			Publisher: publisher, Storage: st, Cache: cache, Client: client,
			Parent: parent, Attempts: settings.FetchAttempts,
		}),
		cache: cache, client: client, server: served, listener: listener, deadline: settings.StartDeadline,
	}, nil
}

// serve answers /objects/ until ctx ends.
func (p *filesPart) serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = p.server.Close() // Serve then returns, and reports how it ended
		p.client.CloseIdleConnections()
	}()

	var err error
	if p.server.TLSConfig != nil {
		err = p.server.ServeTLS(p.listener, "", "")
	} else {
		err = p.server.Serve(p.listener)
	}

	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve /objects/: %w", err)
}

// bridgeFiles returns what the bridge prints through, and a nil interface
// when the extension is off, so the bridge refuses a fractal URL.
func (p *filesPart) bridgeFiles() bridge.Files {
	if p == nil {
		return nil
	}

	return p.files
}

func (p *filesPart) startDeadline() time.Duration {
	if p == nil {
		return 0
	}

	return p.deadline
}
