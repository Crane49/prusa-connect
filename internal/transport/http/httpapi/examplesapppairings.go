package httpapi

import "time"

// The app pairing samples follow one app, a kitchen display, from its request
// to the credential it collects.
const (
	exampleUserCode   = "BCDF-GHJK"
	exampleDeviceCode = "Qm9vdGg3cGFpcmluZ0NvZGVGb3JUaGVTYW1wbGUwMDAw"
	exampleAppName    = "kitchen display"
	exampleAppAddress = "192.0.2.41"
	// exampleVerificationPage is where the sample's person confirms the code.
	exampleVerificationPage = "http://prusa-connect.local:8080/pair"
)

// exampleAppPairingLifetime and exampleAppPairingInterval are what the sample
// shows. The constants themselves belong to internal/platform/auth, which this
// package may not import.
const (
	exampleAppPairingLifetime = 10 * time.Minute
	exampleAppPairingInterval = 5 * time.Second
)

// appPairingExamples are the worked examples for the app pairing surface.
func appPairingExamples() map[string]operationExample {
	return map[string]operationExample{
		"request-app-pairing": {
			Status:  createdCode,
			Request: appPairingRequestBody{AppName: exampleAppName, Scopes: []string{"read", "operate"}},
			Response: issuedAppPairingBody{
				DeviceCode:              exampleDeviceCode,
				UserCode:                exampleUserCode,
				VerificationURI:         exampleVerificationPage,
				VerificationURIComplete: exampleVerificationPage + "?code=" + exampleUserCode,
				ExpiresIn:               int(exampleAppPairingLifetime / time.Second),
				Interval:                int(exampleAppPairingInterval / time.Second),
			},
		},

		"open-page-session": {
			Response: map[string]any{
				"expires_at": exampleTime.Add(time.Hour),
				"cookie": pageSessionCookieBody{
					Name: "pcl_session", Value: exampleCredentialToken, Path: "/",
					Expires: exampleTime.Add(time.Hour), HTTPOnly: true, SameSite: "Strict", Secure: true,
				},
			},
		},

		"collect-app-pairing": {
			Request: collectAppPairingBody{DeviceCode: exampleDeviceCode},
			Response: collectedCredentialBody{
				ID:          exampleCredentialID,
				Name:        exampleAppName,
				Subject:     exampleSubject,
				CreatedAt:   exampleTime,
				ExpiresAt:   exampleTime.Add(credentialExampleLife),
				Scopes:      []string{"read"},
				Origin:      OriginAppPairing,
				Token:       exampleCredentialToken,
				AccessToken: exampleAccessToken,
			},
		},

		"get-app-pairing": {
			Response: AppPairing{
				UserCode:      exampleUserCode,
				AppName:       exampleAppName,
				Scopes:        []string{"read", "operate"},
				ClientAddress: exampleAppAddress,
				ExpiresAt:     exampleTime.Add(exampleAppPairingLifetime),
			},
		},
	}
}
