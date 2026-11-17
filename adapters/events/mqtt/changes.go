package mqtt

import (
	"maps"
	"strings"
	"sync"
)

// changeFilter implements "publish only on change". It remembers the last
// payload accepted for each topic and refuses an identical one. It compares
// encoded payloads, so a temperature that rounds to the same whole degree is
// not published again.
type changeFilter struct {
	mu   sync.Mutex
	last map[string]string
}

func newChangeFilter() *changeFilter {
	return &changeFilter{last: make(map[string]string)}
}

// accept records a payload and reports whether it differs from the last one
// for that topic. It records before the publish, so a burst of identical
// readings takes one queue slot. Call invalidate when the publish fails.
func (f *changeFilter) accept(topic, payload string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if previous, seen := f.last[topic]; seen && previous == payload {
		return false
	}

	f.last[topic] = payload

	return true
}

// invalidate forgets a topic after a publish that did not reach the broker,
// so the same value is published on the next attempt.
func (f *changeFilter) invalidate(topic string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.last, topic)
}

// forgetPrinter drops everything remembered about one printer, so the map
// does not grow with every printer ever registered.
func (f *changeFilter) forgetPrinter(printerID string) {
	prefix := Base + "/" + printerID + "/"

	f.mu.Lock()
	defer f.mu.Unlock()

	maps.DeleteFunc(f.last, func(topic, _ string) bool {
		return strings.HasPrefix(topic, prefix)
	})
}

// reset forgets everything. The publisher calls it on every connection,
// because a broker that restarted without persistence has lost its retained
// messages and needs every value again.
func (f *changeFilter) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	clear(f.last)
}
