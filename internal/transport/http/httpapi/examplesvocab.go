package httpapi

import (
	"net/http"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The vocabulary examples.
//
// A vocabulary answer is the whole closed set: about a hundred error codes, a
// hundred and fifty capability keys, and nine resource kinds with a paragraph
// each. Inlining one whole would put a quarter of a megabyte of prose in the
// document a client generates from, so each example carries a few entries and
// the operation's own description says the answer is the whole set.
//
// Every entry here is one this build serves, byte for byte.
// TestAVocabularyExampleEntryIsOneTheServerSends drives the real operation and
// fails on an entry the answer does not contain, so an invented error code or a
// capability key nothing declares cannot be published here.
func vocabularyExamples() map[string]operationExample {
	return map[string]operationExample{
		"list-vocabularies": {Response: map[string]any{"items": []vocabularyEntry{
			{
				Name:      "error-codes",
				Path:      "/api/v1/vocabularies/error-codes",
				Reference: "/docs/error-codes/",
				Summary: "What a failure is called, what the caller does about it, and whether a " +
					"retry can succeed.",
			},
			{
				Name:      "event-types",
				Version:   event.VocabularyVersion,
				Format:    event.VocabularyFormat,
				Path:      "/api/v1/vocabularies/event-types",
				Reference: "/docs/event-types/",
				Summary:   "The core event types this build publishes, and the class each one is carried under.",
			},
		}}},

		"get-error-code-vocabulary": {Response: map[string]any{
			"vocabulary": "error-codes",
			"version":    "",
			"format":     "",
			"items": []errorCodeEntry{
				{
					Code:      "core.auth/ACCESS_DENIED",
					Status:    http.StatusForbidden,
					Summary:   "The caller may not perform that action on that object",
					Action:    "Ask an administrator for a grant on the device or on a group that contains it.",
					Retryable: false,
					Redacted:  false,
					Params:    []string{"object", "action", "verb", "command_id"},
				},
				{
					Code:    "core.auth/TOKEN_INVALID",
					Status:  http.StatusUnauthorized,
					Summary: "The credential is not one this server accepts",
					Action: "Authenticate again and retry with the token that returns. Quote the trace id " +
						"if a fresh token is refused too.",
					Retryable: false,
					Redacted:  true,
					Params:    []string{},
				},
			}}},

		"get-event-type-vocabulary": {Response: map[string]any{
			"vocabulary": "event-types",
			"version":    event.VocabularyVersion,
			"format":     "1.0",
			"namespace":  "com.prusa.connect.",
			"items": []eventTypeEntry{{
				Type:    "com.prusa.connect.device.telemetry.sampled",
				Class:   "telemetry",
				Durable: false,
				Summary: "One measurement of a device's live state.",
				Description: "Conflated and at-most-once. A slow reader is given the newest sample " +
					"rather than every sample it missed, because a superseded temperature has no " +
					"value and the queue it would fill does.",
			}},
		}},

		"get-device-issue-vocabulary": {Response: map[string]any{
			"vocabulary": "device-issues",
			"version":    "1.0.0",
			"format":     "1.0",
			"items": []deviceIssueEntry{{
				Reason:   "awaiting_operator",
				Severity: "error",
				Summary:  "The device raised a dialog and is waiting for a person.",
				Description: "The device put a question on its own screen and stopped. Nothing this " +
					"server sends moves it on except an answer to that dialog, so a job does not " +
					"continue until somebody answers it. The host reads it from the reserved dialog " +
					"subtree of the state tree: /dialog/id raises this issue, and /dialog/title, " +
					"/dialog/text and /dialog/code are what the machine displayed, in the machine's " +
					"own words.",
				Action: "Read the dialog title and text on the device record, then answer it at the " +
					"machine or with the dialog action verb.",
			}},
			"severities": []string{"info", "warning", "error"},
		}},

		// The one example here that is not an entry this build serves. The
		// device-state set is per-install, so what a server answers is what the
		// connectors an operator mounted declared, and no example can be both
		// short and byte for byte a server's own answer. This is three states
		// of a reference dialect, and a reader needs the shape.
		"get-device-state-vocabulary": {Response: map[string]any{
			"vocabulary": "device-states",
			"version":    "",
			"format":     "1.0",
			"items": []deviceStateKindEntry{{
				Kind:       "printer",
				Namespace:  "core",
				Connector:  "core/echo",
				Connectors: []string{"bench"},
				Guarded:    true,
				States: []deviceStateEntry{
					{State: "IDLE", DisplayName: "Idle", Accepts: []string{
						"core.command.get_status", "core.command.start_print",
					}},
					{State: "PRINTING", DisplayName: "Printing", Accepts: []string{"core.command.get_status"}},
					{State: "FINISHED", DisplayName: "Finished", Accepts: []string{"core.command.get_status"}},
				},
				Transitions: []deviceStateTransition{
					{From: "IDLE", To: "PRINTING", Verb: "core.command.start_print"},
					{From: "PRINTING", To: "FINISHED"},
					{From: "FINISHED", To: "IDLE"},
				},
			}},
		}},

		// Three rows, in the served order by spelling, which is enough to show a
		// reader what a row holds, how two models share one icon key, and what a
		// machine reached through another dialect looks like beside them. The
		// last two are the core's own.
		//
		// The first is a row a driver outside this project declares in a build
		// that composes it (ADR-20261016-qrp), with the drawing it declared. Its dialect
		// row is the driver's own answer: the brands, and the address a person
		// declares because this server dials the machine. The same driver also
		// declared a Prusa model, which the server refused. All of it is
		// invented, so the reference names no machine a given build may not reach.
		"get-device-model-vocabulary": {Response: map[string]any{
			"vocabulary": "device-models",
			"version":    "",
			"format":     "",
			"items": []deviceModelEntry{
				{
					Model:       exampleDeclaredModel,
					DisplayName: "Bench Works Tool Changer",
					Vendor:      "Bench Works",
					Kind:        "printer",
					Icon:        "printer-generic",
					Connector:   "bench",
					ToolChanger: true,
					Artwork:     artworkPathOf(exampleDeclaredModel),
				},
				{
					Model:        "CoreOne",
					DisplayName:  "Prusa CORE One",
					Vendor:       "Prusa Research",
					Kind:         "printer",
					Icon:         "printer-core-one",
					Connector:    "xbuddy",
					DeviceTypeID: "7.1.0",
				},
				{
					Model:        "CoreOneL",
					DisplayName:  "Prusa CORE One L",
					Vendor:       "Prusa Research",
					Kind:         "printer",
					Icon:         "printer-core-one",
					Connector:    "xbuddy",
					DeviceTypeID: "8.1.0",
				},
			},
			"connectors": []connectorEntry{
				{
					Connector: "xbuddy",
					Qualified: "com.prusa.connect/xbuddy",
					Kind:      "printer",
					Brands:    []string{"Prusa"},
					Summary:   "The dialect every Prusa machine speaks.",
					Available: true,
					Arrives:   true,
					Needs:     connectorapi.NeedsSettingsFile.String(),
					Models:    []string{"CoreOne", "CoreOneL"},
				},
				{
					Connector: "bench",
					Qualified: "com.example.aux.bench/bench",
					Kind:      "printer",
					Brands:    []string{"Bench Works"},
					Summary:   "A bench machine this server dials.",
					Available: true,
					Dials:     true,
					Needs:     connectorapi.NeedsAddress.String(),
					Models:    []string{exampleDeclaredModel},
				},
			},
			"refused": []refusedModelEntry{{
				Model:        "MK4",
				Connector:    "bench",
				Qualified:    "com.example.aux.bench/bench",
				CollidesWith: profile.Refusal{Profile: "MK4"}.CollidesWith(),
				Remedy:       profile.Refusal{Profile: "MK4"}.Remedy(),
			}},
		}},

		"get-resource-kind-vocabulary": {Response: map[string]any{
			"vocabulary": "resource-kinds",
			"version":    "1.2.0",
			"format":     "1.1",
			"items": []resourceKindEntry{{
				Kind:    "storage",
				Summary: "Where uploaded files and job artefacts are kept.",
				Description: "A storage instance is a place bytes live: a directory on the host, a " +
					"bucket, or a temporary area that is emptied on restart. Uploads, job files and " +
					"camera snapshots are addressed through one, and an instance that is not ready " +
					"takes only the feature that needs it out of service.",
				Configurable: true,
				Runtimes:     []string{"builtin", "service"},
			}},
			"runtimes": []resourceRuntimeEntry{},
		}},

		"get-capability-vocabulary": {Response: capabilitiesBody{
			Vocabulary: "capabilities",
			Version:    "",
			Format:     "",
			Contract:   "1.0.0",
			Kinds: []capabilityKind{
				{Kind: "flag", Numeric: false},
				{Kind: "integer", Numeric: true},
			},
			Keys: []capabilityKey{
				{Key: "core.command.set_nozzle_temperature", Kind: "flag", Required: false},
				{
					Key:      "core.command.set_nozzle_temperature#nozzle_temperature",
					Verb:     "core.command.set_nozzle_temperature",
					Argument: "nozzle_temperature",
					Kind:     "integer",
					Required: true,
				},
			},
			// Empty, and it has to be: TestAVocabularyExampleEntryIsOneTheServerSends
			// holds every entry here to what this build sends, and the one host
			// feature that exists is the full build's alone. A reader who wants
			// the keys reads the host features table of the capability
			// vocabulary page, which names every key whatever build generated it.
			Host: []hostFeature{},
		}},

		"get-authorization-vocabulary": {Response: map[string]any{
			"vocabulary": "authorization",
			"version":    "1.0.0",
			"format":     "1.0",
			"actions": []authorizationAction{{
				Action:  "operate",
				Domain:  "device",
				Summary: "Send a verb of this class to a device: act on the machine in its normal duty.",
				Description: "Pause, resume, home, set a target, stop a job, put a file on the " +
					"machine, and every other verb the device's kind declares at this class. It is the " +
					"floor a verb may carry: there is no read class for a verb. The kind's declaration " +
					"decides which verbs exist; this action decides whether the caller may send one of " +
					"them at all.",
				ConferredBy: []string{"parent", "org", "group", "operator", "manager", "owner"},
			}},
			"types": []authorizationType{{
				Type:    "device",
				Summary: "One physical device.",
				Description: "The object every device action is defined on. A device names its org " +
					"and its group, so a grant written at either level reaches it without a tuple per " +
					"device. Its kind says what type of device it is: this build registers printers " +
					"and cameras. A device may also name another device as its parent, which is how a " +
					"camera is reached by whoever may reach the printer it watches.",
				Subject: true,
				Object:  true,
			}},
			"relations": []authorizationRelation{{
				Relation: "operator",
				Summary:  "The day-to-day role.",
				Description: "Contains viewer and adds operate on a device. Adds operate_resource and " +
					"write_storage on a resource kind. These actions change what a device or an instance " +
					"is doing, and what is inside one, without changing what it is.",
				Role: true,
			}},
			// The camera, because an example entry has to be one the server
			// really sends and the printer's entry is 59 verbs long. A kind
			// with no verbs is also the shape worth showing: a device the
			// directory holds and no command reaches.
			"kinds": []authorizationKind{{
				Kind:        "camera",
				DisplayName: "Camera",
				Verbs:       []authorizationVerb{},
			}},
			"shapes": []authorizationShape{
				{ObjectType: "device", Relation: "operator", SubjectType: "user"},
				{
					ObjectType: "device", Relation: "operator",
					SubjectType: "group", SubjectRelation: "member",
				},
			},
		}},

		"get-extension-vocabulary": {Response: map[string]any{
			"vocabulary": "extensions",
			"version":    "",
			"format":     "",
			"items": []extensionEntry{{
				Kind:      "connector",
				Name:      "xbuddy",
				Qualified: "core/xbuddy",
				Module:    "github.com/prusa3d/prusa-connect",
				Summary: "The dialect Prusa xBuddy and Buddy boards speak, over the frozen registration " +
					"and websocket wire.",
				Description: "The only dialect with machines in the field, so its wire is frozen rather " +
					"than chosen.",
				Stability: "stable",
				Version:   "1.0.0",
				Options: []extensionOption{
					{
						Key:     "mode",
						Type:    "string",
						Default: "none",
						Summary: "The credential a printer presents on the websocket: none, token, fingerprint or both.",
					},
					{
						Key:     "queue_depth",
						Type:    "int",
						Default: "32",
						Summary: "How many commands may wait for one printer, whether it is connected or away.",
					},
				},
			}}}},
	}
}
