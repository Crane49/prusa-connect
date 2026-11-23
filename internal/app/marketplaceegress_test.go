package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The index carries the licence and the connections outside Prusa Connect the
// marketplace states for a release, so the install dialog shows them before a
// person approves the install.
func TestTheIndexCarriesTheLicenceAndTheEgressOfAMarketplaceRelease(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		egress *[]marketplaceEgress
		want   []httpapi.ModuleEgress
		known  bool
	}{
		"declared": {
			egress: &[]marketplaceEgress{
				{Host: "api.github.com", Why: "Lists the releases", Sends: "The request"},
				{Option: "manifest_url", Ports: []int{8443}, From: "ui", Why: "Reads the manifest", Sends: "Nothing"},
			},
			want: []httpapi.ModuleEgress{
				{
					Host:  "api.github.com",
					Ports: []int{443},
					From:  "process",
					Why:   "Lists the releases",
					Sends: "The request",
				},
				{Option: "manifest_url", Ports: []int{8443}, From: "ui", Why: "Reads the manifest", Sends: "Nothing"},
			},
			known: true,
		},
		"connects nowhere": {egress: &[]marketplaceEgress{}, want: []httpapi.ModuleEgress{}, known: true},
		"not declared":     {egress: nil, known: false},
		"unreadable entry": {egress: &[]marketplaceEgress{{Why: "w", Sends: "s"}}, known: false},
		"an escape in why": {
			egress: &[]marketplaceEgress{{Host: "a.example", Why: "w\x1b[2J", Sends: "s"}}, known: false,
		},
		"a reversal in sends": {
			egress: &[]marketplaceEgress{{Host: "a.example", Why: "w", Sends: "s\u202eevil"}}, known: false,
		},
		"a port out of range": {
			egress: &[]marketplaceEgress{{Host: "a.example", Ports: []int{70000}, Why: "w", Sends: "s"}}, known: false,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
			market.asks = &marketplaceRelease{
				Permissions: []string{"observe"}, License: " MIT ", Egress: testCase.egress,
			}
			installer, _ := marketInstaller(t, market.server.URL)

			index, err := installer.Index(t.Context(), "user:operator")
			require.NoError(t, err)
			var offer httpapi.ModuleOffer
			for _, row := range index.Items {
				if row.Module == theModulePath {
					offer = row
				}
			}
			require.Equal(t, theModulePath, offer.Module)
			require.Equal(t, "MIT", offer.License)
			require.Equal(t, testCase.known, offer.EgressKnown)

			if testCase.known {
				require.Equal(t, testCase.want, offer.Egress)
			} else {
				require.Empty(t, offer.Egress)
			}
		})
	}
}

// A licence the marketplace states with an invisible or control character, or
// longer than a manifest may name, is not shown.
func TestTheIndexDropsALicenceThatCouldHideWhatItSays(t *testing.T) {
	t.Parallel()

	for _, license := range []string{"MIT\u202eLPG", "MIT\x1b[31m", strings.Repeat("x", 201)} {
		market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
		market.asks = &marketplaceRelease{Permissions: []string{"observe"}, License: license}
		installer, _ := marketInstaller(t, market.server.URL)

		index, err := installer.Index(t.Context(), "user:operator")
		require.NoError(t, err)

		for _, row := range index.Items {
			if row.Module == theModulePath {
				require.Empty(t, row.License, "the licence %q was shown", license)
			}
		}
	}
}

// A request header the catalogue lists with a name no manifest may ask for, or
// with a reason holding an invisible or control character, is not offered, so
// the install dialog never shows a line that could hide what it asks. A name
// the catalogue spells in lower case is offered in its canonical form.
func TestTheIndexDropsARequestHeaderThatCouldHideWhatItAsks(t *testing.T) {
	t.Parallel()

	market := newFakeMarketplace(t, archiveBytes(t, bothHalvesManifest()), "1.0.0")
	market.asks = &marketplaceRelease{
		Permissions: []string{"observe"},
		RequestHeaders: []install.RequestHeader{
			{Name: "print-after-upload", Reason: "PrusaSlicer says whether to print"},
			{Name: "Cookie", Reason: "a credential"},
			{Name: "Overwrite", Reason: "PrusaSlicer replaces a file of the same name"},
			{Name: "Print-After-Upload", Reason: "r\x1b[2K"},
			{Name: "Print-After-Upload", Reason: "r\u202eevil"},
			{Name: "Print-After-Upload"},
		},
	}
	installer, _ := marketInstaller(t, market.server.URL)

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)

	var offer httpapi.ModuleOffer
	for _, row := range index.Items {
		if row.Module == theModulePath {
			offer = row
		}
	}

	require.Equal(t, []httpapi.ModuleRequestHeader{
		{Name: "Print-After-Upload", Reason: "PrusaSlicer says whether to print"},
	}, offer.RequestHeaders)
}

// A release on this server is offered with the licence and the egress its own
// manifest declares.
func TestALocalModuleIsOfferedWithItsLicenceAndEgress(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)
	document := strings.Replace(asking("1.2.0", 0, "observe"), "permissions:\n", `egress:
  - host: materials.example
    why: Downloads the catalogue
    sends: The catalogue version it has
permissions:
`, 1)
	installer.core.ModulesLocal = map[string]string{theModulePath: aRelease(t, document, true)}

	index, err := installer.Index(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, index.Items, 1)

	offer := index.Items[0]
	require.Equal(t, "MIT", offer.License)
	require.True(t, offer.EgressKnown)
	require.Equal(t, []httpapi.ModuleEgress{{
		Host: "materials.example", Ports: []int{443}, From: "process",
		Why: "Downloads the catalogue", Sends: "The catalogue version it has",
	}}, offer.Egress)
}
