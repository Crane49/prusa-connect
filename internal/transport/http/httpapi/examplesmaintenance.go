package httpapi

import (
	"time"

	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

var exampleMaintenanceTime = time.Date(2026, time.October, 25, 19, 14, 5, 0, time.UTC)

func exampleUpgradeJournal() upgrade.Journal {
	return upgrade.Journal{
		Schema: 1, ID: "upgrade-20261025T191405Z", State: upgrade.StateRehearsed,
		FromVersion: "1.2.0", ToVersion: "1.3.0",
		StagedBinary:  "/var/lib/prusa-connect/upgrade/staged/prusa-connect",
		CurrentBinary: "/var/lib/prusa-connect/prusa-connect",
		Revision:      4, UpdatedAt: exampleMaintenanceTime,
	}
}

func exampleBackupManifest() upgrade.BackupManifest {
	return upgrade.BackupManifest{
		Format: 1, ID: "backup-20261025T191405Z", CreatedAt: exampleMaintenanceTime,
		FromVersion: "1.2.0", ToVersion: "1.3.0", Files: []upgrade.BackupFile{},
	}
}

func maintenanceExamples() map[string]operationExample {
	journal := exampleUpgradeJournal()
	pairingExpiry := exampleMaintenanceTime.Add(10 * time.Minute)

	return map[string]operationExample{
		"get-server-version": {Response: ServerVersion{
			Version: "1.2.0", BuildProfile: "normal", BuildVariant: "full", GOOS: "linux", GOARCH: "arm64",
			Contract: "1.0.0", Capabilities: []string{"device.read", "state.read"},
			HostSchemaVersion: 12, HostSchemaCompat: 10,
		}},
		"get-server-upgrade":          {Response: journal},
		"list-server-upgrade-history": {Response: []upgrade.Journal{}},
		"get-beta-portal-pairing-status": {
			Response: betaPortalPairingStatus{
				State: betaportal.StatePaired,
				Identity: &betaportal.Identity{
					Subject: "account-42",
					Name:    "Print room",
				},
				Product: &BetaPortalProduct{Slug: "connect-local", Name: "Prusa Connect"},
				Device:  &BetaPortalDevice{Identifier: "instance-1"},
			},
		},
		"start-beta-portal-pairing": {
			Response: betaPortalPairingStatus{
				State:             betaportal.StatePending,
				Step:              betaportal.StepConfirmation,
				UserCode:          "ABCD-EFGH",
				VerifyURL:         "https://beta.prusa3d.com/device",
				VerifyURLComplete: "https://beta.prusa3d.com/device?user_code=ABCD-EFGH",
				ExpiresAt:         &pairingExpiry,
				PollIntervalMS:    5000,
			},
			Status: createdCode,
		},
		"refresh-beta-portal-pairing": {
			Response: betaPortalPairingStatus{
				State:             betaportal.StatePending,
				Step:              betaportal.StepConfirmation,
				UserCode:          "ABCD-EFGH",
				VerifyURL:         "https://beta.prusa3d.com/device",
				VerifyURLComplete: "https://beta.prusa3d.com/device?user_code=ABCD-EFGH",
				ExpiresAt:         &pairingExpiry,
				PollIntervalMS:    5000,
			},
		},
		"list-server-upgrade-releases": {
			Response: []upgrade.Release{
				{
					Version:     "1.3.0",
					Name:        "Prusa Connect 1.3.0",
					Notes:       "Reliability improvements.",
					PublishedAt: exampleMaintenanceTime,
					Binary: upgrade.Asset{
						Name: "prusa-connect_linux_arm64",
						URL:  "https://example.invalid/core",
						Size: 42,
					},
					Sums: upgrade.Asset{Name: "SHA256SUMS", URL: "https://example.invalid/sums", Size: 64},
					Signature: upgrade.Asset{
						Name: "SHA256SUMS.sig",
						URL:  "https://example.invalid/signature",
						Size: 96,
					},
				},
			},
		},
		"stage-server-upgrade": {
			Request: map[string]any{
				"version": "1.3.0", "name": "prusa-connect_linux_arm64",
				"binary": "YmluYXJ5", "sha256sums": "c3Vtcw==", "signature": "c2lnbmF0dXJl",
			},
			Response: journal, Status: createdCode,
		},
		"stage-server-upgrade-bundle": {
			RequestMedia: "application/octet-stream",
			Request:      "<the bytes of prusa-connect-1.3.0-offline.tar>",
			Response:     journal, Status: createdCode,
		},
		"prepare-server-upgrade": {Response: journal},
		"apply-server-upgrade": {
			Request:  map[string]any{"schedule": "when-idle"},
			Response: journal, Status: "202",
		},
		"restart-server": {
			Request:  map[string]any{"mode": "safe"},
			Response: map[string]any{"mode": "safe"}, Status: "202",
		},
		"list-server-backups": {Response: []upgrade.BackupManifest{exampleBackupManifest()}},
		"create-server-backup": {
			Query: "to_version=1.3.0", Response: exampleBackupManifest(), Status: createdCode,
		},
	}
}
