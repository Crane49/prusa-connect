package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// server is one server process in its own folder, on this machine or in a
// container.
type server struct {
	binary   string
	folder   string
	platform string
	image    string
	// container is the name of the container, when the server runs in one.
	container string
	logPath   string
	process   *exec.Cmd
	logFile   *os.File
}

func newServer(binary, folder, platform, image string) *server {
	return &server{
		binary:    binary,
		folder:    folder,
		platform:  platform,
		image:     image,
		container: "wizard-smoke-" + strconv.Itoa(os.Getpid()),
		logPath:   filepath.Join(folder, "server.log"),
	}
}

func (s *server) start(ctx context.Context) error {
	if s.platform != "" {
		return s.startContainer(ctx)
	}

	logFile, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open the server's log: %w", err)
	}

	// Not CommandContext: on Windows the restart is a new process that
	// outlives this one, and stop finds it by its file name.
	//nolint:gosec,noctx // the binary is the one this run copied
	cmd := exec.Command(s.binary, "-addr", ":"+strconv.Itoa(apiPort))
	cmd.Dir = s.folder
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err = cmd.Start(); err != nil {
		_ = logFile.Close() // the start error is the one to report

		return fmt.Errorf("start %s: %w", s.binary, err)
	}

	s.process, s.logFile = cmd, logFile

	return nil
}

func (s *server) startContainer(ctx context.Context) error {
	data := filepath.Join(s.folder, "data")
	if err := os.MkdirAll(data, 0o750); err != nil {
		return fmt.Errorf("make the container's data folder: %w", err)
	}

	// The container runs as an unprivileged user, as a release is meant to.
	//nolint:gosec // a scratch folder the container's user has to write into
	if err := os.Chmod(data, 0o777); err != nil {
		return fmt.Errorf("open the data folder to the container: %w", err)
	}

	api, printers := strconv.Itoa(apiPort), strconv.Itoa(printerPort)

	//nolint:gosec // every argument is this run's own
	out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--name", s.container,
		"--platform", s.platform, "--user", "1000:1000",
		"-v", s.binary+":/app/pcl:ro", "-v", data+":/data", "-w", "/data",
		"-p", api+":"+api, "-p", printers+":"+printers,
		s.image, "/app/pcl", "-addr", ":"+api).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// log is what the server wrote so far.
func (s *server) log(ctx context.Context) string {
	if s.platform != "" {
		//nolint:gosec // the container is this run's own
		out, err := exec.CommandContext(ctx, "docker", "logs", s.container).CombinedOutput()
		if err != nil {
			return "docker logs: " + err.Error()
		}

		return string(out)
	}

	body, err := os.ReadFile(s.logPath)
	if err != nil {
		return ""
	}

	return string(body)
}

// stop ends the server, however many times it restarted itself, and waits for
// its port to close.
func (s *server) stop(ctx context.Context) {
	if s.platform != "" {
		// A container that is already gone has nothing left to stop.
		//nolint:gosec // the container is this run's own
		_ = exec.CommandContext(ctx, "docker", "stop", "-t", "20", s.container).Run()
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", s.container).Run() //nolint:gosec // as above

		return
	}

	stopProcess(ctx, s.process, filepath.Base(s.binary))

	if s.logFile != nil {
		_ = s.logFile.Close() // the server is gone, and so is anything it would write
		s.logFile = nil
	}

	waitClosed(ctx, apiPort, stopLimit)
}

// waitClosed waits until nothing answers on the port.
func waitClosed(ctx context.Context, port int, limit time.Duration) {
	var dialer net.Dialer

	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return
		}

		_ = conn.Close() // a probe connection carries nothing

		if !pause(ctx) {
			return
		}
	}
}

// pause waits one poll, and reports false when the run was cancelled meanwhile.
func pause(ctx context.Context) bool {
	timer := time.NewTimer(pollEvery)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
