package xbuddy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// refusalLogEvery is how often one printer's refused websocket is logged. A
// refused printer dials again every few seconds and never stops, so a line per
// attempt would fill the log with one fact.
const refusalLogEvery = time.Minute

// refusalLogLimit bounds how many printers the refusal log remembers. The
// fingerprint header is whatever the caller sent, so the table holds the
// newest window of keys and no more.
const refusalLogLimit = 256

// maxLoggedFingerprint is the most of a presented fingerprint a line carries.
// Firmware sends 16 characters; a longer value is not from a printer.
const maxLoggedFingerprint = 64

// The two refusal lines. The first says how a printer gets a credential this
// server accepts.
const (
	refusedCredentials = "a printer's websocket was refused: this server did not issue its credentials. " +
		"On the printer choose Add Printer to Connect, then approve its code on the Add printer page"
	refusedAdmission = "a printer's websocket was refused: the server would not admit it"
)

// refusalNoticeLimit bounds how many refused printers the host hears about in
// one minute, whatever their fingerprints. The fingerprint is the caller's to
// choose, so without it one caller inventing fingerprints fills a page and its
// notifications with refusals of printers that do not exist.
const refusalNoticeLimit = 20

// maxReportedLabel is the most of a model or firmware header a refusal notice
// carries. Both are text from a caller nobody has admitted.
const maxReportedLabel = 64

// refusalLog decides whether a refused websocket is reported, in the log and
// to the host. It remembers when it last reported each printer.
type refusalLog struct {
	mu   sync.Mutex
	last map[string]time.Time
	// notify is the host's notice, nil until the host hands one over.
	notify connectorapi.RefusalNotice
	// window and told are the minute the notices are counted in, and how many
	// it has carried.
	window time.Time
	told   int
}

// spend reports whether one more notice fits in the minute at now, and counts
// it when it does.
func (l *refusalLog) spend(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.window) >= refusalLogEvery {
		l.window, l.told = now, 0
	}

	if l.told >= refusalNoticeLimit {
		return false
	}

	l.told++

	return true
}

// use takes the host's notice.
func (l *refusalLog) use(notify connectorapi.RefusalNotice) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.notify = notify
}

// notice is the host's notice, or nil.
func (l *refusalLog) notice() connectorapi.RefusalNotice {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.notify
}

// due reports whether a line for key may be written at now, and records it
// when it may. A full table first drops the keys whose window has passed; a
// table still full stays quiet rather than grow.
func (l *refusalLog) due(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if at, seen := l.last[key]; seen && now.Sub(at) < refusalLogEvery {
		return false
	}

	if l.last == nil {
		l.last = make(map[string]time.Time)
	}

	if _, seen := l.last[key]; !seen && len(l.last) >= refusalLogLimit {
		for each, at := range l.last {
			if now.Sub(at) >= refusalLogEvery {
				delete(l.last, each)
			}
		}

		if len(l.last) >= refusalLogLimit {
			return false
		}
	}

	l.last[key] = now

	return true
}

// reportRefusedWebsocket reports a websocket this driver answered with 401 or
// 403, at most once a minute for each printer. It writes one Warn line, and
// tells the host so a person waiting for the printer sees why it does not
// join. The fingerprint goes under its own log key, which a diagnostics bundle
// drops (ADR-20261015-mde rule 4), and the host is given only its digest.
func (d *Driver) reportRefusedWebsocket(r *http.Request, status int, reason error) {
	fingerprint := machinetext.Clip(r.Header.Get(fingerprintHeader), maxLoggedFingerprint)

	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}

	key := fingerprint
	if key == "" {
		key = remote
	}

	if !d.refusals.due(key, time.Now()) {
		return
	}

	ctx := context.WithoutCancel(r.Context())

	if d.log != nil {
		message := refusedCredentials
		if status == http.StatusForbidden {
			message = refusedAdmission
		}

		d.log.LogAttrs(ctx, slog.LevelWarn, message,
			slog.Int("status", status),
			slog.String("reason", reason.Error()),
			slog.String("fingerprint", fingerprint),
			slog.String("remote", remote),
			slog.String("mode", string(d.registrar.mode)))
	}

	notify := d.refusals.notice()
	if notify == nil || !d.refusals.spend(time.Now()) {
		return
	}

	refusal := connectorapi.DeviceRefusal{
		Reason:   connectorapi.RefusalUnknownCredentials,
		Model:    machinetext.Clip(r.Header.Get(modelHeader), maxReportedLabel),
		Firmware: machinetext.Clip(r.Header.Get(firmwareHeader), maxReportedLabel),
	}

	if status == http.StatusForbidden {
		refusal.Reason = connectorapi.RefusalNotAdmitted
	}

	if fingerprint != "" {
		sum := sha256.Sum256([]byte(fingerprint))
		refusal.FingerprintDigest = hex.EncodeToString(sum[:])
	}

	notify(ctx, refusal)
}
