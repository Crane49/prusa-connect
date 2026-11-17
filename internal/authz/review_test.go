package authz

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// absentPrinterID is an id no fixture directory row carries. Every review of it
// has to read exactly like a review of a printer the caller may not reach.
const absentPrinterID = "p-not-registered"

// TestObjectActionsMatchThePointCheck is the invariant a review surface exists
// to keep: over every principal, every action and every object, the name is in
// the review answer exactly when Can permits it. Without it the review is a
// second authorization implementation, and the one that says yes where Can says
// no is the defect.
func TestObjectActionsMatchThePointCheck(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				for _, id := range append(directoryIDs(), "p-hidden", absentPrinterID) {
					object := deviceOf(t, id)

					held, err := store.ObjectActions(t.Context(), who, object)
					require.NoError(t, err)

					for _, action := range printerActions() {
						allowed, canErr := store.Can(t.Context(), who, action, object)
						require.NoError(t, canErr)
						require.Equalf(t, allowed, slices.Contains(held, action),
							"%s %s on device:%s: review says %v, Can says %v",
							name, action, id, slices.Contains(held, action), allowed)
					}
				}
			})
		}
	})
}

// TestResourceKindActionsMatchThePointCheck is the same invariant on the other
// object type the review answers for. The two masks share no bit, so a review
// that read the wrong table would answer the empty set rather than fail.
func TestResourceKindActionsMatchThePointCheck(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				for _, kind := range ref.ConfigurableKinds() {
					object := kindOf(t, kind)

					held, err := store.ObjectActions(t.Context(), who, object)
					require.NoError(t, err)

					for _, action := range resourceActions() {
						allowed, canErr := store.Can(t.Context(), who, action, object)
						require.NoError(t, canErr)
						require.Equalf(t, allowed, slices.Contains(held, action),
							"%s %s on %s", name, action, object)
					}
				}
			})
		}
	})
}

// TestAListedRowCarriesTheActionsThePointCheckPermits holds the action set the
// listing carries to the same oracle. The row's set comes from the mask the row
// was filtered by, so a query that selected the column of a different join
// would pass the sweep above and fail here.
func TestAListedRowCarriesTheActionsThePointCheckPermits(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				for _, action := range printerActions() {
					rows, err := store.ListDevices(t.Context(), who, action, Page{Limit: maxPageLimit})
					require.NoError(t, err)

					for _, row := range rows {
						require.Containsf(t, row.Actions, action,
							"%s listed device:%s under %s and the row does not carry it",
							name, row.ID, action)

						held, reviewErr := store.ObjectActions(t.Context(), who, deviceOf(t, row.ID))
						require.NoError(t, reviewErr)
						require.Equalf(t, held, row.Actions, "%s device:%s", name, row.ID)
					}
				}
			})
		}
	})
}

// TestTheWalkCarriesTheSameActionsTheListingDoes holds the second directory
// query to the first. The fleet listing reads the walk and never the list, so a
// column added to one statement and not the other would ship an API whose rows
// carry no action set while every list test stayed green.
func TestTheWalkCarriesTheSameActionsTheListingDoes(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, name := range fixturePrincipals {
			who := subjectOf(t, name)

			for _, action := range printerActions() {
				listed, err := store.ListDevices(t.Context(), who, action, Page{Limit: maxPageLimit})
				require.NoError(t, err)

				var walked []DeviceRow

				require.NoError(t, store.WalkDevices(t.Context(), who, action, 3,
					func(row DeviceRow) error {
						walked = append(walked, row)

						return nil
					}))

				if len(listed) == 0 {
					require.Empty(t, walked, "%s %s", name, action)

					continue
				}

				require.Equalf(t, listed, walked, "%s %s", name, action)
			}
		}
	})
}

// TestAnAbsentPrinterReviewsLikeAnUnreachableOne executes
// absence-indistinguishability on the review surface, which earlier research
// names as the property a check endpoint is the obvious way to lose. An id
// the directory does not carry and an id the caller holds nothing on are the
// same answer, and neither is an error.
func TestAnAbsentPrinterReviewsLikeAnUnreachableOne(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		who := subjectOf(t, "user:bob")

		unreachable, err := store.ObjectActions(t.Context(), who, deviceOf(t, "p-alpha"))
		require.NoError(t, err)

		absent, err := store.ObjectActions(t.Context(), who, deviceOf(t, absentPrinterID))
		require.NoError(t, err)

		require.Empty(t, unreachable, "user:bob holds nothing on the registered p-alpha")
		require.Equal(t, unreachable, absent)

		// The printer this caller does reach is what proves the two empties
		// above are the fixture answering and not the query reading nothing.
		reachable, err := store.ObjectActions(t.Context(), who, deviceOf(t, "p-gamma"))
		require.NoError(t, err)
		require.NotEmpty(t, reachable, "user:bob views p-gamma, so the sweep is not blind")
	})
}

// TestTheReviewNamesActionsInTheVocabularyOrder holds the answer to the order
// Actions declares, which is the order the published vocabulary page renders.
// A client rendering the set without sorting it shows the same order this
// server documents.
func TestTheReviewNamesActionsInTheVocabularyOrder(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		held, err := store.ObjectActions(t.Context(), subjectOf(t, "user:root"), deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.NotEmpty(t, held)

		var want []Action

		for _, action := range Actions() {
			if slices.Contains(held, action) {
				want = append(want, action)
			}
		}

		require.Equal(t, want, held)
	})
}

// TestTheReviewRefusesASubjectThatCannotAsk keeps the review on the same
// admission rule the point check has. A userset is a set of principals and an
// org is a place grants are written, and answering either with an empty set
// would report "you may do nothing" for a question nobody may ask.
func TestTheReviewRefusesASubjectThatCannotAsk(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		_, err := store.ObjectActions(t.Context(), Subject{}, deviceOf(t, "p-alpha"))
		requireCode(t, err, CodeUnknownType())

		userset := subjectOf(t, "group:maintenance#member")
		_, err = store.ObjectActions(t.Context(), userset, deviceOf(t, "p-alpha"))
		requireCode(t, err, CodeInvalidSubject())

		org := subjectOf(t, "org:acme")
		_, err = store.ObjectActions(t.Context(), org, deviceOf(t, "p-alpha"))
		requireCode(t, err, CodeInvalidSubject())
	})
}

// TestTheReviewRefusesAnObjectTypeThatCarriesNoActions is the other half of the
// admission rule. A printer group holds grants and carries no action set of its
// own, so a review of one is a caller bug and never the empty answer a denial
// reads as.
func TestTheReviewRefusesAnObjectTypeThatCarriesNoActions(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		who := subjectOf(t, "user:root")

		for _, typ := range []Type{TypeOrg, TypeDeviceGroup, TypeUser, TypeGroup} {
			object, err := NewObject(typ, "anything")
			require.NoError(t, err)

			_, err = store.ObjectActions(t.Context(), who, object)
			requireCode(t, err, CodeUnsupportedObject())
		}
	})
}
