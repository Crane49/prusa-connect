package xbuddy

import (
	"encoding/json"
	"net"
	"strings"
	"unicode"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// networkPaths is where each key of the INFO event's network_info lands in
// the state tree. A module that receives the printer's syslog matches a
// datagram to a device by these. wifi_ssid is left
// out: it names the owner's network and nothing here needs it.
//
// Everyone who may observe the printer can read these nodes, like the rest of
// its state tree.
//
//nolint:gochecknoglobals // a lookup table, read-only after init.
var networkPaths = map[string]string{
	"lan_mac":   "/network/lan_mac",
	"lan_ipv4":  "/network/lan_ipv4",
	"wifi_mac":  "/network/wifi_mac",
	"wifi_ipv4": "/network/wifi_ipv4",
	"hostname":  "/network/hostname",
}

// maxHostnameBytes is the longest hostname kept. A printer reports the
// short name it answers to on the network, and a DNS label is at most 63
// bytes.
const maxHostnameBytes = 64

// nullValue removes a node from the state tree.
var nullValue = json.RawMessage("null") //nolint:gochecknoglobals // a constant the json package cannot hold.

// networkPatches reads network_info out of an INFO payload. A key the
// printer left out removes its node, because the firmware leaves out an
// interface that is down and a stale address would match another machine.
// A value that is not a MAC, an IPv4 address or a hostname removes its node
// too. The MACs are written in lower case with colons, the spelling the
// printer's syslog uses, where INFO writes them in upper case. A payload with
// no network_info changes nothing.
func networkPatches(data json.RawMessage) []connectorapi.StatePatch {
	var payload struct {
		Network map[string]json.RawMessage `json:"network_info"`
	}

	if err := json.Unmarshal(data, &payload); err != nil || payload.Network == nil {
		return nil
	}

	patches := make([]connectorapi.StatePatch, 0, len(networkPaths))

	for key, path := range networkPaths {
		var value string
		if raw, sent := payload.Network[key]; sent && json.Unmarshal(raw, &value) == nil {
			value = networkValue(key, value)
		}

		if value == "" {
			patches = append(patches, connectorapi.StatePatch{Path: path, Value: nullValue})

			continue
		}

		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}

		patches = append(patches, connectorapi.StatePatch{Path: path, Value: encoded})
	}

	return patches
}

// networkValue answers the value kept for one network_info key, or "" when the
// printer sent something that is not one.
func networkValue(key, value string) string {
	switch {
	case strings.HasSuffix(key, "_mac"):
		return macText(value)
	case strings.HasSuffix(key, "_ipv4"):
		return ipv4Text(value)
	default:
		return hostnameText(value)
	}
}

// macText answers a 48-bit MAC in lower case with colons, or "".
func macText(value string) string {
	hw, err := net.ParseMAC(value)
	if err != nil || len(hw) != 6 {
		return ""
	}

	return hw.String()
}

// ipv4Text answers an IPv4 address in dotted form, or "".
func ipv4Text(value string) string {
	ip := net.ParseIP(value).To4()
	if ip == nil {
		return ""
	}

	return ip.String()
}

// hostnameText drops control and format characters, which would reach a log
// line or a terminal as they are or turn the text around, and cuts the rest
// to maxHostnameBytes.
func hostnameText(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}

		return r
	}, value))

	return machinetext.Clip(value, maxHostnameBytes)
}

// reportNetwork puts what an INFO said about the network on the state tree.
// The nodes stay when the connection goes, as the rest of the last state
// does. A module that matches by address checks that the printer is
// connected.
func (s *Session) reportNetwork(data json.RawMessage) {
	for _, patch := range networkPatches(data) {
		s.report(patch)
	}
}
