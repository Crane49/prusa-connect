package app

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestARemovalReachesAnOwnerWatchingTheStream runs a module removal's order over
// the real hub and the real graph: the instance stops, and the reconcile takes
// its row out of the directory. The row going takes the origin out of every
// reader's sight, so what reaches the owner is the hub naming it withdrawn.
func TestARemovalReachesAnOwnerWatchingTheStream(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		graph := newGraph(t)
		asset := instanceRef(t, ref.KindAsset, "nozzle-watch")

		require.NoError(t, graph.RegisterResources(t.Context(), asset))

		origin, err := event.NewOrigin(event.Kind(asset.Kind()), asset.ID())
		require.NoError(t, err)

		hub, err := stream.New(stream.Options{
			Site: "local", Interval: stream.MinInterval, Authorizer: visibility{graph: graph}, Logger: discard(),
		})
		require.NoError(t, err)

		defer hub.Close()

		owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
		require.NoError(t, err)

		sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{Principal: owner.String()})
		require.NoError(t, err)

		defer sub.Close()

		require.Contains(t, sub.Origins(), origin)

		publish(discard(), hub, nil, supervisor.Status{
			Ref: asset, Type: "asset", State: condition.StateStopped(), Result: supervisor.ResultStoppedByOperator,
		})
		require.NoError(t, graph.RemoveResource(t.Context(), asset))

		time.Sleep(2 * stream.AuthorizeInterval)
		synctest.Wait()

		var withdrawn []event.Origin

		for drained := false; !drained; {
			select {
			case batch := <-sub.Batches():
				withdrawn = append(withdrawn, batch.Withdrawn...)
			default:
				drained = true
			}
		}

		require.Contains(t, withdrawn, origin, "the owner was not told the removed module left its sight")
	})
}
