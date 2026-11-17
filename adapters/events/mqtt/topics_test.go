package mqtt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// shippingSpec is the AsyncAPI document the shipping server generates from its
// own topic registry, captured into this adapter's testdata. It is read as
// data and never imported. The server that generated it is deleted, so this
// file is the record of what it published, and holding it here is what let
// that server go without taking this gate with it.
const shippingSpec = "testdata/shipping-asyncapi.yaml"

type asyncAPIDocument struct {
	Channels map[string]struct {
		QoS      byte   `yaml:"x-prusa-connect-qos"`
		Retain   bool   `yaml:"x-prusa-connect-retain"`
		Key      string `yaml:"x-prusa-connect-ts-key"`
		Wildcard bool   `yaml:"x-prusa-connect-wildcard"`
	} `yaml:"channels"`
}

// TestTheTopicTableMatchesTheShippingAsyncAPIDocument is the gate that makes
// section 7 an assertion rather than a paragraph. A subscriber to these topics
// cannot tell a rewrite from the original, and the
// only way to keep that true is to compare against what the original publishes
// rather than against a transcription of it.
func TestTheTopicTableMatchesTheShippingAsyncAPIDocument(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Clean(shippingSpec))
	require.NoError(t, err, "the captured contract is this adapter's own testdata, so its absence is a failure")

	var doc asyncAPIDocument
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	ours := make(map[string]Topic, len(Topics))
	for _, topic := range Topics {
		ours[topic.Pattern] = topic
	}

	published := 0

	for pattern, channel := range doc.Channels {
		if channel.Wildcard {
			// Wildcards are subscription helpers for the frontend. Nothing
			// publishes to them.
			continue
		}

		published++

		topic, known := ours[pattern]
		if !assert.True(t, known, "the shipping server publishes %s and this adapter has no topic for it", pattern) {
			continue
		}

		assert.Equal(t, channel.Key, topic.Name, "topic key for %s", pattern)
		assert.Equal(t, channel.QoS, topic.QoS, "QoS for %s", pattern)
		assert.Equal(t, channel.Retain, topic.Retain, "retain for %s", pattern)
	}

	assert.Len(t, Topics, published,
		"this adapter publishes a topic the shipping server does not, which puts a channel into a frozen namespace")
}

func TestTopicsResolveTheirPlaceholders(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "v1/devices/printers/abc/data/state", DeviceState.For("abc"))
	assert.Equal(t, "v1/devices/printers/abc/jobs/12/data/state", JobState.ForJob("abc", 12))
	assert.Equal(t, "v1/devices/printers/abc/data/tools/1/material", ToolMaterial.ForTool("abc", "1"))
	assert.Equal(t, "v1/devices/printers/abc/data/tools/0.3/active", ToolActive.ForTool("abc", "0.3"))
	assert.Equal(t, "v1/server/online", ServerOnline.Pattern)
}

// TestEveryTopicIsInTheTable catches a channel added as a package variable and
// never listed, which would make it invisible to the divergence test above.
func TestEveryTopicIsInTheTable(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, len(Topics))
	for _, topic := range Topics {
		assert.False(t, seen[topic.Name], "topic %q is listed twice", topic.Name)
		seen[topic.Name] = true
	}

	for _, topic := range []Topic{
		ServerOnline, DeviceState, PrinterOnline, Speed,
		BedTemperature, BedTarget, ChamberTemperature,
		AxisX, AxisY, AxisZ, BlockingReasons, CurrentJob, JobProgress,
		JobState, JobProgressByID, JobTimeRemaining, Dialog,
		ToolActive, ToolMaterial, ToolNozzleTemperature, ToolNozzleTarget,
	} {
		assert.True(t, seen[topic.Name], "topic %q is declared but missing from Topics", topic.Name)
	}
}
