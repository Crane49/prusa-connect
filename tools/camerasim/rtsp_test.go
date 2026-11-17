package camerasim

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRTSPIsSkippedWhenNoToolIsOnThePath(t *testing.T) {
	t.Parallel()

	if path, ok := RTSPAvailable(); ok {
		t.Skip("an RTSP tool is installed here: " + path)
	}

	server := start(t)

	_, err := StartRTSP(t.Context(), server, "")
	require.ErrorIs(t, err, ErrNoRTSPTool)
}

func TestPublishingRefusesWhenOnlyFFmpegIsInstalled(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed here")
	}

	if path, err := exec.LookPath("mediamtx"); err == nil {
		t.Skip("mediamtx is installed here, so there is a server to publish to: " + path)
	}

	server := start(t)

	_, err := StartRTSP(t.Context(), server, "")
	require.ErrorIs(t, err, ErrNoRTSPServer)
}
