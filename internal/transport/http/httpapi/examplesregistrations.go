package httpapi

// admissionExamples is the worked answer for the event stream's reader count
// and for the operator's half of admission. They are in this file because
// examples.go is at its length limit.
func admissionExamples() map[string]operationExample {
	return map[string]operationExample{
		"get-event-stream-stats": {Response: EventStreamStats{Subscribers: 3}},

		"list-pending-registrations": {Response: map[string]any{
			"items": []Registration{exampleRegistration()},
		}},

		"approve-registration": {Response: approvedExample()},

		"set-device-tags": {
			Request:  map[string]any{"tags": []string{"workshop", "PETG only", "night shift"}},
			Response: map[string]any{"tags": []string{"workshop", "PETG only", "night shift"}},
		},

		"set-device-labels": {
			Request:  map[string]any{"labels": map[string]string{"site": "brno", "spare": ""}},
			Response: map[string]any{"labels": map[string]string{"site": "brno", "spare": ""}},
		},
	}
}

// exampleRegistration is one device waiting for a decision, as the listing
// shows it. The code is drawn from the alphabet an operator can read aloud, and
// the row carries no credential.
func exampleRegistration() Registration {
	return Registration{
		Code:        exampleRegistrationCode,
		Connector:   "xbuddy",
		Fingerprint: "b3f1c0d2e4a5",
		Serial:      "8221-1234567",
		Model:       "MK4",
		Firmware:    "6.1.3",
	}
}

// approvedExample is the same registration once somebody has let it through.
//
// It carries the device id the approval bound the machine to, which is what
// separates the answer to an approval from a row on the listing: a page reads
// it and opens that device's page.
func approvedExample() Registration {
	approved := exampleRegistration()
	approved.Approved = true
	approved.DeviceID = examplePrinterID

	return approved
}
