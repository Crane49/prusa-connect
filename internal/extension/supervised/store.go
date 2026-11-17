package supervised

import (
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
)

// storageCallSlots is how many bulkhead slots a storage keeps for calls. An
// upload is a stream paced by its client, and without these slots enough slow
// uploads leave no slot for a list, a download or a delete.
const storageCallSlots = 2

// DefaultStoragePolicy is the host policy for a storage backend: synchronous
// calls, a breaker, no queue. Storage is what a breaker is for: many callers,
// one remote, slow failures, and something useful left when it degrades.
func DefaultStoragePolicy() supervisor.Policy {
	policy := supervisor.DefaultPolicy()
	policy.Queue = supervisor.QueuePolicy{}
	policy.StreamConcurrency = policy.Concurrency - storageCallSlots

	return policy
}
