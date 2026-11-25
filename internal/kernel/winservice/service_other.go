//go:build !windows

package winservice

import (
	"context"
	"errors"
	"os"
)

// errNotWindows is returned by the calls that only Windows can answer.
var errNotWindows = errors.New("a Windows service runs only on Windows")

// Detect reports false: only Windows has a service control manager.
func Detect() (bool, error) { return false, nil }

// Run refuses: only Windows has a service control manager.
func Run(func(context.Context) int) (int, error) { return 0, errNotWindows }

// RedirectStandardError refuses: a Linux service has systemd's journal.
func RedirectStandardError(string) (*os.File, error) { return nil, errNotWindows }
