package app

// The labels view over tags. A client written against key and value labels
// keeps working while it moves to tags. Nothing stores a label. Every label is
// read from a tag and written as one, so both views always agree.
//
// A label with an empty value is the tag spelled as its key. A label with a
// value is the tag "key: value". The database migration converted the stored
// labels with the same rule, so a label set reads back unchanged.

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

const (
	// labelValueSeparator joins a label's key and value into one tag.
	labelValueSeparator = ": "

	// maxLabelPrefix, maxLabelName and maxLabelValue are the label grammar's
	// limits in characters. A key is an optional dotted prefix, a slash and a
	// name, and each part is built from DNS names.
	maxLabelPrefix = 253
	maxLabelName   = 63
	maxLabelValue  = 63

	// labelPrefixSeparator divides a key's prefix from its name.
	labelPrefixSeparator = "/"
)

// SetLabels replaces the whole tag list with the tags these labels spell, and
// returns the labels the device now carries. It takes what SetTags takes.
func (d *devices) SetLabels(
	ctx context.Context, principal string, id string, labels map[string]string,
) (map[string]string, error) {
	spelled := make([]string, 0, len(labels))

	for _, key := range slices.Sorted(maps.Keys(labels)) {
		spelled = append(spelled, tagOfLabel(key, labels[key]))
	}

	cleaned, err := normaliseTags("body.labels", spelled)
	if err != nil {
		return nil, err
	}

	held, err := d.writeTags(ctx, principal, id, "body.labels", cleaned)
	if err != nil {
		return nil, err
	}

	return labelsOfTags(held), nil
}

// tagOfLabel spells one label as a tag.
func tagOfLabel(key, value string) string {
	if value == "" {
		return key
	}

	return key + labelValueSeparator + value
}

// labelsOfTags reads a tag list as labels. A tag holding the separator splits
// at its first one. When two tags name one key, the later tag wins, because a
// map holds one value per key.
func labelsOfTags(tags []string) map[string]string {
	out := make(map[string]string, len(tags))

	for _, tag := range tags {
		key, value, _ := strings.Cut(tag, labelValueSeparator)
		out[key] = value
	}

	return out
}

// labelSet is what this device carries, read as labels.
func (e *device) labelSet() map[string]string {
	return labelsOfTags(e.tagList())
}

// parseLabelFilter turns the listing's labels parameter into the tag test a
// device has to pass. A clause key=value asks for the tag "key: value". A bare
// key asks for the tag "key" or for any tag that starts with "key: ".
//
// A clause a label could not spell is refused, so a typo does not read as an
// empty fleet.
func parseLabelFilter(raw []string) ([]labelClause, error) {
	out := make([]labelClause, 0, len(raw))

	for _, clause := range raw {
		key, value, exact := strings.Cut(clause, "=")

		if err := validateLabelKey(key); err != nil {
			return nil, httpfault.RequestInvalid("query.labels",
				"each term is a label key, or a key and a value separated by =", err.Error())
		}

		if exact {
			if err := validateLabelValue(key, value); err != nil {
				return nil, httpfault.RequestInvalid("query.labels",
					"each term is a label key, or a key and a value separated by =", err.Error())
			}
		}

		out = append(out, labelClause{key: key, value: value, exact: exact})
	}

	return out, nil
}

// validateLabelKey checks the key grammar. A key is an optional dotted prefix,
// a slash and a name. Every part is a DNS name label.
func validateLabelKey(key string) error {
	prefix, name, prefixed := strings.Cut(key, labelPrefixSeparator)
	if !prefixed {
		prefix, name = "", key
	}

	if strings.Contains(name, labelPrefixSeparator) {
		return fmt.Errorf("the key %q carries more than one %q", key, labelPrefixSeparator)
	}

	if prefixed {
		if prefix == "" || len(prefix) > maxLabelPrefix {
			return fmt.Errorf("the prefix of %q is empty or over %d characters", key, maxLabelPrefix)
		}

		for segment := range strings.SplitSeq(prefix, ".") {
			if err := dnsLabel(segment); err != nil {
				return fmt.Errorf("the prefix of %q: %w", key, err)
			}
		}
	}

	if err := dnsLabel(name); err != nil {
		return fmt.Errorf("the name of %q: %w", key, err)
	}

	return nil
}

// validateLabelValue checks the value grammar. An empty value is allowed.
func validateLabelValue(key, value string) error {
	if value == "" {
		return nil
	}

	if len(value) > maxLabelValue {
		return fmt.Errorf("the value of %q is over %d characters", key, maxLabelValue)
	}

	if !labelEdge(rune(value[0])) || !labelEdge(rune(value[len(value)-1])) {
		return fmt.Errorf("the value of %q starts or ends with something other than a letter or a digit", key)
	}

	for _, char := range value {
		if !labelEdge(char) && char != '-' && char != '_' && char != '.' {
			return fmt.Errorf("the value of %q carries %q", key, char)
		}
	}

	return nil
}

// dnsLabel checks one DNS name label. It is lowercase letters and digits at
// each end, with hyphens allowed between, and at most 63 characters.
func dnsLabel(label string) error {
	if label == "" || len(label) > maxLabelName {
		return fmt.Errorf("%q is empty or over %d characters", label, maxLabelName)
	}

	if !labelEdge(rune(label[0])) || !labelEdge(rune(label[len(label)-1])) {
		return fmt.Errorf("%q starts or ends with something other than a lowercase letter or a digit", label)
	}

	for _, char := range label {
		if !labelEdge(char) && char != '-' {
			return fmt.Errorf("%q carries %q", label, char)
		}
	}

	return nil
}

// labelEdge reports the characters a DNS name label may begin and end with.
func labelEdge(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
}

// labelClause is one clause of the labels filter.
type labelClause struct {
	key   string
	value string
	exact bool
}

// matchesLabels reports whether this device carries a tag for every clause.
// Tags are compared the way the tag filter compares them.
func (e *device) matchesLabels(clauses []labelClause) bool {
	if len(clauses) == 0 {
		return true
	}

	tags := e.tagList()

	for _, clause := range clauses {
		if !clause.matchesOne(tags) {
			return false
		}
	}

	return true
}

// matchesOne reports whether one of the tags satisfies the clause.
func (c labelClause) matchesOne(tags []string) bool {
	if c.exact {
		return containsTag(tags, tagOfLabel(c.key, c.value))
	}

	return slices.ContainsFunc(tags, func(tag string) bool {
		key, _, _ := strings.Cut(tag, labelValueSeparator)

		return sameTag(key, c.key)
	})
}
