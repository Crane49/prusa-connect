package discovery

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// titles are instance names that need escapes on the wire: the default
// title, a title outside ASCII, and a name a rename made.
var titles = []string{"Prusa Connect", "Tiskárna Průša", "Desk (2)"}

// startAs is an announcer on the fake network under one title, started and
// left to probe and announce.
func startAs(t *testing.T, over *fakeNetwork, title string) {
	t.Helper()

	announcer := fakeAnnouncer(over)
	announcer.config.Instance = title

	announcer.Start(t.Context(), 8080)
	t.Cleanup(func() { announcer.Stop(t.Context()) })
}

// instanceOf is the full instance name of a title under the link service.
func instanceOf(title string) string {
	return escapeLabel(title) + "." + linkService + ".local."
}

// TestAQuestionForTheInstanceIsAnsweredWhateverTheTitle is a question for the
// service record of the instance, asked with the name as another machine
// read it off the wire. The name there carries the escapes of the DNS
// library, and the question is still answered.
func TestAQuestionForTheInstanceIsAnsweredWhateverTheTitle(t *testing.T) {
	t.Parallel()

	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				over := newFakeNetwork()
				startAs(t, over, title)
				settle()

				mark := over.mark()
				over.deliver(question(instanceOf(title), dns.TypeSRV), peer, groupIPv4)

				answered := over.since(mark)
				require.Len(t, answered, 1, "the question for %q got no answer", title)
				require.Equal(t, uint16(8080), findRecord[*dns.SRV](t, answered[0].msg.Answer).Port)
			})
		})
	}
}

// TestAClaimOnTheInstanceIsSeenWhateverTheTitle is another machine that holds
// the instance name. The claim arrives with the escapes of the DNS library,
// and the server still sees it and renames itself.
func TestAClaimOnTheInstanceIsSeenWhateverTheTitle(t *testing.T) {
	t.Parallel()

	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				over := newFakeNetwork()
				startAs(t, over, title)
				synctest.Wait()

				over.deliver(claimOf(title), peer, groupIPv4)
				settle()

				sent := over.all()
				pointer := findRecord[*dns.PTR](t, withName(sent[slices.IndexFunc(sent, isAnnouncement)].msg.Answer,
					linkService+".local."))
				require.Equal(t, title+" (2)", instanceName(pointer.Ptr, linkService+".local."))
			})
		})
	}
}

// TestATieOnTheInstanceIsSeenWhateverTheTitle is another machine probing for
// the instance name at the same moment. Its probe arrives with the escapes of
// the DNS library, and the server still sees the tie and waits.
func TestATieOnTheInstanceIsSeenWhateverTheTitle(t *testing.T) {
	t.Parallel()

	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				over := newFakeNetwork()
				startAs(t, over, title)

				time.Sleep(130 * time.Millisecond)
				synctest.Wait()

				rival := question(instanceOf(title), dns.TypeANY)
				rival.Ns = []dns.RR{&dns.SRV{
					Hdr:  dns.RR_Header{Name: instanceOf(title), Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120},
					Port: 9999, Target: "zzz.local.",
				}}
				over.deliver(rival, peer, groupIPv4)

				time.Sleep(500 * time.Millisecond)
				synctest.Wait()
				require.Equal(t, 1, count(over.all(), isProbe), "the tie on %q was not seen", title)
			})
		})
	}
}

// TestAKnownAnswerHoldsTheAnswerBackWhateverTheTitle is RFC 6762 section 7.1.
// An asker that already holds the pointer with most of its time to live left
// gets no answer with it, however the instance name is written.
func TestAKnownAnswerHoldsTheAnswerBackWhateverTheTitle(t *testing.T) {
	t.Parallel()

	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				over := newFakeNetwork()
				startAs(t, over, title)
				settle()

				ask := question(linkService+".local.", dns.TypePTR)
				ask.Answer = []dns.RR{&dns.PTR{
					Hdr: dns.RR_Header{
						Name:   linkService + ".local.",
						Rrtype: dns.TypePTR,
						Class:  dns.ClassINET,
						Ttl:    4500,
					},
					Ptr: instanceOf(title),
				}}

				mark := over.mark()
				over.deliver(ask, peer, groupIPv4)
				waitOut()

				require.Empty(t, over.since(mark), "a known answer did not hold the answer back for %q", title)
			})
		})
	}
}

// TestTheNameFormsCompareAsOne is the canonical form itself: a name as this
// server writes it and the same name as the DNS library writes it after the
// wire are one name, and case does not matter for ASCII letters.
func TestTheNameFormsCompareAsOne(t *testing.T) {
	t.Parallel()

	for _, title := range titles {
		ours := instanceOf(title)
		heard := roundTrip(question(ours, dns.TypeSRV)).Question[0].Name

		require.True(t, sameName(ours, heard), "%q and %q", ours, heard)
	}

	require.True(t, sameName("DESK.local.", "desk.local."))
	require.False(t, sameName("Průša.local.", "PRŮŠA.local."), "a byte outside ASCII compares exactly")
}
