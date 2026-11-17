package connectorapitest

import (
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The rule a driver that reports a print is held to: every print has an id.
//
// A host names the row it keeps for a print, and the two events it publishes
// about it, by the value at /job/id. A driver that reports a file, a progress
// or an outcome and no id therefore reports a print no host can record.

// jobWatchWindow is how long the case reads the state channel for. It is short
// because it is watching a machine that is doing whatever it was doing: a
// driver whose machine is idle reports no print at all, and the case says so
// rather than waiting for one.
const jobWatchWindow = 2 * time.Second

// jobPathPrefix is the part of the tree a print is addressed under.
const jobPathPrefix = "/job/"

// checkJobsCarryAnID reads the state channel and holds every print path the
// driver reported to connectorapi.JobIDRule.
//
// What it does not demonstrate: a machine that printed nothing during the
// window reports nothing, and the case passes with the log saying so. The rule
// is still the driver's to keep, and its own tests are where a print is staged.
func checkJobsCarryAnID(t *testing.T, session connectorapi.Session) {
	t.Helper()

	patches := session.State()
	if patches == nil {
		t.Fatal("State returned a nil channel, which a subscriber ranges over forever")
	}

	reported, held := watchJobPaths(patches)

	if len(reported) == 0 {
		t.Log("this machine reported no print during the case, so nothing here was exercised. " +
			"The rule stands: " + connectorapi.JobIDRule)

		return
	}

	if held {
		return
	}

	t.Errorf("this driver reported a print at %s and never at %s. %s",
		strings.Join(reported, ", "), connectorapi.JobIDPath, connectorapi.JobIDRule)
}

// watchJobPaths reads the channel for one window and returns the print paths
// the driver wrote, and whether the id was among them.
func watchJobPaths(patches <-chan connectorapi.StatePatch) (reported []string, held bool) {
	seen := map[string]bool{}

	window := time.NewTimer(jobWatchWindow)
	defer window.Stop()

	for {
		select {
		case patch, open := <-patches:
			if !open {
				return reported, held
			}

			if !strings.HasPrefix(patch.Path, jobPathPrefix) || seen[patch.Path] {
				continue
			}

			seen[patch.Path] = true

			if patch.Path == connectorapi.JobIDPath {
				held = true

				continue
			}

			reported = append(reported, patch.Path)
		case <-window.C:
			return reported, held
		}
	}
}
