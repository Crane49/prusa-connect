package fault

// The core.auth codes. They redact by default, so a failure does not tell an
// attacker which part was wrong.
var (
	// AuthTokenInvalid refuses a token this server did not issue, or one that
	// has been revoked or has expired.
	AuthTokenInvalid = Register(DomainAuth(), "TOKEN_INVALID", statusUnauthorized, Document(
		"The credential is not one this server accepts",
		"Authenticate again and retry with the token that returns. Quote the trace id if a fresh token is refused too.",
		RetryNever,
		RedactDetail,
	))

	// AuthLoginRefused refuses a sign-in whose username and password name no
	// account here. A wrong password and an unknown username give the same
	// answer, so a caller cannot learn which usernames exist.
	AuthLoginRefused = Register(DomainAuth(), "LOGIN_REFUSED", statusUnauthorized, Document(
		"The username or password is wrong",
		"Type the username and the password again. An administrator sets a new password "+
			"for an account whose password nobody has.",
		RetryNever,
		RedactDetail,
	))

	// AuthSetupClosed refuses first-run setup on a server that already has an
	// administrator.
	AuthSetupClosed = Register(DomainAuth(), "SETUP_CLOSED", statusConflict, Document(
		"First-run setup has already completed on this server",
		"Sign in as an existing user. Setup cannot be reopened over the API.",
		RetryNever,
		ShowDetail,
	))

	// AuthSetupPending refuses every other path on a server whose first run has
	// recorded no decision about who may call it.
	AuthSetupPending = Register(DomainAuth(), "SETUP_PENDING", statusUnavailable, Document(
		"This server has not been set up yet",
		"Open this server in a browser and finish the first run with the setup code from its log, or "+
			"POST /api/v1/setup with that code and the mode this deployment should serve. Every other "+
			"path answers this until one is recorded.",
		RetryNever,
		ShowDetail,
	))

	// AuthSetupCodeRefused refuses a first run that did not send the one-time
	// code this server printed in its log, or the code core.auth.setup_code
	// sets. Without it, whoever reached the port first would own the server.
	AuthSetupCodeRefused = Register(DomainAuth(), "SETUP_CODE_REFUSED", statusForbidden, Document(
		"The setup code is missing or wrong",
		"Send the setup code in the code field. The server prints it in its log when it starts: read it "+
			"with docker logs for a container or journalctl for a service. A restart prints a new code, "+
			"and an install that sets core.auth.setup_code takes that value.",
		RetryNever,
		ShowDetail,
	))

	// AuthSetupCodeLimited refuses a first run from an address that sent too
	// many wrong setup codes in a short time, or from a new address while too
	// many addresses have. It stops a caller guessing the code.
	AuthSetupCodeLimited = Register(DomainAuth(), "SETUP_CODE_LIMITED", statusTooManyRequests, Document(
		"Too many wrong setup codes were sent",
		"Wait for the time the message names, then send the code again. Retry-After gives the same "+
			"wait in seconds. A restart of the server clears the count and prints a new code.",
		RetryAllowed,
		ShowDetail,
		"retry_after_seconds",
	))

	// AuthAccountExists refuses an account whose username another account
	// already has. Usernames are compared case-folded.
	AuthAccountExists = Register(DomainAuth(), "ACCOUNT_EXISTS", statusConflict, Document(
		"An account already holds that username",
		"Choose another username, or reset the password on the account that holds this one.",
		RetryNever,
		ShowDetail,
	))

	// AuthLastAccount refuses removing the only account a server has. Nothing
	// could then log in, and first-run setup would reopen for whoever reached
	// it.
	AuthLastAccount = Register(DomainAuth(), "LAST_ACCOUNT", statusConflict, Document(
		"The last account on this server cannot be removed",
		"Create another account and grant it what this one holds, then remove this one.",
		RetryNever,
		ShowDetail,
	))

	// AuthCredentialRefused refuses minting an api credential: for a subject
	// that cannot hold one, or on a deployment that authenticates nobody and
	// would never check it.
	AuthCredentialRefused = Register(DomainAuth(), "CREDENTIAL_REFUSED", statusUnprocessable, Document(
		"An api credential cannot be minted for that subject on this server",
		"Mint it for a person or a service account, on a deployment that has a login configured.",
		RetryNever,
		ShowDetail,
		"subject",
	))

	// AuthCredentialExpired refuses an api credential asked for with an expiry
	// that has already passed.
	AuthCredentialExpired = Register(DomainAuth(), "CREDENTIAL_EXPIRED", statusUnprocessable, Document(
		"The requested expiry has already passed",
		"Send an expires_at in the future, or omit it to take the default lifetime.",
		RetryNever,
		ShowDetail,
		"expires_at",
	))

	// AuthSessionRequired refuses a caller that presented an api credential
	// where only a person who signed in may act: any act that creates or
	// grants access to this server. A stolen token cannot mint another.
	AuthSessionRequired = Register(DomainAuth(), "SESSION_REQUIRED", statusForbidden, Document(
		"This operation takes a sign-in as a person",
		"Sign in as a person, with your password or with a Prusa Account paired to your account, and repeat the request. An api credential cannot do this, and neither can a Prusa Account token paired to a service account.",
		RetryNever,
		ShowDetail,
	))

	// AuthCredentialUnknown refuses an api credential id this server does not
	// know, or one the caller may not see.
	AuthCredentialUnknown = Register(DomainAuth(), "CREDENTIAL_UNKNOWN", statusNotFound, Document(
		"No api credential has that id",
		"List your credentials and use an id from the listing. A revoked credential is gone and needs no second revocation.",
		RetryNever,
		ShowDetail,
		"credential",
	))
)

// The refusals of a bearer token an external issuer signed. They show their
// detail, unlike TOKEN_INVALID, because the caller can already read the claims
// of its own token.
var (
	// AuthBearerSignature refuses a token whose signature does not verify
	// against any key the issuer publishes, or one signed with an algorithm
	// this server does not accept.
	AuthBearerSignature = Register(DomainAuth(), "BEARER_SIGNATURE", statusUnauthorized, Document(
		"The token's signature does not verify against the issuer's published keys",
		"Sign in at the issuer again and present the token it returns. A token this server accepts is RS256 and signed with a key the issuer publishes.",
		RetryNever,
		ShowDetail,
	))

	// AuthBearerExpired refuses a token past its expiry or not yet valid.
	AuthBearerExpired = Register(DomainAuth(), "BEARER_EXPIRED", statusUnauthorized, Document(
		"The token has expired or is not valid yet",
		"Renew the token at the issuer and retry with the one it returns.",
		RetryNever,
		ShowDetail,
	))

	// AuthBearerIssuer refuses a token another issuer signed.
	AuthBearerIssuer = Register(DomainAuth(), "BEARER_ISSUER", statusUnauthorized, Document(
		"The token names an issuer this server does not accept",
		"Present a token from the issuer this server is configured for. The operator sets it on the identity resource.",
		RetryNever,
		ShowDetail,
	))

	// AuthBearerAudience refuses a token issued for another audience.
	AuthBearerAudience = Register(DomainAuth(), "BEARER_AUDIENCE", statusUnauthorized, Document(
		"The token was issued for another audience",
		"Request a token for the audience this server is configured for, or point the client at the server the token was issued for.",
		RetryNever,
		ShowDetail,
	))

	// AuthBearerUnpaired refuses a verified token whose subject no
	// administrator has paired to a local principal. A subject is never
	// created on first sight.
	AuthBearerUnpaired = Register(DomainAuth(), "BEARER_UNPAIRED", statusForbidden, Document(
		"The token verified, and its subject is not paired to a principal on this server",
		"Ask an administrator to pair the subject at POST /api/v1/auth/pairings with the person or service account it should act as, then retry.",
		RetryNever,
		ShowDetail,
		"provider",
		"subject",
	))

	// AuthBearerKeysUnavailable refuses a token because the issuer's signing
	// keys could not be read, so no signature can be checked.
	AuthBearerKeysUnavailable = Register(DomainAuth(), "BEARER_KEYS_UNAVAILABLE", statusUnavailable, Document(
		"The issuer's signing keys could not be fetched, so the token cannot be checked",
		"Retry after a moment. If it persists, the operator checks that this server reaches the issuer's key set URL.",
		RetryAllowed,
		ShowDetail,
	))

	// AuthPairingExists refuses pairing a subject that is already paired. To
	// change the principal, unpair first.
	AuthPairingExists = Register(DomainAuth(), "PAIRING_EXISTS", statusConflict, Document(
		"That subject is already paired to a principal on this server",
		"Unpair it at DELETE /api/v1/auth/pairings/{provider}/{subject} first, then pair it again.",
		RetryNever,
		ShowDetail,
		"provider",
		"subject",
	))
)
