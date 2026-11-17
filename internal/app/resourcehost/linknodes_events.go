package resourcehost

import (
	"encoding/json"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// The changes the host derives from one reading of a link.
//
// A module sends the whole reading and never a difference, and it makes no call
// of its own for a server joining or going quiet. So the host compares the
// reading it received with the reading before it. A module therefore needs no
// new call, and it cannot report a server twice or report one going quiet that
// never did.
//
// The first reading of a process reports nothing. The host has no reading to
// compare it with, and publishing one joined frame per server every time this
// server restarts would fill a webhook with servers that joined months ago.

// nodeFrame is one event the comparison produced: the type and the body.
type nodeFrame struct {
	eventType string
	key       string
	body      any
}

// nodeChanges compares two readings of one link and returns the events the
// difference produced. The events for the servers come first, in the order of
// the reading: a server that joined, a connection that closed or opened
// again. The refusal of this site's own request to join comes last.
//
// before is the zero Status on the first reading of a process, and firstReading
// says so. Nothing is published then.
func nodeChanges(
	instance string, before, now linkapi.Status, firstReading bool,
) []nodeFrame {
	if firstReading {
		return nil
	}

	held := make(map[linkapi.NodeID]linkapi.Node, len(before.Nodes))
	for _, node := range before.Nodes {
		held[node.ID] = node
	}

	frames := make([]nodeFrame, 0, len(now.Nodes)+1)

	for _, node := range now.Nodes {
		was, known := held[node.ID]
		if !known {
			frames = append(frames, joinedFrame(instance, node))

			continue
		}

		if was.Connected != node.Connected {
			frames = append(frames, presenceFrame(instance, node))
		}
	}

	if denial := deniedFrame(instance, before.Uplink, now.Uplink); denial != nil {
		frames = append(frames, *denial)
	}

	return frames
}

// joinedFrame is the event for a server this site had no record of.
func joinedFrame(instance string, node linkapi.Node) nodeFrame {
	return nodeFrame{
		eventType: event.TypeLinkNodeJoined,
		key:       instance + "/" + node.ID.String(),
		body: event.LinkNodeJoined{
			Link:      instance,
			Node:      node.ID.String(),
			Path:      node.Path.String(),
			Admission: node.Admission.String(),
			PublicKey: node.PublicKey,
		},
	}
}

// presenceFrame is the event for a connection that closed or opened again.
func presenceFrame(instance string, node linkapi.Node) nodeFrame {
	eventType := event.TypeLinkNodeLost
	if node.Connected {
		eventType = event.TypeLinkNodeReturned
	}

	return nodeFrame{
		eventType: eventType,
		key:       instance + "/" + node.ID.String(),
		body: event.LinkNodePresence{
			Link:      instance,
			Node:      node.ID.String(),
			Connected: node.Connected,
			LastSeen:  node.LastSeen,
		},
	}
}

// deniedFrame is the event for the server above refusing this site's request to
// join. It returns nil while the state was already denied, so a link that
// reports the same refusal every few seconds produces one frame.
func deniedFrame(instance string, before, now *linkapi.Uplink) *nodeFrame {
	if now == nil || now.State != linkapi.UplinkDenied {
		return nil
	}

	if before != nil && before.State == linkapi.UplinkDenied {
		return nil
	}

	return &nodeFrame{
		eventType: event.TypeLinkNodeDenied,
		key:       instance,
		body: event.LinkJoinDenied{
			Link:      instance,
			Address:   now.Address,
			Code:      now.Code,
			FaultCode: now.FaultCode,
			Reason:    now.Reason,
		},
	}
}

// publishNodeChanges sends the events one comparison produced. The caller holds
// the publish mutex, because the event hub takes the events of one origin one
// at a time.
//
// A frame that cannot be encoded is skipped and the rest are sent. The whole
// reading was already published, so a reader still has the correct state, and
// one bad body must not stop the events of the other servers.
func publishNodeChanges(emitter bus.Emitter, frames []nodeFrame) error {
	for _, frame := range frames {
		body, err := json.Marshal(frame.body)
		if err != nil {
			continue
		}

		if err = emitter.Emit(bus.Emission{
			Type: frame.eventType,
			Key:  frame.key,
			Body: string(body),
		}); err != nil {
			return fmt.Errorf("publish %s: %w", frame.eventType, err)
		}
	}

	return nil
}
