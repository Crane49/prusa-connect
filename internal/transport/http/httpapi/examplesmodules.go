package httpapi

import "time"

// The module a marketplace page installs in the worked examples: a spool
// tracker with a process half and a browser half, from the repository the
// module guides already use.
const (
	exampleModulePath      = "github.com/acme/spools"
	exampleModuleNamespace = "com.github.aux.acme.spools"
	exampleModuleVersion   = "1.4.0"
	exampleModuleSource    = "https://github.com/acme/spools/releases/download/v1.4.0/spools-1.4.0.tar.gz"
)

// moduleInstallExamples are the worked examples for installing a module and
// listing what there is to install. They are in this file because examples.go
// is at the length limit this project holds a source file to.
func moduleInstallExamples() map[string]operationExample {
	return map[string]operationExample{
		// Without approve, which is the install a page makes first: the module
		// lands switched off and the answer says what it asks for, so a person
		// reads that before anything of the module runs.
		"install-module": {
			Status: createdCode,
			Request: ModuleRelease{
				Module:  exampleModulePath,
				Version: "^1.4.0",
				From:    exampleModuleSource,
			},
			Response: ModuleInstallation{
				Instance:  "sink:spools",
				Type:      exampleModuleNamespace + ".spools",
				Namespace: exampleModuleNamespace,
				Kind:      "sink",
				Version:   exampleModuleVersion,
				Runtime:   "service",
				Root:      "/var/lib/prusa-connect/modules/spools",
				Enabled:   false,
				Verified:  4,
				State:     "disabled",
				Requests: []string{
					"event device.job.finished as " + exampleModuleNamespace + ".spool_used",
					"permission observe",
				},
			},
		},

		// A print planner that requires a 3MF reader this server does not
		// have: the plan names the release the install would add.
		"plan-module-install": {
			Request: ModuleReleaseRef{Module: "github.com/acme/planner", Version: "1.0.0"},
			Response: ModulePlan{Dependencies: []ModulePlanned{{
				ModuleOffer: ModuleOffer{
					Module: "github.com/acme/threemf", Name: "threemf", Version: "1.4.0",
					Source:    "https://github.com/acme/threemf/releases/download/v1.4.0/threemf-1.4.0.tar.gz",
					Namespace: "com.github.aux.acme.threemf", Kind: "sink", Origin: "marketplace",
					RequestsKnown: true, Permissions: []string{"read_storage"}, Process: true,
					FileTypes: []string{".3mf"}, ViewTypes: []string{".3mf"},
				},
				RequiredBy: []string{"com.github.aux.acme.planner ^1.2"},
			}}, Conflicts: []ModuleFileTypeConflict{}},
		},

		// Removing the print planner leaves the 3MF reader an install added for
		// it, and nothing else requires that.
		"get-module-removal": {
			Response: ModuleRemoval{
				RequiredBy: []ModuleDependant{},
				Unused: []ModuleUnused{{
					Instance: "sink:threemf", Module: "com.github.aux.acme.threemf", Version: "1.4.0",
					Title: "3MF projects",
				}},
			},
		},

		"get-module-why": {Response: map[string]any{"items": []ModuleReason{{
			By: "com.github.aux.acme.planner", Constraint: "^1.2",
			Text: "com.github.aux.acme.planner 1.0.0 requires com.github.aux.acme.threemf ^1.2",
		}}}},

		"get-module-why-not": {
			Query: "version=2.0.0",
			Response: map[string]any{"items": []ModuleReason{{
				By: "com.github.aux.acme.planner", Constraint: "^1.2",
				Text: "com.github.aux.acme.planner 1.0.0 requires com.github.aux.acme.threemf ^1.2",
			}}},
		},

		"list-module-outdated": {Response: map[string]any{"items": []ModuleOutdated{{
			Namespace: "com.github.aux.acme.threemf", Path: "github.com/acme/threemf", Installed: "1.2.0",
			Allowed: "1.4.0", Latest: "2.0.0", Status: "update-possible",
		}}}},

		"list-module-index": {Response: map[string]any{"items": []ModuleOffer{
			{
				Module:      exampleModulePath,
				Name:        "Spools",
				Description: "Counts what each print used and warns before a spool runs out.",
				Version:     exampleModuleVersion,
				Source:      exampleModuleSource,
				Author:      "acme",
				Kind:        "sink",
				Installed:   true,
				Process:     true,
				Instance:    "sink:spools",
				Namespace:   exampleModuleNamespace,
				Origin:      "local",
				// An installed release older than the one on offer is an update.
				InstalledVersion: "1.3.0",
			},
			{
				Module:      "github.com/acme/nozzle-watch",
				Name:        "nozzle-watch",
				Title:       "Nozzle watch",
				Description: "Adds a nozzle wear panel to every device card.",
				Version:     "2.1.0",
				Source:      "https://plugins.example/api/connect/v1/extensions/com.github.aux.acme.nozzle-watch/download?version=2.1.0",
				Author:      "acme",
				Kind:        "asset",
				Namespace:   "com.github.aux.acme.nozzle-watch",
				Origin:      "marketplace",
				Sha256:      "9f2c4e1a7b3d5f60812a9c4e7d1b3f5a6c8e0d2f4a6b8c0e2d4f6a8b0c2e4f6a",
				Trust:       "verified",
				PageURL:     "https://plugins.example/connect/extensions/com.github.aux.acme.nozzle-watch",
				// A UI-only release names no platform and runs no process.
			},
		}, "marketplace": exampleMarketplace}},

		"get-module-marketplace": {Response: exampleMarketplace},

		// Switching it off: nothing is read from it any more, so the answer
		// says nothing about a last read.
		"set-module-marketplace": {
			Request: map[string]any{"enabled": false},
			Response: ModuleMarketplace{
				URL:       exampleMarketplace.URL,
				BrowseURL: exampleMarketplace.BrowseURL,
				Enabled:   false,
			},
		},

		// A module removed with its database left on the server's Postgres
		// server, which this server made and a purge drops.
		"list-module-databases": {Response: map[string]any{"items": []ModuleDatabaseLeft{{
			Instance:    exampleLeftInstance,
			Type:        exampleModuleNamespace + "/notes",
			Engine:      "postgres",
			Where:       "db.lan:5432/module_notes",
			Provisioned: true,
			LeftAt:      exampleLeftAt,
			Why:         "removed",
			Purge:       "drop",
		}}}},

		"purge-module-database": {
			Query: "confirm=" + exampleLeftInstance,
			Response: map[string]any{"items": []ModuleDatabasePurged{{
				Instance: exampleLeftInstance,
				Engine:   "postgres",
				Where:    "db.lan:5432/module_notes",
				Done:     "dropped",
			}}},
		},
	}
}

// The database the module examples leave behind. The instance is the one the
// path parameters name, so the purge example deletes what the listing lists.
const (
	exampleLeftKind     = "sink"
	exampleLeftName     = "notes"
	exampleLeftInstance = exampleLeftKind + ":" + exampleLeftName
)

//nolint:gochecknoglobals // a fixed sample time, read-only after init.
var exampleLeftAt = time.Date(2026, time.October, 21, 9, 30, 0, 0, time.UTC)

// exampleMarketplace is the marketplace the module samples read from.
var exampleMarketplace = ModuleMarketplace{
	URL:       "https://plugins.example",
	BrowseURL: "https://plugins.example/connect",
	Enabled:   true,
	Reachable: true,
	ReadAt:    "2026-10-05T12:00:00Z",
}
