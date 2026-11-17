// Package wirerpc is the layer above the frame: the JSON-RPC control codec, the
// credit balance, and the session that runs both over one channel.
//
// A child builds a Conn over the channel its host gave it, names the methods
// it answers, and calls Serve. A host builds a Conn over the same channel from
// the other side and calls a method on it.
//
// Stream is an io.ReadWriter. A write waits for the credit the peer granted,
// and a read grants credit back as it consumes. So a receiver that stops
// reading stops the sender.
//
// This package does not launch a child process or dial a socket.
package wirerpc
