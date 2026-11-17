// Package node implements one node of the Fractal protocol, which connects
// servers and devices in a tree.
//
// An application registers with a Node the interfaces it owns and calls
// Interface.Report when a property value changes. The Node keeps a shadow of
// what the nodes below reported and an ancestor shadow of what the nodes
// above reported. It forwards messages between the node above and the nodes
// below, and lets the application call a method or request a property change
// on a node below.
//
// The Node contains no network code. A transport package implements Binding
// and calls Node.Deliver for every message it receives.
package node
