-- A page session is a short-lived api credential that stands in for an app
-- pairing credential in a browser's HttpOnly cookie (ADR-20261112-nay). It
-- names that credential here, and is refused once that credential is gone or
-- expired. Empty for every other row.
ALTER TABLE auth_credential ADD COLUMN parent TEXT NOT NULL DEFAULT '';

-- The page sessions an app pairing credential opened are found by their
-- parent: when the parent opens another, and when it is revoked. Only a page
-- session has a parent, so the index holds those rows alone.
CREATE INDEX auth_credential_parent ON auth_credential (parent) WHERE parent <> '';
