package manifest

import (
	"fmt"
	"slices"
	"strings"
)

// MaxStagedMiB is the largest body a staged route may ask the host to write
// to disk, in MiB.
const MaxStagedMiB = 4096

// MaxRoutes is how many entries one list under routes may hold.
const MaxRoutes = 32

// Routes is the routes block: the routes the host treats differently from
// the module's other routes (ADR-20261120-xys). Every other route is reached
// as ADR-20260827-wmv rule 13 says, by a caller the host authorized.
type Routes struct {
	// Public is the routes the host sends to the module without resolving a
	// caller, so the module reads an empty principal there. It is a request
	// like Permissions: the operator approves each route at install.
	Public []Route
	// Staged is the routes whose request body the host writes into the
	// module's data directory, and the module reads the file instead of the
	// body. A staged route needs needs.data.
	Staged []StagedRoute
}

// Route is one method and one path under the module's mount.
//
// The path starts with a slash and is relative to the mount, as
// routesapi.Request.Path is. A segment written {name} matches any one
// non-empty segment. Every other segment matches itself, and a trailing slash
// is part of the path.
type Route struct {
	// Method is the HTTP method in upper case, such as POST.
	Method string
	// Path is the path pattern, such as /hooks/announce/ or /files/{id}.
	Path string
}

// StagedRoute is a route whose body the host writes to a file.
type StagedRoute struct {
	Route

	// MaxMiB is the largest body the host accepts on the route, in MiB, from
	// 1 to MaxStagedMiB.
	MaxMiB int
}

// String is the route as one line, such as "POST /hooks/announce/". An
// install lists a public route in this shape, and ParseRoute reads it back.
func (r Route) String() string { return r.Method + " " + r.Path }

// Matches reports whether a request with method and a path relative to the
// mount is on this route.
func (r Route) Matches(method, path string) bool {
	if method != r.Method {
		return false
	}

	want := strings.Split(r.Path, "/")
	got := strings.Split(path, "/")

	if len(want) != len(got) {
		return false
	}

	for i, segment := range want {
		if isPlaceholder(segment) {
			if got[i] == "" {
				return false
			}

			continue
		}

		if segment != got[i] {
			return false
		}
	}

	return true
}

// Overlaps reports whether one request could be on both routes: the same
// method, as many segments, and each pair of segments equal or one of them a
// placeholder that the other could fill. A placeholder never matches an empty
// segment, so /a/{id} and /a/ do not overlap.
func (r Route) Overlaps(other Route) bool {
	if r.Method != other.Method {
		return false
	}

	mine := strings.Split(r.Path, "/")
	theirs := strings.Split(other.Path, "/")

	if len(mine) != len(theirs) {
		return false
	}

	for i, segment := range mine {
		switch {
		case segment == theirs[i]:
		case isPlaceholder(segment) && theirs[i] != "":
		case isPlaceholder(theirs[i]) && segment != "":
		default:
			return false
		}
	}

	return true
}

// ParseRoute reads a route written as Route.String writes it, and holds it to
// the rules the manifest holds a route to.
func ParseRoute(line string) (Route, error) {
	method, path, found := strings.Cut(strings.TrimSpace(line), " ")
	if !found {
		return Route{}, fmt.Errorf("%w: the route %q is not a method and a path", ErrInvalid, line)
	}

	route := Route{Method: method, Path: strings.TrimSpace(path)}

	return route, route.check("the route")
}

// routeMethods is every method a route may name.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var routeMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

// check holds a route to the method list and the path grammar. where names
// the entry in a refusal.
func (r Route) check(where string) error {
	if !slices.Contains(routeMethods, r.Method) {
		return fmt.Errorf("%w: %s.method is %q, expected one of %s",
			ErrInvalid, where, r.Method, strings.Join(routeMethods, ", "))
	}

	if !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("%w: %s.path is %q, expected a path starting with /", ErrInvalid, where, r.Path)
	}

	segments := strings.Split(r.Path[1:], "/")

	for i, segment := range segments {
		last := i == len(segments)-1
		if !validSegment(segment, last) {
			return fmt.Errorf("%w: %s.path is %q, and the segment %q is not a name, a {placeholder} "+
				"or a trailing slash", ErrInvalid, where, r.Path, segment)
		}
	}

	return nil
}

// validSegment reports whether one path segment may stand in a route. An
// empty segment is the trailing slash and stands only last. A dot segment is
// refused, because the host refuses a request path that carries one.
func validSegment(segment string, last bool) bool {
	switch {
	case segment == "":
		return last
	case segment == "." || segment == "..":
		return false
	case isPlaceholder(segment):
		return validPlaceholder(segment[1 : len(segment)-1])
	}

	for _, c := range segment {
		if !literalRune(c) {
			return false
		}
	}

	return true
}

// literalRune reports whether a rune may stand in a literal segment: the
// unreserved characters of RFC 3986, so a pattern never needs escaping.
func literalRune(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

func isPlaceholder(segment string) bool {
	return len(segment) >= 2 && segment[0] == '{' && segment[len(segment)-1] == '}'
}

// validPlaceholder reports whether the name between the braces is lower case
// letters, digits and underscores, starting with a letter.
func validPlaceholder(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}

	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}

	return true
}

// routesKeys is every key the routes block may carry.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var routesKeys = []string{"public", "staged"}

// routeKeys is every key a public route may carry.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var routeKeys = []string{"method", "path"}

// stagedKeys is every key a staged route may carry.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var stagedKeys = []string{"method", "path", "max_mib"}

// readRoutes reads the routes block. An absent block is a module whose every
// route takes an authorized caller and a body the wire carries.
func readRoutes(tree map[string]any, needs Needs) (Routes, error) {
	block, present, err := node(tree, "", "routes")
	if err != nil || !present {
		return Routes{}, err
	}

	if err = refuseUnknown(block, "routes", routesKeys); err != nil {
		return Routes{}, err
	}

	var held Routes

	if held.Public, err = readPublic(block); err != nil {
		return Routes{}, err
	}

	if held.Staged, err = readStaged(block); err != nil {
		return Routes{}, err
	}

	// A public route resolves no caller, and a staged one fills the disk up to
	// its limit. Both at once would let anybody fill it, which the operator
	// never approves as one request (ADR-20261120-xys rule 4).
	// A placeholder matches any segment, so the routes are compared as
	// patterns and not as text.
	for _, staged := range held.Staged {
		if slices.ContainsFunc(held.Public, staged.Overlaps) {
			return Routes{}, fmt.Errorf("%w: routes.public and routes.staged both reach %q, and a staged route "+
				"takes a caller", ErrInvalid, staged.String())
		}
	}

	// The host writes a staged body into the data directory, so a module with
	// none has nowhere to receive it (ADR-20261120-xys rule 6).
	if len(held.Staged) > 0 && !needs.Data {
		return Routes{}, fmt.Errorf("%w: routes.staged is set and needs.data is not, and the host writes "+
			"a staged body into the module's data directory", ErrInvalid)
	}

	return held, nil
}

func readPublic(block map[string]any) ([]Route, error) {
	entries, err := routeEntries(block, "public")
	if err != nil {
		return nil, err
	}

	var out []Route

	for index, entry := range entries {
		where := fmt.Sprintf("routes.public[%d]", index)

		route, readErr := readRoute(where, entry, routeKeys)
		if readErr != nil {
			return nil, readErr
		}

		if slices.Contains(out, route) {
			return nil, fmt.Errorf("%w: routes.public names %q twice", ErrInvalid, route.String())
		}

		out = append(out, route)
	}

	return out, nil
}

func readStaged(block map[string]any) ([]StagedRoute, error) {
	entries, err := routeEntries(block, "staged")
	if err != nil {
		return nil, err
	}

	var out []StagedRoute

	for index, entry := range entries {
		where := fmt.Sprintf("routes.staged[%d]", index)

		route, readErr := readRoute(where, entry, stagedKeys)
		if readErr != nil {
			return nil, readErr
		}

		limit, readErr := whole(entry, where, "max_mib")
		if readErr != nil {
			return nil, readErr
		}

		if limit < 1 || limit > MaxStagedMiB {
			return nil, fmt.Errorf("%w: %s.max_mib is %d, expected 1 to %d", ErrInvalid, where, limit, MaxStagedMiB)
		}

		// The host holds a request to the first route it matches, so two routes
		// one request can reach would tell the module a limit the host does not
		// keep.
		for _, held := range out {
			if held.Overlaps(route) {
				return nil, fmt.Errorf("%w: routes.staged names %q and %q, and one request can reach both",
					ErrInvalid, held.String(), route.String())
			}
		}

		out = append(out, StagedRoute{Route: route, MaxMiB: limit})
	}

	return out, nil
}

// routeEntries reads one list under routes and holds it to MaxRoutes.
func routeEntries(block map[string]any, key string) ([]map[string]any, error) {
	entries, err := nodeList(block, "routes", key)
	if err != nil {
		return nil, err
	}

	if len(entries) > MaxRoutes {
		return nil, fmt.Errorf("%w: routes.%s names %d routes, and the limit is %d",
			ErrInvalid, key, len(entries), MaxRoutes)
	}

	return entries, nil
}

func readRoute(where string, entry map[string]any, known []string) (Route, error) {
	if err := refuseUnknown(entry, where, known); err != nil {
		return Route{}, err
	}

	method, err := requiredText(entry, where, "method")
	if err != nil {
		return Route{}, err
	}

	path, err := requiredText(entry, where, "path")
	if err != nil {
		return Route{}, err
	}

	route := Route{Method: method, Path: path}

	return route, route.check(where)
}
