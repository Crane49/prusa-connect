//go:build !unix

package main

import "os/exec"

// detach does nothing: there is no session to leave.
func detach(*exec.Cmd) {}
