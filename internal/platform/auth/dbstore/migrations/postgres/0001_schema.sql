-- The people this server holds a password for, the credentials it has issued,
-- and the identity-provider subjects paired to principals here.
--
-- None of these tables has a foreign key into another: an OIDC deployment holds
-- no accounts here and still mints credentials and pairs subjects, and a
-- service account has no row anywhere but the graph.

-- One person. The id is the subject the authorization graph is keyed by and it
-- never changes: renaming somebody must not move their grants.
CREATE TABLE auth_account (
    id                  TEXT NOT NULL PRIMARY KEY,
    -- Stored folded (lower case, trimmed), so the comparison at login is a
    -- plain equality and two accounts cannot differ by case alone.
    username            TEXT NOT NULL,
    display_name        TEXT NOT NULL DEFAULT '',
    -- The argon2id PHC string. It is the only secret here and it is rendered
    -- nowhere.
    password_hash       TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL,
    password_changed_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX auth_account_by_username ON auth_account (username);

-- One credential this server minted: a browser session, an api token, an mqtt
-- token.
--
-- It holds no secret. digest is the SHA-256 of the token's secret half, which
-- is what a verify compares against, so a row somebody reads is not a token
-- they can present.
CREATE TABLE auth_credential (
    public_id         TEXT NOT NULL PRIMARY KEY,
    -- Who the credential speaks for, as the graph keys them.
    subject           TEXT NOT NULL,
    kind              TEXT NOT NULL,
    -- Hex encoded, because a digest that reaches a log or a psql session
    -- unprintable is a digest somebody pastes wrong when they are debugging.
    digest            TEXT NOT NULL,
    issued_at         TIMESTAMPTZ NOT NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    -- The sliding window in seconds, zero when only expires_at applies. A
    -- session slides; an api or mqtt credential does not.
    idle_secs         INTEGER NOT NULL DEFAULT 0,
    last_seen         TIMESTAMPTZ NOT NULL,
    -- The label an api credential was minted under, so a listing can say
    -- which token is which. A session and an mqtt credential carry none.
    name              TEXT NOT NULL DEFAULT '',
    -- The subject's generation when the credential was checked for. The
    -- verifier refuses a row below the subject's current generation in
    -- auth_subject_generation.
    generation        BIGINT NOT NULL DEFAULT 0,
    -- Who minted an api credential, and the minter's generation when the mint
    -- was checked for. A person can mint for a service account, whose row then
    -- names the service account as its subject, so the person's password
    -- change finds the rows they minted through minted_by. Empty for a session
    -- and an mqtt credential.
    minted_by         TEXT NOT NULL DEFAULT '',
    minter_generation BIGINT NOT NULL DEFAULT 0,
    -- The person at the top of the chain that minted the credential, and that
    -- person's generation then. A token minted with a pairing that another
    -- person's stolen session wrote names that other person here, so their
    -- password change ends it, however long the chain.
    root_creator      TEXT NOT NULL DEFAULT '',
    root_generation   BIGINT NOT NULL DEFAULT 0
);

-- A password change ends every credential the person holds, on every transport
-- at once, which is one delete over this index, and the ones they minted or
-- are at the root of, over the two after it.
CREATE INDEX auth_credential_by_subject ON auth_credential (subject);

CREATE INDEX auth_credential_by_minter ON auth_credential (minted_by);

CREATE INDEX auth_credential_by_root ON auth_credential (root_creator);

-- A subject an identity provider names, paired to the principal it acts as on
-- this server. A verified token whose subject has no row is refused, and no row
-- is ever written on first sight: an administrator writes it. A pairing is not
-- a token, has no secret, and outlives every token the subject presents.
CREATE TABLE auth_pairing (
    -- The plugin type that vouches for the subject, prusa-account today, so two
    -- providers that number their subjects alike cannot collide.
    provider           TEXT NOT NULL,
    -- The subject as the token's sub claim spells it.
    subject            TEXT NOT NULL,
    -- The graph subject in full, user:<id> or service_account:<id>. Never a
    -- device: a person and a device are never linked.
    principal          TEXT NOT NULL,
    -- What the administrator wrote to recognise the row in a listing.
    label              TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL,
    -- The principal's generation when the pairing was checked for. A pairing
    -- saved by a session that a password change ended is refused.
    generation         BIGINT NOT NULL DEFAULT 0,
    -- Who wrote the pairing, and that person's generation then. A pairing can
    -- name somebody other than its writer, such as a service account, so the
    -- writer's password change ends it through created_by.
    created_by         TEXT NOT NULL DEFAULT '',
    creator_generation BIGINT NOT NULL DEFAULT 0,
    -- The person at the top of the chain that wrote the pairing, and that
    -- person's generation then, as auth_credential.root_creator.
    root_creator       TEXT NOT NULL DEFAULT '',
    root_generation    BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (provider, subject)
);

CREATE INDEX auth_pairing_by_creator ON auth_pairing (created_by);

CREATE INDEX auth_pairing_by_root ON auth_pairing (root_creator);

-- A counter for each subject that a password change raises. A password write,
-- a reset and a deleted account raise the generation of every spelling of the
-- account, in the same transaction as the write: a session names the bare
-- account id and an api credential names user:<id>, so each has a row.
--
-- A login, a mint and a pairing read the generation before they check the
-- password or the session, and store it on the row they save, so a row saved
-- after the change by a check that ran before it is refused. No clock takes
-- part, so a board whose clock steps back at its first time sync refuses
-- nothing it should accept.
CREATE TABLE auth_subject_generation (
    subject    TEXT NOT NULL PRIMARY KEY,
    generation BIGINT NOT NULL
);
