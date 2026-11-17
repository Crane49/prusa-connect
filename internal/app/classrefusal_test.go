package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// requireClassRefusal holds the rule a class refusal follows, in one place,
// because four cases in this package assert it and they had all written down
// the older, simpler rule.
//
// The older rule was that every refusal on this surface is the not-found: a
// printer the caller may not act on reads exactly like one this server has
// never been dialled by, so a refusal never tells anybody a printer exists.
// That is still right for a caller who holds nothing. It was wrong for the one
// caller it also caught, which is somebody who already reads the machine: they
// see it in their listing and read it 200 by id, so answering 404 when they ask
// for something they may not do withholds nothing at all. What it does instead
// is send an operator looking for a lost printer when the answer is that they
// hold observe and the operation takes operate.
//
// So the refusal turns on exactly the fact that decides it. reads is whether
// the caller can read this device: if they can, the refusal is the ordinary
// forbidden and names the class; if they cannot, it is the not-found, and
// nothing about what exists is learned from it.
func requireClassRefusal(t *testing.T, err error, reads bool) {
	t.Helper()

	require.Error(t, err)

	if reads {
		require.Truef(t, fault.HasCode(err, authz.CodeAccessDenied()),
			"a caller who reads this printer is refused the class and not told the printer is "+
				"absent, because they are looking at it: %v", err)

		return
	}

	require.Truef(t, fault.HasCode(err, fault.PrinterNotFound.Code()),
		"a caller who reads nothing on this printer is told what a caller asking about a printer "+
			"this server has never been dialled by is told: %v", err)
}
