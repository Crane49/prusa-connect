package mqtt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteResolvesTopics(t *testing.T) {
	t.Parallel()

	const state = "v1/devices/printers/abc/data/state"

	for name, tc := range map[string]struct {
		routes    []Route
		fallback  Action
		topic     string
		want      string
		wantDrop  bool
		rationale string
	}{
		"no rules forwards unchanged": {
			topic: state, want: state,
		},
		"a named segment is captured and substituted": {
			routes: []Route{{From: "v1/devices/printers/{id}/data/state", To: "printers/{id}/state"}},
			topic:  state, want: "printers/abc/state",
		},
		"a name used twice in the target is substituted twice": {
			routes: []Route{{From: "v1/devices/{id}/data", To: "{id}/data/{id}"}},
			topic:  "v1/devices/abc/data", want: "abc/data/abc",
		},
		"first match wins": {
			routes: []Route{
				{From: "v1/devices/printers/{id}/data/state", To: "first/{id}"},
				{From: "v1/devices/printers/{id}/data/state", To: "second/{id}"},
			},
			topic: state, want: "first/abc",
			rationale: "the order of an operator's rules is the contract",
		},
		"an empty target forwards unchanged": {
			routes: []Route{{From: "v1/devices/printers/{id}/data/state"}},
			topic:  state, want: state,
		},
		"a named segment does not cross a slash": {
			routes: []Route{{From: "v1/devices/printers/{id}/data/{metric}", To: "printers/{id}/{metric}"}},
			topic:  "v1/devices/printers/abc/data/temp/nozzle/current",
			want:   "v1/devices/printers/abc/data/temp/nozzle/current",
		},
		"a plus matches one segment": {
			routes: []Route{{From: "v1/devices/printers/+/data/{metric}", To: "sensors/{metric}"}},
			topic:  state, want: "sensors/state",
		},
		"a plus does not cross a slash": {
			routes: []Route{{From: "v1/devices/printers/+/data/state", To: "printers/state"}},
			topic:  "v1/devices/printers/abc/extra/data/state",
			want:   "v1/devices/printers/abc/extra/data/state",
		},
		"a hash matches a deep tail": {
			routes: []Route{{From: "v1/devices/printers/+/data/#", Action: ActionDrop}},
			topic:  "v1/devices/printers/abc/data/slot/3/temp/current", wantDrop: true,
		},
		"a hash does not match a sibling branch": {
			routes: []Route{{From: "v1/devices/printers/+/data/#", Action: ActionDrop}},
			topic:  "v1/devices/printers/abc/jobs/42/data/state",
			want:   "v1/devices/printers/abc/jobs/42/data/state",
		},
		"a hash alone matches everything": {
			routes: []Route{{From: "#", Action: ActionDrop}},
			topic:  "v1/server/online", wantDrop: true,
		},
		"a hash needs at least one segment under its prefix": {
			routes: []Route{{From: "v1/server/#", Action: ActionDrop}},
			topic:  "v1/server", want: "v1/server",
			rationale: "the shipping engine compiles a trailing # to a segment that must be present",
		},
		"a drop rule discards the message": {
			routes: []Route{{From: "v1/devices/printers/{id}/data/state", Action: ActionDrop}},
			topic:  state, wantDrop: true,
		},
		"a drop rule ignores its target": {
			routes: []Route{{From: "v1/devices/printers/{id}/data/state", To: "ignored", Action: ActionDrop}},
			topic:  state, wantDrop: true,
		},
		"a drop rule before a catch-all wins for the topic it names": {
			routes: []Route{
				{From: "v1/devices/printers/{id}/data/state", Action: ActionDrop},
				{From: "v1/devices/printers/{id}/data/{metric}", To: "printers/{id}/{metric}"},
			},
			topic: state, wantDrop: true,
		},
		"a drop rule before a catch-all leaves other topics to it": {
			routes: []Route{
				{From: "v1/devices/printers/{id}/data/state", Action: ActionDrop},
				{From: "v1/devices/printers/{id}/data/{metric}", To: "printers/{id}/{metric}"},
			},
			topic: "v1/devices/printers/abc/data/material", want: "printers/abc/material",
		},
		"default_action drop discards what no rule matched": {
			fallback: ActionDrop,
			routes:   []Route{{From: "v1/devices/printers/{id}/data/state"}},
			topic:    "v1/devices/printers/abc/data/material", wantDrop: true,
		},
		"default_action drop still forwards what a rule matched": {
			fallback: ActionDrop,
			routes:   []Route{{From: "v1/devices/printers/{id}/data/state"}},
			topic:    state, want: state,
		},
		"a literal segment is matched literally": {
			routes: []Route{{From: "v1/printers.prod/data/state", To: "printers/state"}},
			topic:  "v1/printersXprod/data/state", want: "v1/printersXprod/data/state",
			rationale: "a dot is a character, not a pattern",
		},
		"a rule that does not compile is skipped rather than applied": {
			routes: []Route{{From: "v1/devices/printers/{id}/data/state", To: "x/{id}", Action: "sideways"}},
			topic:  state, want: state,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, drop := NewRouter(tc.routes, tc.fallback).Route(tc.topic)

			assert.Equal(t, tc.wantDrop, drop, tc.rationale)

			if !tc.wantDrop {
				assert.Equal(t, tc.want, got, tc.rationale)
			}
		})
	}
}

func TestARuleThatDoesNotCompileIsReported(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		route   Route
		wantMsg string
	}{
		"empty from":            {route: Route{From: ""}, wantMsg: "must not be empty"},
		"doubled slash":         {route: Route{From: "v1//state"}, wantMsg: "empty segment"},
		"hash not last":         {route: Route{From: "v1/#/state"}, wantMsg: "# must be the last segment"},
		"hash with a target":    {route: Route{From: "v1/#", To: "flat"}, wantMsg: "cannot be combined with #"},
		"unknown action":        {route: Route{From: "v1/state", Action: "sideways"}, wantMsg: "unknown action"},
		"duplicate placeholder": {route: Route{From: "v1/{id}/{id}"}, wantMsg: "duplicate placeholder"},
		"placeholder digit":     {route: Route{From: "v1/{1id}"}, wantMsg: "must start with a letter"},
		"placeholder symbol":    {route: Route{From: "v1/{id-x}"}, wantMsg: "letters, digits and underscores"},
		"empty placeholder":     {route: Route{From: "v1/{}"}, wantMsg: "placeholder must be named"},
		"mixed segment":         {route: Route{From: "v1/pre{id}"}, wantMsg: "mixes literal text"},
		"unknown target name":   {route: Route{From: "v1/{id}", To: "{other}"}, wantMsg: "'from' does not capture"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			warnings := NewRouter([]Route{tc.route}, "").Warnings()
			require.Len(t, warnings, 1)
			assert.Contains(t, warnings[0], tc.wantMsg)
		})
	}
}

// The startup warning is the half of the routing contract this adapter cannot
// log, because sdk/eventsapi hands a processor no logger. It is reported
// instead, so a host can say it out loud.
func TestAShadowedRuleIsReported(t *testing.T) {
	t.Parallel()

	warnings := NewRouter([]Route{
		{From: "v1/devices/printers/+/data/#", Action: ActionDrop},
		{From: "v1/devices/printers/{id}/data/state", To: "printers/{id}/state"},
	}, "").Warnings()

	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "may be unreachable")
	assert.Contains(t, warnings[0], "rule 0")
}

func TestRulesThatDoNotShadowEachOtherAreQuiet(t *testing.T) {
	t.Parallel()

	assert.Empty(t, NewRouter([]Route{
		{From: "v1/devices/printers/{id}/data/state", To: "printers/{id}/state"},
		{From: "v1/devices/printers/{id}/data/material", To: "printers/{id}/material"},
		{From: "v1/server/online"},
	}, ActionDrop).Warnings())
}

func TestTheZeroRouterForwards(t *testing.T) {
	t.Parallel()

	// A Publisher built without routing configuration still has to publish.
	var router *Router

	got, drop := router.Route("v1/server/online")
	assert.Equal(t, "v1/server/online", got)
	assert.False(t, drop)
	assert.Empty(t, router.Warnings())
}
