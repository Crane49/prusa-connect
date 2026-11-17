// Package wire is the frame an out-of-process extension and the host
// exchange, and the handshake line that opens the connection.
//
// One frame has a payload length, a stream id, a kind and a payload. Control
// frames contain one JSON-RPC 2.0 object, data frames contain raw bytes, and
// the three remaining kinds end a stream, reset it and grant credit on it.
//
// The published wire protocol specification is the contract. This package is
// one implementation of it.
//
// This package frames bytes. It does not launch a child, dial a socket,
// encode a JSON-RPC object, or account for credit. See sdk/wirerpc.
package wire
