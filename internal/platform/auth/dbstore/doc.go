// Package dbstore keeps this server's people and its credentials in the
// database the host already opened.
//
// It implements store ports and holds no policy. Accounts satisfies
// internal/platform/auth/local.Store. Credentials, Pairings, AppPairings and
// OIDCSubjects satisfy the store ports of internal/platform/auth. Every rule
// about what a password must be, what a credential may do and who may ask is
// decided above, by the packages that declared those ports. This package reads
// rows and writes rows.
//
// # Why the database and not a file
//
// A store kept in memory or in a file beside the database forgets every session
// at a restart, and a deployment that loses the file returns to its first-run
// state, where the next caller to reach the setup path becomes the
// administrator.
//
// The tables are the host's own schema. This package owns their migrations
// (Migrations), and the host applies them under its one ledger. One engine
// selector already decides where the graph, the records and the queue go.
// Keeping an account in the same database as the grants written for it means
// one backup and one restore.
//
// # What is in a row
//
// No secret. An account row holds an argon2id PHC string, which is a hash, and
// a credential row holds the SHA-256 of the token's secret half, which a verify
// compares against. A reader of either table cannot log in with what they read.
package dbstore
