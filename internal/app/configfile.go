package app

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// configurationName is the file name a release ships the configuration under.
const configurationName = "config.yaml"

// warnMissingConfiguration logs a warning when the configuration file is not
// there. The server still starts, on its defaults, and those declare no
// storage, so a person who put config.yaml in the wrong folder sees a server
// that accepts no upload and no reason why. The warning names the path it
// read and any config.yaml it found in the places a person puts one.
//
// A server the first-run wizard configured has no operator file and needs
// none, so the file the wizard wrote silences the warning.
func warnMissingConfiguration(ctx context.Context, logger *slog.Logger, path string) {
	if fileExists(config.SetupFile(path)) {
		return
	}

	missing, looked, found := configurationHint(path, fileExists, executableDir())
	if !missing {
		return
	}

	attrs := []any{"looked_at", looked}
	if len(found) > 0 {
		attrs = append(attrs, "found", found,
			"fix", "move the file to "+looked+", or start with -config <file>")
	}

	logger.WarnContext(ctx,
		"no configuration file, so the server runs on its defaults and has no storage for uploads", attrs...)
}

// configurationHint reports whether the file at path is missing, the absolute
// path it names, and each config.yaml that exists in the folder the server
// started in, beside the binary, or in a config folder beside the binary.
func configurationHint(path string, exists func(string) bool, binaryDir string) (bool, string, []string) {
	looked, err := filepath.Abs(path)
	if err != nil {
		looked = path
	}

	if exists(looked) {
		return false, looked, nil
	}

	candidates := []string{configurationName}
	if binaryDir != "" {
		candidates = append(candidates,
			filepath.Join(binaryDir, configurationName),
			filepath.Join(binaryDir, "config", configurationName))
	}

	var found []string

	seen := map[string]bool{looked: true}

	for _, candidate := range candidates {
		absolute, absErr := filepath.Abs(candidate)
		if absErr != nil || seen[absolute] {
			continue
		}

		seen[absolute] = true

		if exists(absolute) {
			found = append(found, absolute)
		}
	}

	return true, looked, found
}

// fileExists reports whether a regular file is at path. Any error but "not
// there" counts as there, so a file the server cannot read is reported by the
// loader and not as missing.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}

	return err != nil || !info.IsDir()
}

// executableDir is the folder the running binary is in, and empty when the
// system cannot say.
func executableDir() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}

	return filepath.Dir(executable)
}
