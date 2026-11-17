// Command vocabcompat holds a published vocabulary's version to its set.
//
// ADR-20260825-bhe rule 3 gives every vocabulary a version so a client, an extension or
// a federated peer can say which set it speaks. Nothing held the rule, and
// fa8f5cc3 took the authorization set from 20 published entries to 22 and left
// the version at 2.0.0. Every gate passed, because the version is written into
// the artefact from the Go constant, so regenerating agrees with itself.
//
// This gate compares the committed artefacts under docs/vocabularies against a
// committed baseline, classifies each difference, and requires the version that
// the classification demands. ADR-20260827-qvk is the rule set.
//
// The artefacts are read rather than the Go tables, for the same reason
// tools/apicompat reads the committed OpenAPI document: each vocabulary already
// has a generator gate that refuses an artefact differing from the code, in both
// directions, so the artefact is the set and a second reader of the Go tables
// would be a second thing to keep in step.
package main
