package app

// The identity index as a test reaches it.
//
// Every helper is here and not beside the index itself because nothing the
// server runs calls one: a production caller either goes through identify, on
// the admission path, or through the approval. Keeping them in a test file is
// what stops a future caller finding a shortcut into the index that skips the
// accounting identify does, and boundTo is the one that matters: a read that
// mints nothing and merges nothing is the read a caller reaches for when it
// wants to decide something about a spelling, and deciding on a spelling
// nobody proved is what BUG-011's review round removed.

import (
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// bindIdentity writes a binding for a whole identity rather than for a unit id
// alone, so a connector whose devices carry no unit id is bound under the same
// key identify would look them up by.
//
// It is what a test does when it has to know a printer's id before the plane
// exists, which is the case an authorizer built over one origin creates.
func (i *deviceIdentities) bindIdentity(id string, identity connectorapi.Identity) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.bindLocked(id, identityKey(identity))
}

// boundTo is the id a spelling already resolves to, and false when this index
// holds no binding for it. It mints nothing and merges nothing.
func (i *deviceIdentities) boundTo(unitUID string) (string, bool) {
	key := identityKeyOf(unitUID)

	i.mu.Lock()
	defer i.mu.Unlock()

	id, bound := i.bound[key]

	return id, bound
}

// idOf is the id this plane addresses a machine by, for a test that has the
// identity a printer arrived with and wants the row it landed in.
//
// It reads the binding rather than deriving anything, so a test using it
// asserts against the id the fleet really minted. A test about identity itself
// reads the id out of the listing instead, because that is what a client sees.
func (d *devices) idOf(identity connectorapi.Identity) string {
	return d.identities.identify(identity)
}
