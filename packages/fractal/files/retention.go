package files

import (
	"net/http"
	"strings"
	"time"
)

// Retention is how long a receiver may keep an object. The zero value is
// none.
type Retention struct {
	Unlimited bool
	Until     time.Time
}

// String names the retention in a log line.
func (r Retention) String() string {
	switch {
	case r.Unlimited:
		return "unlimited"
	case r.Until.IsZero():
		return "none"
	default:
		return "until " + r.Until.UTC().Format(http.TimeFormat)
	}
}

// unlimitedDirectives is the one way to write unlimited. A relay passes it on
// unchanged and never lowers max-age.
const unlimitedDirectives = "private, max-age=31536000, immutable"

// unlimited is the retention of every object this server publishes, so a
// server below may keep its copy after the folder is unshared.
var unlimited = Retention{Unlimited: true}

// set writes the directives of r into h.
func (r Retention) set(h http.Header) {
	switch {
	case r.Unlimited:
		h.Set("Cache-Control", unlimitedDirectives)
	case !r.Until.IsZero():
		h.Set("Cache-Control", "private")
		h.Set("Expires", r.Until.UTC().Truncate(time.Second).Format(http.TimeFormat))
	default:
		h.Set("Cache-Control", "no-store")
	}
}

// unlimitedMaxAge is the max-age of unlimitedDirectives.
const unlimitedMaxAge = "max-age=31536000"

// retentionOf reads the retention of an answer. The first rule that applies
// wins: no-store, then Expires, then unlimited, else none.
func retentionOf(h http.Header) Retention {
	directives := map[string]bool{}

	for _, value := range h.Values("Cache-Control") {
		for part := range strings.SplitSeq(value, ",") {
			directives[strings.ToLower(strings.TrimSpace(part))] = true
		}
	}

	if directives["no-store"] {
		return Retention{}
	}

	if expires := h.Get("Expires"); expires != "" {
		if until, err := http.ParseTime(expires); err == nil {
			return Retention{Until: until}
		}

		return Retention{}
	}

	if directives[unlimitedMaxAge] && directives["immutable"] {
		return unlimited
	}

	return Retention{}
}

// restrict returns the more restrictive of r and other: none when either is
// none, else the earlier instant, else unlimited.
func (r Retention) restrict(other Retention) Retention {
	switch {
	case r.none() || other.none():
		return Retention{}
	case r.Unlimited:
		return other
	case other.Unlimited:
		return r
	case other.Until.Before(r.Until):
		return other
	}

	return r
}

func (r Retention) none() bool { return !r.Unlimited && r.Until.IsZero() }
