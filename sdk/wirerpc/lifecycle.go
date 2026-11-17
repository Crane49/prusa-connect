package wirerpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// The lifecycle method set. Every child answers MethodInfo, MethodShutdown and
// MethodPing whatever contract it serves. A host calls MethodInfo before it
// trusts a child with anything, and MethodShutdown before it stops one.
const (
	// MethodInfo asks a child what it is. It takes no params and answers an
	// Info. The host calls it once, after the handshake.
	MethodInfo Method = "plugin.info"

	// MethodShutdown asks a child to stop. It takes no params and answers
	// nothing, and the child closes the channel once it has finished. A child
	// that does not close it is stopped by the host.
	MethodShutdown Method = "plugin.shutdown"

	// MethodPing asks a child whether it is still reading. It takes no params
	// and answers nothing. The Conn answers it itself, so a method set cannot
	// replace it and a blocked handler does not stop it.
	MethodPing Method = "plugin.ping"

	// MethodConfigure gives a child its settings. It takes Settings and
	// answers nothing, or Configured. The host calls it once, after
	// MethodInfo and before any other call, and only on a child that names
	// it in Info.Serves. The host refuses to start a child that does not
	// name it when the operator wrote settings for it.
	MethodConfigure Method = "plugin.configure"
)

// Contract is the version of the lifecycle method set this package publishes.
// A child reports the version it was built against in Info.Contract, and a
// host checks it with extension.Version.CompatibleWith.
const Contract = extension.Version("1.1.0")

// maxInfoNameBytes bounds the name a child reports.
const maxInfoNameBytes = 128

// ErrInfo reports an Info a host cannot use.
var ErrInfo = errors.New("wirerpc: the child reported an unusable info")

// Info is what a child answers MethodInfo with.
type Info struct {
	// Name is the extension's own name, as in its descriptor.
	Name string `json:"name"`
	// Version is the release of the extension itself.
	Version extension.Version `json:"version"`
	// Contract is the extension contract version the child was built against.
	Contract extension.Version `json:"contract"`
	// Serves is the methods the child answers beyond the lifecycle set, and
	// absent for a child that answers none. A host reads it before it makes
	// any other call.
	Serves []Method `json:"serves,omitempty"`
	// OpenAPI is the fragment describing the routes the child serves on
	// routes.serve, with paths relative to its mount. A child that names
	// routes.serve in Serves must send one. routesapi.Describe sets both.
	OpenAPI json.RawMessage `json:"openapi,omitempty"`
}

// Answers reports whether the child named method in Serves. The lifecycle set
// is answered by every child and is never named there.
func (i Info) Answers(method Method) bool {
	return slices.Contains(i.Serves, method)
}

// Validate reports what makes an Info unusable. A host runs it on the answer
// before it reads any field.
func (i Info) Validate() error {
	if i.Name == "" || len(i.Name) > maxInfoNameBytes || !utf8.ValidString(i.Name) {
		return fmt.Errorf("name %q: %w", i.Name, ErrInfo)
	}

	if strings.ContainsFunc(i.Name, func(r rune) bool { return r < 0x21 || r == 0x7f }) {
		return fmt.Errorf("name %q carries a space or a control character: %w", i.Name, ErrInfo)
	}

	if _, _, _, err := i.Version.Parse(); err != nil {
		return fmt.Errorf("version: %w: %w", err, ErrInfo)
	}

	if _, _, _, err := i.Contract.Parse(); err != nil {
		return fmt.Errorf("contract: %w: %w", err, ErrInfo)
	}

	for _, method := range i.Serves {
		if strings.TrimSpace(string(method)) == "" {
			return fmt.Errorf("serves names a blank method: %w", ErrInfo)
		}
	}

	return nil
}
