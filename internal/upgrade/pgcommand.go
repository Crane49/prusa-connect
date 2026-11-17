package upgrade

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

const postgresPasswordVariable = "PGPASSWORD"

// postgresCommand starts a PostgreSQL client program without the password
// on its command line, where every local user can read it in the process
// list. The password goes to the child alone, in PGPASSWORD. args receives
// the DSN with the password removed.
func postgresCommand(ctx context.Context, program, dsn string, args func(string) []string) *exec.Cmd {
	clean, password := splitPostgresPassword(dsn)
	command := exec.CommandContext(ctx, program, args(clean)...) // #nosec G204 -- operator-configured client program
	environment := make([]string, 0, len(os.Environ())+1)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, postgresPasswordVariable+"=") {
			environment = append(environment, variable)
		}
	}
	if password != "" {
		environment = append(environment, postgresPasswordVariable+"="+password)
	}
	command.Env = environment

	return command
}

// splitPostgresPassword removes the password from a URL or a keyword DSN and
// returns it separately. A DSN it cannot read is returned unchanged.
func splitPostgresPassword(dsn string) (string, string) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return dsn, ""
		}
		var password string
		if parsed.User != nil {
			if secret, set := parsed.User.Password(); set {
				password = secret
				parsed.User = url.User(parsed.User.Username())
			}
		}
		query := parsed.Query()
		if secret := query.Get("password"); secret != "" || query.Has("password") {
			password = secret
			query.Del("password")
			parsed.RawQuery = query.Encode()
		}

		return parsed.String(), password
	}

	return splitKeywordPassword(dsn)
}

// splitKeywordPassword reads the libpq keyword form, key=value pairs with
// single-quoted values and backslash escapes.
func splitKeywordPassword(dsn string) (string, string) {
	var (
		kept     []string
		password string
	)
	rest := strings.TrimSpace(dsn)
	for rest != "" {
		key, value, remaining, ok := nextKeyword(rest)
		if !ok {
			return dsn, ""
		}
		if key == "password" {
			password = value
		} else {
			kept = append(kept, key+"="+quoteKeyword(value))
		}
		rest = strings.TrimSpace(remaining)
	}

	return strings.Join(kept, " "), password
}

func nextKeyword(text string) (string, string, string, bool) {
	equals := strings.IndexByte(text, '=')
	if equals <= 0 {
		return "", "", "", false
	}
	key := strings.TrimSpace(text[:equals])
	rest := strings.TrimLeft(text[equals+1:], " ")
	if !strings.HasPrefix(rest, "'") {
		end := strings.IndexByte(rest, ' ')
		if end < 0 {
			return key, rest, "", true
		}

		return key, rest[:end], rest[end:], true
	}

	var value strings.Builder
	for index := 1; index < len(rest); index++ {
		switch rest[index] {
		case '\\':
			if index+1 < len(rest) {
				index++
				value.WriteByte(rest[index])
			}
		case '\'':
			return key, value.String(), rest[index+1:], true
		default:
			value.WriteByte(rest[index])
		}
	}

	return "", "", "", false
}

func quoteKeyword(value string) string {
	if value != "" && !strings.ContainsAny(value, ` '\`) {
		return value
	}

	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
}
