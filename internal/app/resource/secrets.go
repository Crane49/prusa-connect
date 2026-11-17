package resource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// RedactedValue is what a read returns in place of a credential. It is a
// fixed string, so it does not show the length of the value.
const RedactedValue = "xxxxx"

// Declared gives the store the settings one registered type accepts. The
// option list belongs to the registry, which this package may not import, so
// the caller passes it in.
//
// It returns false for a name this build has no type for. Every value of a
// block declared for such a name is then replaced, because the store cannot
// tell which of its keys is a credential, and the block is still returned to
// everybody who may view the instance.
type Declared interface {
	DeclaredOptions(kind ref.Kind, typeName string) ([]extension.Option, bool)
}

// declaredOptions is the option list for one declaration: the module's own,
// recorded at install, or the registered type's. The second result is false
// for a type the build has no implementation for, for example a misspelt type
// name.
//
// A store built with no Declared has no type table. It answers true with no
// options, because it does not know that the type is unknown. Every
// deployment passes a Declared.
func (s *Store) declaredOptions(
	declared Declaration,
) ([]extension.Option, bool) {
	if declared.Spec.Module != nil {
		return optionsOf(declared.Spec.Module.Options), true
	}

	if s.declared == nil {
		return nil, true
	}

	return s.declared.DeclaredOptions(declared.Ref.Kind(), declared.Spec.Type)
}

// optionsOf converts a module's recorded options to extension.Option values.
// The two types carry the same fields, and the install record is a copy of
// the manifest the module shipped.
func optionsOf(recorded []install.Option) []extension.Option {
	out := make([]extension.Option, 0, len(recorded))
	for _, option := range recorded {
		out = append(out, extension.Option{
			Key:      option.Key,
			Type:     option.Type,
			Required: option.Required,
			Default:  option.Default,
			Summary:  option.Summary,
		})
	}

	return out
}

// rules says what to mask in one instance's settings: the keys its type
// declared, or every value.
type rules struct {
	// all says this build has no implementation for the type, so it cannot
	// tell which keys are credentials, and every value is masked.
	all bool
	// secrets is the keys whose whole value is a credential.
	secrets map[string]bool
	// addresses is the keys declared as a URL. A URL can contain a password.
	addresses map[string]bool
}

// rulesFor reads one option list. known is false for a type this build has no
// implementation for.
func rulesFor(options []extension.Option, known bool) rules {
	out := rules{all: !known, secrets: make(map[string]bool), addresses: make(map[string]bool)}

	for _, option := range options {
		switch option.Type {
		case extension.OptionSecret:
			out.secrets[option.Key] = true
		case extension.OptionURL:
			out.addresses[option.Key] = true
		case extension.OptionString, extension.OptionInt, extension.OptionNumber,
			extension.OptionBool, extension.OptionDuration, extension.OptionList,
			extension.OptionObject:
		}
	}

	return out
}

// nothing reports whether these rules mask no value.
func (r rules) nothing() bool { return !r.all && len(r.secrets) == 0 && len(r.addresses) == 0 }

// Redact returns one settings block as a caller outside this process may read
// it.
//
// A value the type declared as a credential is replaced by RedactedValue. An
// address the type declared as a URL keeps its host and path, and its
// password is replaced. A declared key is masked wherever it is written: at
// the top of the block, inside a nested block, and inside a block in a list.
//
// known is false for a type this build has no implementation for, for example
// a misspelt type name. Every value in such a block is replaced and the keys
// are kept. No option list says which key holds a credential, and the block
// is still returned to everybody who may view the instance.
//
// A block that does not parse is answered as an empty object. A stored
// declaration always parses, because it is canonicalised at load. Returning
// the raw bytes of a block that does not parse could return a credential.
func Redact(options []extension.Option, known bool, config json.RawMessage) json.RawMessage {
	rule := rulesFor(options, known)
	if rule.nothing() || len(bytes.TrimSpace(config)) == 0 {
		return config
	}

	held, readable := decodeBlock(config)
	if !readable {
		return json.RawMessage(emptyObject)
	}

	encoded, err := json.Marshal(masked(held, rule, nil))
	if err != nil {
		return json.RawMessage(emptyObject)
	}

	return encoded
}

// masked walks one decoded block and replaces the values a reader may not
// see. When found is not nil, it is called with every value the walk masks
// and whether the value is a declared address. SecretValues uses it to
// collect exactly the values Redact hides.
func masked(value any, rule rules, found func(value any, address bool)) any {
	if found == nil {
		found = func(any, bool) {}
	}

	switch held := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(held))

		for key, member := range held {
			switch {
			case rule.secrets[key]:
				found(member, false)
				out[key] = RedactedValue
			case rule.addresses[key]:
				found(member, true)
				out[key] = maskedAddress(member)
			default:
				out[key] = masked(member, rule, found)
			}
		}

		return out
	case []any:
		out := make([]any, 0, len(held))
		for _, member := range held {
			out = append(out, masked(member, rule, found))
		}

		return out
	}

	if rule.all {
		found(value, false)

		return RedactedValue
	}

	return value
}

// ErrSecretUnknown reports the marker written for a credential this instance
// has no stored value for.
var ErrSecretUnknown = errors.New("resource: the marker was written for a credential nothing is stored for")

// Restore puts the stored credential back where the caller sent the marker.
//
// A client reads an instance, changes one key and writes the whole block
// back. It then sends the marker for every credential it did not change. If
// the store saved that, the marker would become the password. The marker
// therefore means "keep what is stored", at every depth Redact masks. A
// client can keep a credential over a write only in this way, because it can
// never read the value.
//
// The marker written where no value is stored is ErrSecretUnknown. Storing
// the marker would give the instance a password no operator chose.
func Restore(
	options []extension.Option, known bool, written, stored json.RawMessage,
) (json.RawMessage, error) {
	rule := rulesFor(options, known)
	if rule.nothing() {
		return written, nil
	}

	desired, readable := decodeBlock(written)
	if !readable {
		return written, nil
	}

	held, _ := decodeBlock(stored)

	restored, err := restored(desired, held, rule)
	if err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(restored)
	if err != nil {
		return nil, err
	}

	return encoded, nil
}

// decodeBlock reads one settings block and keeps every number as it was
// written, so a restored block is still in the canonical form the store
// compares by bytes. It reports false for a block that is empty or does not
// parse.
func decodeBlock(raw json.RawMessage) (any, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var held any
	if err := decoder.Decode(&held); err != nil {
		return nil, false
	}

	return held, true
}

// restored walks the written block and the stored block together and puts
// back the values the caller could not read.
func restored(written, stored any, rule rules) (any, error) {
	block, isBlock := written.(map[string]any)
	if !isBlock {
		return restoredList(written, stored, rule)
	}

	under, _ := stored.(map[string]any)
	out := make(map[string]any, len(block))

	for key, value := range block {
		switch {
		case rule.secrets[key], rule.all && value == RedactedValue:
			kept, err := keptSecret(key, value, under[key])
			if err != nil {
				return nil, err
			}

			out[key] = kept
		case rule.addresses[key]:
			kept, err := keptAddress(key, value, under[key])
			if err != nil {
				return nil, err
			}

			out[key] = kept
		default:
			member, err := restored(value, under[key], rule)
			if err != nil {
				return nil, err
			}

			out[key] = member
		}
	}

	return out, nil
}

// restoredList walks a written list and the stored list together, member by
// member and by position, so a credential inside a list entry is kept like
// one inside a block.
func restoredList(written, stored any, rule rules) (any, error) {
	members, isList := written.([]any)
	if !isList {
		return written, nil
	}

	under, _ := stored.([]any)
	out := make([]any, 0, len(members))

	for i, member := range members {
		var beside any
		if i < len(under) {
			beside = under[i]
		}

		restoredMember, err := restored(member, beside, rule)
		if err != nil {
			return nil, err
		}

		out = append(out, restoredMember)
	}

	return out, nil
}

// keptSecret returns the value for one credential key: the stored value when
// the marker was written, and the written value otherwise.
func keptSecret(key string, written, stored any) (any, error) {
	if written != RedactedValue {
		return written, nil
	}

	if stored == nil {
		return nil, fmt.Errorf("%w: %q", ErrSecretUnknown, key)
	}

	return stored, nil
}

// keptAddress returns the value for one declared URL. An address whose
// password is the marker gets the stored password, and the rest of the
// address is what the caller wrote.
//
// The marker where no password is stored is ErrSecretUnknown, as for a
// credential key. The instance could not connect with xxxxx as its password.
func keptAddress(key string, written, stored any) (any, error) {
	text, isText := written.(string)
	if !isText {
		return written, nil
	}

	// A value that is declared as a URL and is not one is kept as written.
	// The type that reads it reports the error.
	address, parsed := parsedURL(text)
	if !parsed || address.User == nil {
		return written, nil
	}

	password, hasPassword := address.User.Password()
	if !hasPassword || password != RedactedValue {
		return written, nil
	}

	held, heldPassword := storedPassword(stored)
	if !held {
		return nil, fmt.Errorf("%w: the password inside %q", ErrSecretUnknown, key)
	}

	address.User = url.UserPassword(address.User.Username(), heldPassword)

	return address.String(), nil
}

// storedPassword is the password inside the address this instance holds, and
// false for an instance holding none.
func storedPassword(stored any) (bool, string) {
	kept, isKept := stored.(string)
	if !isKept {
		return false, ""
	}

	held, parsed := parsedURL(kept)
	if !parsed || held.User == nil {
		return false, ""
	}

	password, written := held.User.Password()
	if !written {
		return false, ""
	}

	return true, password
}

// parsedURL reads one address, and reports false for text that is not a URL.
func parsedURL(text string) (*url.URL, bool) {
	address, err := url.Parse(text)
	if err != nil {
		return nil, false
	}

	return address, true
}

// maskedAddress is one declared URL with its password replaced, so an
// operator can still read which broker or database the instance talks to.
//
// A value that is not a string, or not a URL, is returned unchanged. The type
// that reads the key refuses such a value.
func maskedAddress(value any) any {
	text, isText := value.(string)
	if !isText {
		return value
	}

	address, err := url.Parse(text)
	if err != nil || address.User == nil {
		return value
	}

	if _, written := address.User.Password(); !written {
		return value
	}

	address.User = url.UserPassword(address.User.Username(), RedactedValue)

	return address.String()
}
