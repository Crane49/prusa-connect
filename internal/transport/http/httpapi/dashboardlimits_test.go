//go:build !nodashboards

package httpapi

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/dashboard"
)

// TestTheDocumentedLimitsAreTheRulesOwn holds the bounds the document publishes
// to the constants the rules check. A struct tag cannot name a constant, so
// the number is written twice, and this is what keeps the two the same.
func TestTheDocumentedLimitsAreTheRulesOwn(t *testing.T) {
	t.Parallel()

	for _, step := range []struct {
		of    any
		field string
		tag   string
		want  int
	}{
		{DashboardCard{}, "X", "maximum", dashboard.MaxColumns - 1},
		{DashboardCard{}, "Y", "maximum", dashboard.MaxRows - 1},
		{DashboardCard{}, "W", "maximum", dashboard.MaxColumns},
		{DashboardCard{}, "H", "maximum", dashboard.MaxCardRows},
		{DashboardCard{}, "Title", "maxLength", dashboard.MaxTitleBytes},
		{Dashboard{}, "Columns", "minimum", dashboard.MinColumns},
		{Dashboard{}, "Columns", "maximum", dashboard.MaxColumns},
		{DashboardDraft{}, "Columns", "minimum", dashboard.MinColumns},
		{DashboardDraft{}, "Columns", "maximum", dashboard.MaxColumns},
		{DashboardDraft{}, "Name", "maxLength", dashboard.MaxNameBytes},
		{DashboardDraft{}, "Cards", "maxItems", dashboard.MaxCards},
	} {
		field, found := reflect.TypeOf(step.of).FieldByName(step.field)
		require.Truef(t, found, "%T has no %s", step.of, step.field)

		got, err := strconv.Atoi(field.Tag.Get(step.tag))
		require.NoErrorf(t, err, "%T.%s carries no %s", step.of, step.field, step.tag)
		require.Equalf(t, step.want, got, "%T.%s %s", step.of, step.field, step.tag)
	}
}
