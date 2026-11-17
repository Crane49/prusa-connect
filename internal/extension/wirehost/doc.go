// Package wirehost is the host side of the plugin wire. It launches a child
// process, opens the channel to it, and does what the specification requires of
// a host.
//
// The specification is docs/src/content/docs/plugin-wire-protocol.md and the
// frame codec is sdk/wire. This package adds what no codec can: the process,
// the digest verified before it runs, the handshake read off its standard
// output, the version negotiated from that line, and its standard error copied
// into the host's log under the instance name.
//
// A child speaks over the standard input and output pair the host gives it. The
// specification also names a unix socket on Linux and macOS. This package opens
// none, because the pipe pair is the same wire on both systems and needs no
// directory for the host to own.
package wirehost
