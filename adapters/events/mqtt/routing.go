package mqtt

import (
	"fmt"
	"strings"
)

// Action is what a router does with a topic that matched a rule.
type Action string

const (
	// ActionForward publishes the topic, rewritten if the rule names a target.
	ActionForward Action = "forward"
	// ActionDrop discards the message. It is never published.
	ActionDrop Action = "drop"
)

// Route is one rewrite or filter rule.
//
// From is matched against the whole published topic, one segment at a time:
//
//	literal   matched exactly
//	{name}    one segment, captured under name for use in To
//	+         one segment, not captured
//	#         the rest of the topic, and must be the last segment
//
// To is the target template, with {name} substituted from From. An empty To
// forwards the topic unchanged, and To is ignored when Action is drop.
type Route struct {
	From   string
	To     string
	Action Action
}

// Router matches a topic against the rules in order. The first match wins. A
// topic that matches no rule gets the default action.
type Router struct {
	rules         []rule
	defaultAction Action
	warnings      []string
}

// rule is a compiled Route.
type rule struct {
	from     string
	segments []segment
	multi    bool
	to       string
	action   Action
}

// segment is one compiled token of a From pattern. A literal has neither name
// nor wildcard set; `+` sets wildcard; `{name}` sets name.
type segment struct {
	literal  string
	name     string
	wildcard bool
}

// NewRouter compiles rules in order. A rule that does not compile is skipped,
// not fatal. Every skipped or shadowed rule is recorded in Warnings, because a
// processor has no logger.
func NewRouter(routes []Route, defaultAction Action) *Router {
	if defaultAction == "" {
		defaultAction = ActionForward
	}

	r := &Router{defaultAction: defaultAction}

	for i, route := range routes {
		compiled, err := compile(route)
		if err != nil {
			r.warnings = append(r.warnings, fmt.Sprintf("rule %d (%q) skipped: %v", i, route.From, err))

			continue
		}

		r.rules = append(r.rules, compiled)
	}

	r.detectShadowing()

	return r
}

// Route returns the topic to publish, and whether to publish at all.
func (r *Router) Route(topic string) (rewritten string, drop bool) {
	if r == nil {
		return topic, false
	}

	parts := strings.Split(topic, "/")

	for _, rule := range r.rules {
		captures, ok := rule.match(parts)
		if !ok {
			continue
		}

		if rule.action == ActionDrop {
			return "", true
		}

		return rule.rewrite(topic, captures), false
	}

	return topic, r.defaultAction == ActionDrop
}

// Warnings reports the rules that were skipped and the rules an earlier rule
// may shadow. A host reads it once after building the processor.
func (r *Router) Warnings() []string {
	if r == nil {
		return nil
	}

	return append([]string(nil), r.warnings...)
}

// detectShadowing warns when an earlier rule matches a later rule's example
// topic. It tests one synthetic topic per rule, so it does not find every
// unreachable rule.
func (r *Router) detectShadowing() {
	for later := 1; later < len(r.rules); later++ {
		example := strings.Split(syntheticTopic(r.rules[later].from), "/")

		for earlier := range later {
			if _, ok := r.rules[earlier].match(example); !ok {
				continue
			}

			r.warnings = append(r.warnings, fmt.Sprintf(
				"rule %d (%q) may be unreachable: rule %d (%q) already matches %q, check the order",
				later, r.rules[later].from, earlier, r.rules[earlier].from, strings.Join(example, "/"),
			))
		}
	}
}

func (rl rule) match(parts []string) (map[string]string, bool) {
	switch {
	case rl.multi:
		// A trailing # needs at least one more segment: `v1/#` does not match
		// `v1`.
		if len(parts) <= len(rl.segments) || strings.Join(parts[len(rl.segments):], "/") == "" {
			return nil, false
		}
	case len(parts) != len(rl.segments):
		return nil, false
	}

	var captures map[string]string

	for i, seg := range rl.segments {
		part := parts[i]

		switch {
		case seg.name != "":
			if part == "" {
				return nil, false
			}

			if captures == nil {
				captures = make(map[string]string, len(rl.segments))
			}

			captures[seg.name] = part
		case seg.wildcard:
			if part == "" {
				return nil, false
			}
		case part != seg.literal:
			return nil, false
		}
	}

	return captures, true
}

func (rl rule) rewrite(topic string, captures map[string]string) string {
	if rl.to == "" {
		return topic
	}

	result := rl.to
	for name, value := range captures {
		result = strings.ReplaceAll(result, "{"+name+"}", value)
	}

	return result
}

func compile(route Route) (rule, error) {
	action := route.Action
	if action == "" {
		action = ActionForward
	}

	if action != ActionForward && action != ActionDrop {
		return rule{}, fmt.Errorf("unknown action %q, expected %q or %q", action, ActionForward, ActionDrop)
	}

	if route.From == "" {
		return rule{}, errEmptyFrom
	}

	compiled := rule{from: route.From, to: route.To, action: action}
	names := map[string]bool{}

	parts := strings.Split(route.From, "/")
	for i, part := range parts {
		seg, multi, err := compileSegment(part, i, len(parts), route, names)
		if err != nil {
			return rule{}, err
		}

		if multi {
			compiled.multi = true

			break
		}

		compiled.segments = append(compiled.segments, seg)
	}

	for _, name := range placeholderNames(route.To) {
		if !names[name] {
			return rule{}, fmt.Errorf("'to' references {%s}, which 'from' does not capture", name)
		}
	}

	return compiled, nil
}

func compileSegment(part string, index, total int, route Route, names map[string]bool) (segment, bool, error) {
	switch {
	case part == "":
		return segment{}, false, fmt.Errorf(
			"empty segment at position %d, check for a leading, trailing or doubled slash", index)

	case part == "#":
		if index != total-1 {
			return segment{}, false, fmt.Errorf(
				"# must be the last segment, found at position %d of %d", index, total-1)
		}

		if route.To != "" {
			return segment{}, false, errMultiLevelRewrite
		}

		return segment{}, true, nil

	case part == "+":
		return segment{wildcard: true}, false, nil

	case strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}"):
		name := part[1 : len(part)-1]
		if err := validateName(name); err != nil {
			return segment{}, false, err
		}

		if names[name] {
			return segment{}, false, fmt.Errorf("duplicate placeholder {%s}", name)
		}

		names[name] = true

		return segment{name: name}, false, nil

	case strings.ContainsAny(part, "+#{}"):
		return segment{}, false, fmt.Errorf(
			"segment %q mixes literal text with a wildcard: +, # and {name} each take a whole segment", part)

	default:
		return segment{literal: part}, false, nil
	}
}

func validateName(name string) error {
	if name == "" {
		return errEmptyPlaceholder
	}

	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_'):
		case i == 0:
			return fmt.Errorf("placeholder %q must start with a letter, got %q", name, c)
		default:
			return fmt.Errorf("placeholder %q may hold only letters, digits and underscores, got %q", name, c)
		}
	}

	return nil
}

// placeholderNames lists the {name} references in a target template.
func placeholderNames(template string) []string {
	var names []string

	rest := template
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			return names
		}

		rest = rest[open+1:]

		closing := strings.Index(rest, "}")
		if closing < 0 {
			return names
		}

		names = append(names, rest[:closing])
		rest = rest[closing+1:]
	}
}

// syntheticTopic turns a pattern into one concrete topic it matches. A #
// becomes two segments, so it is deeper than a single wildcard.
func syntheticTopic(from string) string {
	parts := strings.Split(from, "/")
	out := make([]string, 0, len(parts)+1)

	for _, part := range parts {
		switch {
		case part == "#":
			out = append(out, "_x_", "_y_")
		case part == "+", strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}"):
			out = append(out, "_x_")
		default:
			out = append(out, part)
		}
	}

	return strings.Join(out, "/")
}
