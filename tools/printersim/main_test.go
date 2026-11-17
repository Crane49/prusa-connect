package main

import (
	"testing"
	"time"
)

func TestJobFrames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		printFor, cadence time.Duration
		want              int
	}{
		{5 * time.Minute, time.Second, 300},
		{0, time.Second, 0},
		{time.Minute, 0, 0},
		{time.Millisecond, time.Second, 1},
	}

	for _, c := range cases {
		if got := jobFrames(c.printFor, c.cadence); got != c.want {
			t.Errorf("jobFrames(%v, %v) = %d, want %d", c.printFor, c.cadence, got, c.want)
		}
	}
}
