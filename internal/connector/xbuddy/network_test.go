package xbuddy

import (
	"encoding/json"
	"strings"
	"testing"
)

func patchesByPath(t *testing.T, data string) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, patch := range networkPatches(json.RawMessage(data)) {
		out[patch.Path] = string(patch.Value)
	}

	return out
}

// A printer on Ethernet reports its MAC and address, written in lower case
// to match the HOSTNAME of its syslog, and the Wi-Fi nodes it left out are
// removed.
func TestAnInfoPutsThePrintersNetworkOnTheStateTree(t *testing.T) {
	t.Parallel()

	got := patchesByPath(t, `{"storages":[],"network_info":{
		"lan_mac":"10:9C:70:2A:BB:CD","lan_ipv4":"192.168.1.20","hostname":"prusa-mk4","wifi_ssid":"home"}}`)

	want := map[string]string{
		"/network/lan_mac":   `"10:9c:70:2a:bb:cd"`,
		"/network/lan_ipv4":  `"192.168.1.20"`,
		"/network/hostname":  `"prusa-mk4"`,
		"/network/wifi_mac":  "null",
		"/network/wifi_ipv4": "null",
	}

	if len(got) != len(want) {
		t.Fatalf("patches = %v, want %v", got, want)
	}

	for path, value := range want {
		if got[path] != value {
			t.Errorf("%s = %s, want %s", path, got[path], value)
		}
	}
}

// An INFO without network_info, or one that is not an object, says nothing
// about the network and changes nothing.
func TestAnInfoWithoutANetworkChangesNothing(t *testing.T) {
	t.Parallel()

	for _, data := range []string{`{"storages":[]}`, `{"network_info":"x"}`, `not json`} {
		if got := networkPatches(json.RawMessage(data)); len(got) != 0 {
			t.Errorf("networkPatches(%s) = %v, want none", data, got)
		}
	}
}

// A value the printer sent that is not a MAC, an IPv4 address or a hostname
// does not reach the state tree. A MAC is written in one spelling whatever the
// printer sent, and a hostname loses its control characters and is cut.
func TestAnInfoKeepsOnlyNetworkValuesThatAreWhatTheySay(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("printer-", 10)

	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "an upper case MAC", key: "lan_mac", value: "10:9C:70:2A:BB:CD", want: `"10:9c:70:2a:bb:cd"`},
		{name: "a MAC with hyphens", key: "wifi_mac", value: "10-9C-70-2A-BB-CD", want: `"10:9c:70:2a:bb:cd"`},
		{name: "a MAC that is too long", key: "lan_mac", value: "10:9c:70:2a:bb:cd:ee:ff", want: "null"},
		{name: "text in place of a MAC", key: "lan_mac", value: "192.168.1.20", want: "null"},
		{name: "an IPv4 address", key: "lan_ipv4", value: "192.168.1.20", want: `"192.168.1.20"`},
		{name: "an IPv6 address", key: "lan_ipv4", value: "fe80::1", want: "null"},
		{name: "text in place of an address", key: "wifi_ipv4", value: "printer.local", want: "null"},
		{
			name:  "a hostname with control characters",
			key:   "hostname",
			value: `prusa\u001b[31m-mk4\n`,
			want:  `"prusa[31m-mk4"`,
		},
		{name: "a hostname of control characters only", key: "hostname", value: `\u0007\u0000`, want: "null"},
		{name: "a hostname that turns its text around", key: "hostname", value: `prusa\u202e4km`, want: `"prusa4km"`},
		{name: "a long hostname", key: "hostname", value: long, want: `"` + long[:maxHostnameBytes] + `…"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := patchesByPath(t, `{"network_info":{"`+tc.key+`":"`+tc.value+`"}}`)
			if value := got[networkPaths[tc.key]]; value != tc.want {
				t.Errorf("%s = %s, want %s", networkPaths[tc.key], value, tc.want)
			}
		})
	}
}
