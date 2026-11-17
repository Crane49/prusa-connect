// Package files defines what both ends of the Fractal file distribution
// extension must agree on: identities, listings, names, URLs and join data.
//
// A file is named by the hash of its bytes, and a directory by the hash of
// its listing. A node publishes folders, and a node below fetches an object
// from its parent by that identity.
//
// This package reads no disk and opens no connection. CheckName does not
// check Unicode Normalization Form C. A node that takes names from a disk
// must normalize them first, for example with golang.org/x/text.
package files
