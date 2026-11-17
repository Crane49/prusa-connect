package routes

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// Asking the host about a person: which printers they may observe, from the
// host's own listing, and whether they may observe or operate one printer,
// with each answer kept for a while.

// Access asks the host about a person.
type Access interface {
	Check(ctx context.Context, params accessapi.CheckParams) (accessapi.Decision, error)
}

// Seen is the host's listing of devices, which answers for the person a
// principal names only the devices they may observe.
type Seen interface {
	List(ctx context.Context, params deviceapi.ListParams) (deviceapi.Page, error)
}

// visible is the ids of the devices by may observe, read from the host's
// listing a page at a time: a few calls for a fleet of hundreds, where
// asking about each printer would be one call per printer.
func (s *server) visible(ctx context.Context, by string) (map[string]bool, error) {
	ids := map[string]bool{}

	for offset := 0; ; {
		page, err := s.seen.List(ctx, deviceapi.ListParams{
			Principal: by, Limit: deviceapi.MaxPageLimit, Offset: offset,
		})
		if err != nil {
			return nil, err
		}

		for _, d := range page.Items {
			ids[d.ID] = true
		}

		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			return ids, nil
		}
	}
}

// keepFor is how long the host's answer is kept.
const keepFor = 30 * time.Second

// The device actions the module asks about.
const (
	actionObserve = "observe"
	actionOperate = "operate"
)

type operator struct{ person, action, device string }

type kept struct {
	allowed bool
	until   time.Time
}

// operators keeps the host's answer for each person, action and printer.
type operators struct {
	access Access

	mu   sync.Mutex
	kept map[operator]kept
}

// may answers whether person may take action on device. A failed question
// is answered as an error and is not kept.
func (o *operators) may(ctx context.Context, person, action, device string) (bool, error) {
	key, now := operator{person, action, device}, time.Now()

	o.mu.Lock()
	k, ok := o.kept[key]
	o.mu.Unlock()

	if ok && now.Before(k.until) {
		return k.allowed, nil
	}

	decision, err := o.access.Check(ctx, accessapi.CheckParams{
		Subject: person, Action: action, ObjectType: "device", ObjectID: device,
	})
	if err != nil {
		return false, fmt.Errorf("ask whether %s may %s %s: %w", person, action, device, err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	// A sweep on each write drops the answers past keepFor. The host limits
	// no question about the person in the request, so the map holds up to
	// people x printers x actions asked about within keepFor.
	for other, answer := range o.kept {
		if !now.Before(answer.until) {
			delete(o.kept, other)
		}
	}

	o.kept[key] = kept{allowed: decision.Allowed, until: now.Add(keepFor)}

	return decision.Allowed, nil
}

// undecided answers a question the host could not answer.
func undecided(w http.ResponseWriter, r *http.Request, err error) {
	fmt.Fprintf(os.Stderr, "materials: %s %s: %v\n", r.Method, r.URL.Path, err)
	refuse(w, AccessUnknown, AccessUnknown.Title+". Try the same request again in a moment.", nil)
}

// rights answers whether by may observe the printer and whether they may
// operate it. Operate is asked first: every role and every credential scope
// of the host that holds operate on a device holds observe too
// (internal/authz deviceMask and operateMask), so a yes answers both.
func (s *server) rights(ctx context.Context, by, device string) (observe, operate bool, err error) {
	operate, err = s.operates.may(ctx, by, actionOperate, device)
	if err != nil || operate {
		return operate, operate, err
	}

	observe, err = s.operates.may(ctx, by, actionObserve, device)

	return observe, false, err
}
