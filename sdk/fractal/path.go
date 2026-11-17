package fractal

import (
	"iter"
	"strings"
)

// checkNodePath applies the Node Path grammar: valid Node IDs separated by a
// slash, and at most MaxNodePathLength characters for the whole path.
func checkNodePath(s string) error {
	if s == "" {
		return ErrEmpty
	}
	if len(s) > MaxNodePathLength {
		return ErrTooLong
	}
	start := 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != '/' {
			continue
		}
		if i == start {
			return ErrEmptySegment
		}
		if err := checkNodeID(s[start:i]); err != nil {
			return err
		}
		start = i + 1
	}
	return nil
}

// NodePath identifies one node in the whole network. It lists the Node ID of
// every node from the root down to that node, separated by a slash, for example
// "hq/brno/printer-1". The path of a root node is its Node ID. When a node
// joins, the node above it assigns the path.
type NodePath string

// Validate applies the Node Path grammar.
func (p NodePath) Validate() error {
	return nameErr("node path", string(p), checkNodePath(string(p)))
}

// String returns the path unchanged.
func (p NodePath) String() string { return string(p) }

// ParseNodePath validates s and returns it as a NodePath.
func ParseNodePath(s string) (NodePath, error) {
	p := NodePath(s)
	return p, p.Validate()
}

// IsRoot reports whether the path names a root node, a node with no node above
// it.
func (p NodePath) IsRoot() bool {
	return !strings.Contains(string(p), "/")
}

// Parent returns the path of the node directly above this one. ok is false for
// the path of a root node.
func (p NodePath) Parent() (parent NodePath, ok bool) {
	i := strings.LastIndexByte(string(p), '/')
	if i < 0 {
		return "", false
	}
	return p[:i], true
}

// Last returns the last segment of the path, which is the Node ID of the node
// the path names.
func (p NodePath) Last() NodeID {
	i := strings.LastIndexByte(string(p), '/')
	return NodeID(p[i+1:])
}

// Depth returns the number of segments in the path. The path of a root node has
// depth 1, and the empty path has depth 0.
func (p NodePath) Depth() int {
	if p == "" {
		return 0
	}
	return strings.Count(string(p), "/") + 1
}

// Segments returns an iterator over the Node IDs of the path, from the root
// down.
func (p NodePath) Segments() iter.Seq[NodeID] {
	return func(yield func(NodeID) bool) {
		rest := string(p)
		for rest != "" {
			i := strings.IndexByte(rest, '/')
			if i < 0 {
				yield(NodeID(rest))
				return
			}
			if !yield(NodeID(rest[:i])) {
				return
			}
			rest = rest[i+1:]
		}
	}
}

// Child returns the Node Path that the node at p assigns to a node that joins
// it with the Node ID id: p, a slash, and id. It returns an error when p or id
// is invalid and when the result is over MaxNodePathLength.
func (p NodePath) Child(id NodeID) (NodePath, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if err := id.Validate(); err != nil {
		return "", err
	}
	child := string(p) + "/" + string(id)
	if len(child) > MaxNodePathLength {
		return "", nameErr("node path", child, ErrTooLong)
	}
	return NodePath(child), nil
}

// IsAncestorOf reports whether other names a node below p, at any depth: p
// followed by a slash is a prefix of other. It is false when other equals p.
// "hq/a" is an ancestor of "hq/a/b" and not of "hq/ab".
func (p NodePath) IsAncestorOf(other NodePath) bool {
	return len(other) > len(p)+1 && other[len(p)] == '/' && other[:len(p)] == p
}

// Covers reports whether other is p itself or names a node below p. The sender
// field of a message that arrived from the node at p must be covered by p.
func (p NodePath) Covers(other NodePath) bool {
	return p == other || p.IsAncestorOf(other)
}

// NextHop returns the Node ID of the node directly below p to which the node at
// p forwards a downward message addressed to receiver. ok is false when
// receiver does not name a node below p. The node at p then drops the message.
func (p NodePath) NextHop(receiver NodePath) (child NodeID, ok bool) {
	if !p.IsAncestorOf(receiver) {
		return "", false
	}
	rest := receiver[len(p)+1:]
	if i := strings.IndexByte(string(rest), '/'); i >= 0 {
		rest = rest[:i]
	}
	return NodeID(rest), true
}
