package httpapi

import "time"

// sessionExampleLife is how far out the login example puts its expiry: a week,
// which is the absolute life a session credential is minted with. The constant
// itself belongs to internal/platform/auth, which this package may not import,
// so the example carries a plain duration.
const sessionExampleLife = 7 * 24 * time.Hour

// colleagueTime is when the second account in the listing example was created,
// an hour after the administrator who added them.
var colleagueTime = exampleTime.Add(time.Hour)

// accountExamples are the worked examples for the local account surface. They
// are in this file because examples.go is at the length limit this project
// holds a source file to.
func accountExamples() map[string]operationExample {
	return map[string]operationExample{
		"get-auth-info": {Response: AuthInfo{Provider: "local", Credentials: true}},

		"create-first-account": {
			Status: createdCode,
			Request: firstAccountBody{
				Username:    "owner@example.com",
				DisplayName: "Owner",
				Password:    "a password of at least eight bytes",
				Code:        "K7QD-3M9X-TP2A",
			},
			Response: Account{
				ID:          "operator",
				Username:    "owner@example.com",
				DisplayName: "Owner",
				CreatedAt:   exampleTime,
				Subject:     "user:operator",
			},
		},

		"password-login": {
			Request:  loginBody{Username: "owner@example.com", Password: "a password of at least eight bytes"},
			Response: map[string]any{"expires_at": exampleTime.Add(sessionExampleLife)},
		},

		// The role and the grants behind it are on every row, which is what a
		// user management page shows and changes.
		"list-accounts": {Response: map[string]any{"items": []Account{
			{
				ID:          "operator",
				Username:    "owner@example.com",
				DisplayName: "Owner",
				CreatedAt:   exampleTime,
				Subject:     "user:operator",
				Role:        "admin",
				Grants:      []string{"admin"},
			},
			{
				ID:          exampleAccountID,
				Username:    "colleague@example.com",
				DisplayName: "A Colleague",
				CreatedAt:   colleagueTime,
				Subject:     "user:" + exampleAccountID,
				Role:        "operator",
				Grants:      []string{"operator", "viewer"},
			},
		}}},

		"create-account": {
			Status: createdCode,
			Request: accountBody{
				Username:    "colleague@example.com",
				DisplayName: "A Colleague",
				Password:    "another password of at least eight bytes",
			},
			Response: Account{
				ID:          exampleAccountID,
				Username:    "colleague@example.com",
				DisplayName: "A Colleague",
				CreatedAt:   colleagueTime,
				Subject:     "user:" + exampleAccountID,
			},
		},
	}
}
