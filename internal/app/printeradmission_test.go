package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// table is a registration table that answers nothing, with or without an
// opinion on how it admits.
type table struct{}

func (table) Pending() []connectorapi.DeviceRegistration { return nil }

func (table) Approve(context.Context, string, string) (connectorapi.DeviceRegistration, error) {
	return connectorapi.DeviceRegistration{}, nil
}

func (table) Reject(context.Context, string) error { return nil }

// saying is a table that says whether it admits on arrival.
type saying struct {
	table

	open bool
}

func (s saying) AdmitsOnArrival() bool { return s.open }

// The add-printer page reads one word for the whole server. One connector that
// asks a person is enough to make it approval, and a table that says nothing is
// taken to ask.
func TestThePrinterAdmissionIsAutomaticOnlyWhenEveryTableApprovesOnArrival(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		tables []admits
		want   string
	}{
		{"no table at all", nil, event.AdmissionAutomatic},
		{"one table that approves on arrival", []admits{saying{open: true}}, event.AdmissionAutomatic},
		{"one table that asks", []admits{saying{open: false}}, event.AdmissionApproval},
		{"a table that says nothing", []admits{table{}}, event.AdmissionApproval},
		{"one of two asks", []admits{saying{open: true}, saying{open: false}}, event.AdmissionApproval},
		{"both approve on arrival", []admits{saying{open: true}, saying{open: true}}, event.AdmissionAutomatic},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()

			tables := make([]admissionTable, 0, len(each.tables))
			for _, driver := range each.tables {
				tables = append(tables, admissionTable{ref: "xbuddy", driver: driver})
			}

			require.Equal(t, each.want, printerAdmission(tables))
		})
	}
}

// The word reaches the page, read from the connector the process built and not
// from what a page might guess.
func TestTheAppSettingsSayHowAPrinterJoins(t *testing.T) {
	t.Parallel()

	asking := start(t, t.TempDir(), decidedAdmissionConfig(freeAddr(t)))
	require.Equal(t, event.AdmissionApproval, admissionOn(t, asking))

	open := strings.NewReplacer("mode: token", "mode: none", "auto_register: false", "auto_register: true").
		Replace(decidedAdmissionConfig(freeAddr(t)))
	require.Equal(t, event.AdmissionAutomatic, admissionOn(t, start(t, t.TempDir(), open)))
}

func admissionOn(t *testing.T, running *process) string {
	t.Helper()

	read := running.get(t, "/api/v1/ui")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var settings struct {
		PrinterAdmission string `json:"printer_admission"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &settings))

	return settings.PrinterAdmission
}
