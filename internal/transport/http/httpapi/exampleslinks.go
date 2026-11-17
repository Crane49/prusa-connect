package httpapi

import "time"

// linkExamples are the worked examples for the links this site holds. They are
// in this file because examples.go is at the length limit this project holds
// a source file to.
//
// One story runs through them. This site joined the site above it on the
// workshop network, accepts other sites under it, and one site has joined and
// answers in under a millisecond.
func linkExamples() map[string]operationExample {
	return map[string]operationExample{
		"list-links": {Response: map[string]any{"items": []LinkInstance{exampleLink()}}},

		"get-link": {Response: exampleLink()},

		"approve-link-node": {Response: exampleLink()},

		"reject-link-node": {Response: exampleLink()},

		"revoke-link-node": {Response: exampleLink()},
	}
}

// exampleLink is one running link with the site above it connected and one site
// under it admitted.
func exampleLink() LinkInstance {
	beat := time.Date(2026, time.September, 11, 9, 41, 12, 0, time.UTC)
	latency := 0.84

	return LinkInstance{
		Name:      exampleLinkName,
		Kind:      "link",
		Type:      "com.github.aux.prusa3d.fractal/fractal",
		Enabled:   true,
		Running:   true,
		Mode:      "parent",
		Enrolment: "inherit",
		Accepting: true,
		NodeID:    exampleNodeID,
		NodePath:  "root.workshop." + exampleNodeID,
		PublicKey: "6c5fd7a0a0a71b1f4c4b5a2ea1d5c84d0d38c6f1b9a9a2b64c8b1f2a3d4e5f60",
		Address:   "0.0.0.0",
		Uplink: &LinkUplink{
			Address:     "nats://parent.workshop.example:4222",
			State:       "connected",
			ConnectedAt: &beat,
		},
		Listeners: []LinkListener{
			{Protocol: "nats", Address: "0.0.0.0", Port: 4222},
		},
		Nodes: []LinkNode{
			{
				ID:            "26c70f1f054e7fdb",
				Role:          "child",
				Path:          "root.workshop." + exampleNodeID + ".26c70f1f054e7fdb",
				Admission:     "admitted",
				PublicKey:     "1f2e3d4c5b6a79880f1e2d3c4b5a69780e1d2c3b4a596877f0e1d2c3b4a59687",
				Address:       "172.22.0.3:35394",
				Alive:         true,
				LastHeartbeat: &beat,
				LatencyMS:     &latency,
			},
		},
	}
}
