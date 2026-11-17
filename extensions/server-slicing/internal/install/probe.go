package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/prusa3d/server-slicing/internal/catalog"
	"github.com/prusa3d/server-slicing/internal/sandbox"
)

// ErrProbe reports an executable that did not say which PrusaSlicer it is.
var ErrProbe = errors.New("the executable did not name a PrusaSlicer version")

// probeTimeout bounds one --help. The first start of a large build on a slow
// card takes a while.
const probeTimeout = 2 * time.Minute

// maxProbeOutput bounds what a probe reads of the help text.
const maxProbeOutput = 64 << 10

// banner is the first line of the help of every PrusaSlicer release this module
// supports, such as "PrusaSlicer-2.9.6 based on Slic3r".
var banner = regexp.MustCompile(`PrusaSlicer-(\d+\.\d+\.\d+(?:-[A-Za-z0-9.]+)?)`)

// Probe runs the executable with --help and answers the version it names. It
// is how a binary an operator names in local_binaries is told apart, and how an
// install checks that it unpacked the version its manifest named.
func Probe(ctx context.Context, binary string, args []string) (catalog.Version, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	arguments := append(append([]string{}, args...), "--help")

	command := exec.CommandContext(ctx, binary, arguments...)

	output := &capped{limit: maxProbeOutput}
	command.Stdout = output
	command.Stderr = output

	home, err := os.MkdirTemp("", "slicer-probe-")
	if err != nil {
		return catalog.Version{}, fmt.Errorf("make a home for the probe: %w", err)
	}

	defer func() { _ = os.RemoveAll(home) }() // a scratch directory; a leftover is harmless

	command.Env = sandbox.Env(home)
	sandbox.Prepare(command)

	// The help ends with a status that differs between releases, so the
	// banner is the answer and the status is not.
	runErr := command.Run()

	found := banner.FindSubmatch(output.Bytes())
	if found == nil {
		if runErr != nil {
			return catalog.Version{}, fmt.Errorf("%w: %w", ErrProbe, runErr)
		}

		return catalog.Version{}, ErrProbe
	}

	version, err := catalog.ParseVersion(string(found[1]))
	if err != nil {
		return catalog.Version{}, fmt.Errorf("%w: %w", ErrProbe, err)
	}

	return version, nil
}

// capped keeps the first limit bytes written to it and drops the rest.
type capped struct {
	bytes.Buffer

	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.limit - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(room, len(p))])
	}

	return len(p), nil
}
