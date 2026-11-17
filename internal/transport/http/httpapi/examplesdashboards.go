//go:build !nodashboards

package httpapi

import "time"

// exampleDashboardNumber is the same id as the body carries it.
const exampleDashboardNumber int64 = 3

// exampleDashboardChanged is when the sample dashboard was last saved.
var exampleDashboardChanged = time.Date(2025, time.August, 26, 9, 30, 0, 0, time.UTC)

// exampleDashboardCards is the wall of a small farm: one printer, its camera
// and a note.
func exampleDashboardCards() []DashboardCard {
	return []DashboardCard{
		{ID: "p1", Type: "core/printer", X: 0, Y: 0, W: 4, H: 4, Config: map[string]any{"device": examplePrinterID}},
		{ID: "c1", Type: "core/camera", X: 4, Y: 0, W: 5, H: 4, Config: map[string]any{"camera": exampleCameraName}},
		{
			ID: "n1", Type: "core/note", X: 9, Y: 0, W: 3, H: 2, Title: "Today",
			Config: map[string]any{"text": "PETG on the left spool."},
		},
	}
}

// exampleDashboard is the dashboard the home page draws on the sample server.
func exampleDashboard() Dashboard {
	return Dashboard{
		ID:         exampleDashboardNumber,
		Name:       "Print farm",
		Visibility: "everyone",
		IsDefault:  true,
		Columns:    12,
		Density:    "comfortable",
		Cards:      exampleDashboardCards(),
		Revision:   4,
		Created:    exampleDashboardChanged.Add(-72 * time.Hour),
		CreatedBy:  exampleSubject,
		Changed:    exampleDashboardChanged,
		ChangedBy:  exampleSubject,
	}
}

// exampleDashboardRow is that dashboard as the listing shows it.
func exampleDashboardRow() DashboardRow {
	board := exampleDashboard()

	return DashboardRow{
		ID: board.ID, Name: board.Name, Visibility: board.Visibility,
		IsDefault: board.IsDefault, Cards: len(board.Cards), Changed: board.Changed, ChangedBy: board.ChangedBy,
	}
}

// exampleDashboardDraft is the change a person saves from the editor: the
// whole dashboard, against the revision they read.
func exampleDashboardDraft() DashboardDraft {
	board := exampleDashboard()
	revision := board.Revision - 1
	cards := board.Cards

	return DashboardDraft{
		Name: board.Name, Visibility: &board.Visibility,
		Columns: &board.Columns, Density: &board.Density, Cards: &cards, Revision: &revision,
	}
}

func dashboardExamples() map[string]operationExample {
	created := exampleDashboard()
	created.IsDefault = false
	created.Revision = 1
	created.Created = created.Changed

	draft := exampleDashboardDraft()
	draft.Revision = nil

	return map[string]operationExample{
		"list-dashboards": {Response: DashboardListing{
			Items: []DashboardRow{exampleDashboardRow()}, Total: 1, DefaultID: new(exampleDashboardNumber),
		}},
		"get-dashboard": {Response: exampleDashboard()},
		"create-dashboard": {
			Status:   createdCode,
			Request:  draft,
			Response: created,
		},
		"replace-dashboard": {Request: exampleDashboardDraft(), Response: exampleDashboard()},
		"set-default-dashboard": {
			Request:  DefaultDashboardBody{ID: new(exampleDashboardNumber)},
			Response: map[string]any{"default_id": exampleDashboardNumber},
		},
	}
}
