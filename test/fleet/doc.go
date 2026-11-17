// Package fleet is the model and firmware matrix: every printer model the
// capability profiles cover, at every firmware version the field runs, driven
// through the real driver and the real capability resolver.
//
// It answers the question a hand-picked printer cannot: not "does it connect",
// but "did this change break any supported printer on any supported firmware".
// A failure names the cell, so the answer is a model and a version rather than
// a stack trace.
//
// # What a cell is
//
// One (model, firmware) pair. The models come from
// internal/printer/profile/profiles.json and the firmware versions from
// releases.json beside this file, which records the shipping firmware releases
// and which models each was published for. Adding a model is a profile edit and
// adding a firmware version is one line of releases.json. Neither is a new test
// function, which is the property that keeps the matrix honest as the fleet
// grows.
//
// # The two sets
//
// The fast set is one cell per model, at the newest firmware released for it.
// It gates every commit. The full set is every cell, and it runs nightly. The
// difference is firmware coverage and nothing else: both run the same scenario
// battery, so the fast set catches a change that breaks a model and misses one
// that breaks only an older firmware of a model that still passes on its
// newest.
//
// # Why it is in process
//
// Everything runs over net.Pipe inside a testing/synctest bubble, so a printer
// that is away for an hour costs microseconds and nothing is timing-dependent.
// Earlier research: testing/synctest excludes network I/O from durable
// blocking, so a matrix over a real socket cannot be in the fast lane at any
// budget. The lane that does use real sockets is test/e2e, against the built
// binary.
package fleet
