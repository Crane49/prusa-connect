package discovery

import (
	"bytes"
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/miekg/dns"
)

// legacyCopies are records as a plain DNS client reads them: no cache-flush
// bit, and ten seconds to live at most. They are copies, because the records
// themselves are kept for the next answer.
func legacyCopies(records []dns.RR) []dns.RR {
	out := make([]dns.RR, 0, len(records))

	for _, rr := range records {
		copied := dns.Copy(rr)
		copied.Header().Class &^= topBit
		copied.Header().Ttl = min(copied.Header().Ttl, legacyTimeToLive)
		out = append(out, copied)
	}

	return out
}

// shared reports whether a record may be held by more than one machine,
// which is a record without the cache-flush bit.
func shared(rr dns.RR) bool {
	return rr.Header().Class&topBit == 0
}

func isAddress(rr dns.RR) bool {
	return rr.Header().Rrtype == dns.TypeA || rr.Header().Rrtype == dns.TypeAAAA
}

// addressIn reports whether an address record carries one of the addresses.
func addressIn(rr dns.RR, addresses []netip.Addr) bool {
	var raw []byte

	switch rr := rr.(type) {
	case *dns.A:
		raw = rr.A
	case *dns.AAAA:
		raw = rr.AAAA
	}

	addr, ok := netip.AddrFromSlice(raw)

	return ok && slices.Contains(addresses, addr.Unmap())
}

// bounded is the records of some sections, cut to maxRecords in all.
func bounded(sections ...[]dns.RR) []dns.RR {
	var out []dns.RR

	for _, section := range sections {
		room := maxRecords - len(out)
		if room <= 0 {
			break
		}

		out = append(out, section[:min(len(section), room)]...)
	}

	return out
}

// recordKey names a record by its name, type and data, with every name in
// canonical form and without the cache-flush bit or the time to live. Two
// records with one key are the same record.
func recordKey(rr dns.RR) string {
	name := canonical(rr.Header().Name) + " " + strconv.Itoa(int(rr.Header().Rrtype)) + " "

	switch rr := rr.(type) {
	case *dns.A:
		return name + string(rr.A.To4())
	case *dns.AAAA:
		return name + string(rr.AAAA.To16())
	case *dns.PTR:
		return name + canonical(rr.Ptr)
	case *dns.SRV:
		return name + strconv.Itoa(int(rr.Priority)) + " " + strconv.Itoa(int(rr.Weight)) + " " +
			strconv.Itoa(int(rr.Port)) + " " + canonical(rr.Target)
	case *dns.TXT:
		return name + strings.Join(rr.Txt, "\x00")
	default:
		return name + string(rdata(rr))
	}
}

// withName is the records that carry one name.
func withName(records []dns.RR, name string) []dns.RR {
	var out []dns.RR

	name = canonical(name)

	for _, rr := range records {
		if canonical(rr.Header().Name) == name {
			out = append(out, rr)
		}
	}

	return out
}

// compareSets orders two sets of records as RFC 6762 section 8.2 does: each
// set sorted, then compared record by record by class, type and data, and a
// longer set wins over one it begins with.
func compareSets(ours, theirs []dns.RR) int {
	ours, theirs = slices.Clone(ours), slices.Clone(theirs)
	slices.SortFunc(ours, compareRecords)
	slices.SortFunc(theirs, compareRecords)

	for i := range min(len(ours), len(theirs)) {
		if order := compareRecords(ours[i], theirs[i]); order != 0 {
			return order
		}
	}

	return cmp.Compare(len(ours), len(theirs))
}

// compareRecords orders two records by class, type and data.
func compareRecords(a, b dns.RR) int {
	return cmp.Or(
		cmp.Compare(a.Header().Class&^topBit, b.Header().Class&^topBit),
		cmp.Compare(a.Header().Rrtype, b.Header().Rrtype),
		bytes.Compare(rdata(a), rdata(b)),
	)
}

// rdata is a record's data as it goes on the wire, without compression.
func rdata(rr dns.RR) []byte {
	rr = dns.Copy(rr)
	buf := make([]byte, dns.Len(rr))

	end, err := dns.PackRR(rr, buf, 0, nil, false)
	if err != nil {
		return nil
	}

	return buf[end-int(rr.Header().Rdlength) : end]
}
