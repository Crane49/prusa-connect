-- Every api credential is limited by scopes, and an app can ask a person for
-- one through a pairing (ADR-20261025-tpp).

-- The scopes, separated by one space, in the order the authorization graph
-- declares them. Every api credential has at least one; a session and an mqtt
-- credential have none. An api credential minted before this file is limited to
-- read, which is the narrowest scope.
ALTER TABLE auth_credential ADD COLUMN scopes TEXT NOT NULL DEFAULT '';

-- How an api credential was made: mint for one a person minted and copied by
-- hand, app_pairing for one an app received through a pairing. Empty for every
-- other kind.
ALTER TABLE auth_credential ADD COLUMN origin TEXT NOT NULL DEFAULT '';

UPDATE auth_credential SET scopes = 'read', origin = 'mint' WHERE kind = 'api';

-- One app asking for a credential, from the moment it asks until it collects
-- the answer or the request expires. A row is deleted when the app collects
-- it, so a code is used once.
CREATE TABLE auth_app_pairing (
    -- The SHA-256 of the code the app polls with, hex encoded. The code itself
    -- is kept nowhere, so a row somebody reads is not a code they can poll
    -- with.
    device_digest      TEXT NOT NULL PRIMARY KEY,
    -- The short code a person types or confirms in the browser.
    user_code          TEXT NOT NULL,
    -- The name the app gave itself, shown to the person who decides.
    app_name           TEXT NOT NULL,
    -- The scopes the app asked for, separated by one space.
    scopes             TEXT NOT NULL,
    -- The address the request came from, shown to the person who decides.
    client_address     TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMP NOT NULL,
    expires_at         TIMESTAMP NOT NULL,
    -- How long the app must wait between two polls, in seconds. A poll that
    -- comes sooner raises it.
    interval_secs      INTEGER NOT NULL,
    polled_at          TIMESTAMP NOT NULL,
    -- pending, approved or denied.
    state              TEXT NOT NULL,
    -- Who approved or denied it, as the graph keys them, and the scopes they
    -- approved, which are the ones asked for or fewer.
    decided_by         TEXT NOT NULL DEFAULT '',
    decided_scopes     TEXT NOT NULL DEFAULT '',
    -- The generations of the person who decided and of the root of their
    -- sign-in, read before the decision. A password change between the
    -- decision and the collection leaves the credential refused.
    decided_generation INTEGER NOT NULL DEFAULT 0,
    decided_root       TEXT NOT NULL DEFAULT '',
    root_generation    INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX auth_app_pairing_by_user_code ON auth_app_pairing (user_code);

CREATE INDEX auth_app_pairing_by_expiry ON auth_app_pairing (expires_at);
