package schema

import (
	"bytes"
	"encoding/json"
)

// keyFrame is the state of one open JSON object or array. For an object it has
// the keys read so far, and wantKey says whether the next string is a key.
type keyFrame struct {
	keys     map[string]struct{}
	isObject bool
	wantKey  bool
}

// checkDuplicateKeys returns ErrDuplicateKey when one JSON object in the file
// has the same key twice. encoding/json would keep the last value silently.
// It returns nil for malformed JSON, which the decoder in AddJSON reports.
func checkDuplicateKeys(source string, data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var stack []*keyFrame
	for tok, tokErr := dec.Token(); tokErr == nil; tok, tokErr = dec.Token() {
		switch t := tok.(type) {
		case json.Delim:
			stack = step(stack, t)
		case string:
			if name, ok := takeKey(stack, t); ok {
				if dupErr := noteKey(source, stack, name); dupErr != nil {
					return dupErr
				}
				continue
			}
			valueDone(stack)
		default:
			valueDone(stack)
		}
	}
	return nil
}

// step handles a delimiter token. It pushes a frame for "{" or "[", and pops
// one for "}" or "]", which completes a value of the frame below.
func step(stack []*keyFrame, d json.Delim) []*keyFrame {
	switch d {
	case '{':
		return append(stack, &keyFrame{keys: map[string]struct{}{}, isObject: true, wantKey: true})
	case '[':
		return append(stack, &keyFrame{})
	}
	stack = stack[:len(stack)-1]
	valueDone(stack)
	return stack
}

// takeKey reports whether the string token s is a key of the innermost open
// object, and not a string value.
func takeKey(stack []*keyFrame, s string) (name string, ok bool) {
	if len(stack) == 0 {
		return "", false
	}
	top := stack[len(stack)-1]
	if !top.isObject || !top.wantKey {
		return "", false
	}
	return s, true
}

// noteKey adds a key to the innermost open object. It returns ErrDuplicateKey
// when that object already has the key.
func noteKey(source string, stack []*keyFrame, name string) error {
	top := stack[len(stack)-1]
	if _, dup := top.keys[name]; dup {
		return &LoadError{Source: source, Where: name, Err: ErrDuplicateKey}
	}
	top.keys[name] = struct{}{}
	top.wantKey = false
	return nil
}

// valueDone marks that the value of the current key is complete. When the
// innermost open frame is an object, the next string token is a key.
func valueDone(stack []*keyFrame) {
	if len(stack) == 0 {
		return
	}
	top := stack[len(stack)-1]
	if top.isObject {
		top.wantKey = true
	}
}
