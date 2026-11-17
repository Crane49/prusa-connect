package manifest

import (
	"fmt"
	"strings"
)

// MaxCalls is how many modules one manifest may name under calls.
const MaxCalls = 16

// maxNamespaceBytes bounds one namespace under calls, as the host bounds a
// namespace it derives.
const maxNamespaceBytes = 253

// readCalls reads the namespaces under calls. Each must be two or more DNS
// labels joined by dots.
func readCalls(tree map[string]any) ([]string, error) {
	calls, err := textList(tree, "", "calls")
	if err != nil {
		return nil, err
	}

	if err = refuseRepeat("calls", calls); err != nil {
		return nil, err
	}

	if len(calls) > MaxCalls {
		return nil, fmt.Errorf("%w: calls names %d modules, and the limit is %d", ErrInvalid, len(calls), MaxCalls)
	}

	for _, namespace := range calls {
		if !validNamespace(namespace) {
			return nil, fmt.Errorf("%w: calls names %q, which is not the namespace of a module, "+
				"such as com.github.acme.slicer", ErrInvalid, namespace)
		}
	}

	return calls, nil
}

// validNamespace reports whether the text is two or more DNS-1123 labels
// joined by dots.
func validNamespace(namespace string) bool {
	if len(namespace) == 0 || len(namespace) > maxNamespaceBytes {
		return false
	}

	labels := strings.Split(namespace, ".")
	if len(labels) < 2 { //nolint:mnd // a host and a name.
		return false
	}

	for _, label := range labels {
		if !dnsLabel(label) {
			return false
		}
	}

	return true
}

// maxLabelBytes is the longest DNS label.
const maxLabelBytes = 63

func dnsLabel(label string) bool {
	if len(label) == 0 || len(label) > maxLabelBytes || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}

	for _, c := range label {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}

	return true
}
