package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// apiTLS is the certificate the API listener presents, or nil for plain HTTP.
//
// TLS 1.3 is the minimum. A browser reaches this listener, and every browser
// in use supports 1.3. No device connects here, so no older version is
// needed.
func apiTLS(ctx context.Context, logger *slog.Logger, cfg config.TLS) (*tls.Config, error) {
	if !cfg.Enabled() {
		return nil, nil //nolint:nilnil // no certificate is not an error: it is plain HTTP.
	}

	pair, err := cfg.Load()
	if err != nil {
		return nil, err
	}

	leaf, err := config.Leaf(pair)
	if err != nil {
		return nil, fmt.Errorf("%w: core.api.tls.certificate is %q", err, cfg.Certificate)
	}

	logger.InfoContext(ctx, "the API listener serves TLS",
		"certificate", cfg.Certificate, "min_version", "1.3",
		"not_after", leaf.NotAfter.Format("2006-01-02"))

	warnAboutExpiry(ctx, logger, "core.api.tls.certificate", cfg.Certificate, leaf)

	return &tls.Config{
		Certificates: []tls.Certificate{pair},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h2", "http/1.1"},
	}, nil
}

// warnAboutExpiry says at startup when a certificate is inside the last stretch
// of its validity. It is a warning and not a refusal: a browser reaches this
// listener, and a person can still click through an expired certificate.
func warnAboutExpiry(ctx context.Context, logger *slog.Logger, key, path string, leaf *x509.Certificate) {
	left := time.Until(leaf.NotAfter)
	if left > config.ExpiryWarning {
		return
	}

	logger.WarnContext(ctx, "the certificate this listener presents expires soon, and every client that "+
		"verifies it fails from that moment",
		"key", key,
		"certificate", path,
		"expires", leaf.NotAfter.UTC().Format(time.RFC3339),
		"remaining", left.Round(time.Hour).String(),
		"remedy", "reissue with scripts/generate_certificates.sh before that date")
}
