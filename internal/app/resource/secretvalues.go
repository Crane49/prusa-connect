//go:build diagnostics

package resource

import (
	"encoding/json"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// SecretValues are the values this store's declarations hold under a secret
// or url option. A diagnostics bundle removes them from everything it writes.
// For a type this build does not know, every string in its block is returned,
// because no option list says which of them is a credential.
//
// Of an address it returns the password, the raw query, and the path when it
// has more than one segment. Redact keeps the query and the path, and a
// webhook URL or a stream URL can carry its token in either.
func (s *Store) SecretValues() []string {
	var out []string

	for _, declared := range s.Document().Instances {
		options, known := s.declaredOptions(declared)
		out = append(out, Secrets(options, known, declared.Spec.Config)...)
	}

	out = append(out, databasePasswords(s.Document())...)

	// A database left behind keeps its connection string in the overlay. No
	// declaration holds it, so it is read from the overlay here.
	left, err := LeftDatabases(s.paths.Overlay)
	if err == nil {
		for _, held := range left {
			if password := held.Grant().Password(); password != "" {
				out = append(out, password)
			}
		}
	}

	return out
}

// databasePasswords is the password of every module's own database. The
// declaration holds it in the service block and not in the settings block.
func databasePasswords(doc Document) []string {
	var out []string

	for _, declared := range doc.Instances {
		if service := declared.Spec.Service; service != nil {
			if password := service.Database.Password(); password != "" {
				out = append(out, password)
			}
		}
	}

	return out
}

// Secrets is what one settings block holds under the secret and url options
// given, as SecretValues sweeps them.
func Secrets(options []extension.Option, known bool, config json.RawMessage) []string {
	rule := rulesFor(options, known)
	if rule.nothing() {
		return nil
	}

	held, readable := decodeBlock(config)
	if !readable {
		return nil
	}

	var out []string

	var collect func(value any, address bool)

	collect = func(value any, address bool) {
		// A secret option may hold a block, such as a webhook's headers. Every
		// string in it is treated as a credential.
		switch held := value.(type) {
		case map[string]any:
			for _, member := range held {
				collect(member, address)
			}

			return
		case []any:
			for _, member := range held {
				collect(member, address)
			}

			return
		}

		text, isText := value.(string)
		if !isText {
			return
		}

		if !address {
			out = append(out, text)

			return
		}

		parsed, readable := parsedURL(text)
		if !readable {
			out = append(out, text)

			return
		}

		if password, set := parsed.User.Password(); set {
			out = append(out, password)
		}

		if parsed.RawQuery != "" {
			out = append(out, parsed.RawQuery)
		}

		if strings.Count(strings.Trim(parsed.Path, "/"), "/") > 0 {
			out = append(out, parsed.Path)
		}
	}

	masked(held, rule, collect)

	return out
}
