package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// redacted is what a password is replaced with in a message. It is a fixed
// token, because the real length would help somebody guess the password.
const redacted = "xxxxx"

// keyValuePassword is the password field of lib/pq's key=value DSN form. A
// single-quoted value may hold spaces, so it is matched before the bare form.
//
//nolint:gochecknoglobals // a compiled pattern read at log time, never written.
var keyValuePassword = regexp.MustCompile(`password\s*=\s*('[^']*'|\S+)`)

// RedactedDSN is the connection string with the password taken out, for a log
// line or an error message. Nothing else may print the DSN: a start-up line
// carrying a database password ends up in a journal, a bug report and a
// screenshot.
//
// Both forms lib/pq accepts can hold a password. The URL form can hold one in
// two places: the userinfo, and the query string, which the driver reads as
// connection settings. A DSN this cannot parse is reported as the engine's
// name alone, so no credential is printed.
func (p PostgresEngine) RedactedDSN() string {
	if p.DSN == "" {
		return ""
	}

	if !strings.Contains(p.DSN, "://") {
		return keyValuePassword.ReplaceAllString(p.DSN, "password="+redacted)
	}

	parsed, err := url.Parse(p.DSN)
	if err != nil {
		return EnginePostgres
	}

	if _, set := parsed.User.Password(); set {
		parsed.User = url.UserPassword(parsed.User.Username(), redacted)
	}

	// Re-encoded only when something was replaced: encoding a query rewrites
	// the order and the escaping of every parameter, and an operator has to
	// recognise the DSN in the message.
	if query := parsed.Query(); redactQueryPasswords(query) {
		parsed.RawQuery = query.Encode()
	}

	return parsed.String()
}

// Passwords are the secrets RedactedDSN takes out, as written, so a report
// that must not carry them can search for them anywhere else they turn up.
// The connection a module's database is made with is swept too.
func (p PostgresEngine) Passwords() []string {
	return append(passwordsOf(p.DSN), passwordsOf(p.ModuleAdminDSN)...)
}

// passwordsOf is every secret one connection string carries.
func passwordsOf(dsn string) []string {
	var out []string

	if !strings.Contains(dsn, "://") {
		for _, match := range keyValuePassword.FindAllStringSubmatch(dsn, -1) {
			out = append(out, strings.Trim(match[1], "'"))
		}

		return out
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil
	}

	if password, set := parsed.User.Password(); set {
		out = append(out, password)
	}

	query := parsed.Query()
	for _, name := range queryPasswords {
		out = append(out, query[name]...)
	}

	return out
}

// queryPasswords are the connection settings that carry a secret when a URL
// DSN puts them in its query string. lib/pq reads the query the same way it
// reads a key=value pair, so both of these are working credentials there:
// password is the database password, sslpassword unlocks the client key.
//
//nolint:gochecknoglobals // the closed list this file redacts, read-only.
var queryPasswords = []string{"password", "sslpassword"}

// redactQueryPasswords replaces every secret in a parsed query, reporting
// whether it replaced any.
func redactQueryPasswords(query url.Values) bool {
	var found bool

	for _, name := range queryPasswords {
		if query.Has(name) {
			query.Set(name, redacted)

			found = true
		}
	}

	return found
}

// validateModuleAdmin refuses a module_admin_dsn that is not a postgres:// URL
// naming a host and a database. A module's own connection string is built
// from it, and the URL form is the one the server reads a password out of to
// keep it out of a log. The message never quotes the value, which may hold a
// password.
func (p PostgresEngine) validateModuleAdmin() error {
	if p.ModuleAdminDSN == "" {
		return nil
	}

	parsed, err := url.Parse(strings.TrimSpace(p.ModuleAdminDSN))
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" ||
		strings.Trim(parsed.Path, "/") == "" {
		return fmt.Errorf("%w: persistence.postgres.module_admin_dsn is not a postgres:// or postgresql:// URL "+
			"with a host and a database name", ErrInvalid)
	}

	return nil
}
