package httpapi

import "time"

// credentialExampleLife is how far out the minting example puts its expiry:
// ninety days, the default an api credential is minted with. The constant
// itself belongs to internal/platform/auth, which this package may not import,
// so the example carries a plain duration.
const credentialExampleLife = 90 * 24 * time.Hour

// exampleCredentialID is shaped like a real public id: 22 base62 characters.
// The token beside it has the shape of a minted one and is not one, because a
// usable token in a published example would be a leaked credential.
const (
	exampleCredentialID    = "7Kp2mQ9xLw4nRt8vBc3zYe"                                                        //nolint:gosec // the shape of a public id, not a credential
	exampleCredentialToken = "pcl_7Kp2mQ9xLw4nRt8vBc3zYe_0000000000000000000000000000000000000000000_000000" //nolint:gosec // the shape of a token, not one
)

// credentialLastUsed is when the listing example's first credential was last
// presented, a day after it was minted.
var credentialLastUsed = exampleTime.Add(24 * time.Hour)

// credentialExamples are the worked examples for the api credential surface.
func credentialExamples() map[string]operationExample {
	return map[string]operationExample{
		"create-credential": {
			Status: createdCode,
			Request: credentialBody{
				Name:   "nightly export",
				Scopes: []string{"read"},
			},
			Response: createdCredentialBody{
				ID:        exampleCredentialID,
				Name:      "nightly export",
				Subject:   exampleSubject,
				CreatedAt: exampleTime,
				ExpiresAt: exampleTime.Add(credentialExampleLife),
				Scopes:    []string{"read"},
				Origin:    OriginMint,
				Token:     exampleCredentialToken,
			},
		},

		"list-credentials": {Response: map[string]any{"items": []APICredential{
			{
				ID:         exampleCredentialID,
				Name:       "nightly export",
				Subject:    exampleSubject,
				CreatedAt:  exampleTime,
				ExpiresAt:  exampleTime.Add(credentialExampleLife),
				LastUsedAt: &credentialLastUsed,
				Scopes:     []string{"read"},
				Origin:     OriginMint,
			},
			{
				ID:        "Xw3bN8qLp5tR2mKv7cJz1H",
				Name:      "kitchen display",
				Subject:   exampleSubject,
				CreatedAt: colleagueTime,
				ExpiresAt: colleagueTime.Add(credentialExampleLife),
				Scopes:    []string{"read", "operate"},
				Origin:    OriginAppPairing,
			},
		}}},
	}
}
