package printcompat

import "testing"

// TestUpToDateComparesAsTheFirmwareDoes is GCodeInfo::is_up_to_date: the
// three numbers first, then the build after a plus sign.
func TestUpToDateComparesAsTheFirmwareDoes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		wanted, have string
		want         bool
	}{
		{"6.4.0", "6.4.0", true},
		{"6.4.0", "6.5.3+12780", true},
		{"6.6.0", "6.5.3+12780", false},
		{"6.5.3+12781", "6.5.3+12780", false},
		{"6.5.3+12780", "6.5.3+12780", true},
		{"6.5.3+99999", "6.5.3-0", true},
		{"7.0.0-rc1+100", "6.9.9+100", false},
		{"not a version", "6.0.0", true},
		{"6.5.0", "unknown", true},
	}

	for _, tc := range cases {
		if got := upToDate(tc.wanted, tc.have); got != tc.want {
			t.Errorf("a file asking %s of %s: got %v, want %v", tc.wanted, tc.have, got, tc.want)
		}
	}
}

// TestEveryModelIDIsReadByTheDeviceRecordsSpelling ties the firmware's ids to
// the spellings a device record carries.
func TestEveryModelIDIsReadByTheDeviceRecordsSpelling(t *testing.T) {
	t.Parallel()

	for name, id := range map[string]string{
		"CoreOne": "COREONE", "CoreOneL": "COREONEL", "CoreOne_INDX": "COREONEINDX",
		"CoreOneL_INDX": "COREONEL-INDX", "IX": "iX", "MK3.5S": "MK3.5S", "XL": "XL", "mini": "MINI",
	} {
		got, ok := printerModel(name)
		if !ok || got.id != id {
			t.Errorf("%s: got %q, want %q", name, got.id, id)
		}
	}

	if _, ok := printerModel("MK35"); ok {
		t.Error("MK35 is not how either side spells the MK3.5")
	}
}
