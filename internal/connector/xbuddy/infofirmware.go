package xbuddy

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// This file keeps the firmware a printer is filed under in step with the one
// its INFO reports. identityOf files a handshake with no User-Agent-Version
// under the version its registration named, and a printer keeps its token, and
// so its registration, across an upgrade.

// reportFirmware ends the session when INFO names a firmware other than the one
// it was built for, once onFirmware has made that the firmware the printer's
// next handshake is filed under. A capability set is fixed for a session's
// life, so the new firmware takes a session of its own, as a reflashed printer
// that announces its version in the handshake does.
//
// onFirmware is nil for a session whose handshake carried User-Agent-Version.
// That header is what such a printer is filed under, and refiling it under
// INFO would end every session whose two spellings differ.
func (s *Session) reportFirmware(data json.RawMessage) {
	if s.onFirmware == nil {
		return
	}

	var payload infoPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	firmware := strings.TrimSpace(payload.Firmware)
	reported := connectorapi.NewFirmwareLabel(firmware)
	if reported.IsZero() || reported == s.identity.Firmware {
		return
	}

	if !s.onFirmware(firmware) {
		return
	}

	now := s.identity
	now.Firmware = reported

	_ = s.CloseReplaced(now)
}

// refilesFirmware is what a session calls when INFO names another firmware: it
// makes that the firmware the registration names, which is what identityOf
// falls back to for a handshake with no User-Agent-Version. It is nil for a
// handshake that carried the header, because the header is then what the
// printer is filed under.
//
// A printer this driver holds no registration for has nowhere for the version
// to be filed, so its session is kept.
func (d *Driver) refilesFirmware(r *http.Request, fingerprint string) func(string) bool {
	if strings.TrimSpace(r.Header.Get(firmwareHeader)) != "" {
		return nil
	}

	// The session outlives the handshake request, and so does this.
	ctx := context.WithoutCancel(r.Context())

	return func(firmware string) bool {
		filed, err := d.registrar.refileFirmware(ctx, fingerprint, firmware)
		if err != nil {
			d.logError(ctx, "the firmware a printer reported could not be stored",
				slog.String("fingerprint", fingerprint), slog.String("error", err.Error()))
		}

		return filed
	}
}

// refileFirmware makes firmware the version the registration for fingerprint
// names, and reports whether the registration names it now. It is false only
// for a printer this driver never registered. The table is changed before the
// store is written, so a failed write still leaves the next handshake reading
// firmware.
func (r *registrar) refileFirmware(ctx context.Context, fingerprint, firmware string) (bool, error) {
	write, filed := r.refileFirmwareInTable(fingerprint, firmware)
	if !filed {
		return false, nil
	}

	return true, write.run(ctx)
}

func (r *registrar) refileFirmwareInTable(fingerprint, firmware string) (storeWrite, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.resolveLocked(fingerprint)
	if !ok {
		return storeWrite{}, false
	}

	if item.Firmware == firmware {
		return storeWrite{}, true
	}

	item.Firmware = firmware

	return r.saveLocked(item), true
}
