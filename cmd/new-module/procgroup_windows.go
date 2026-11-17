package main

import (
	"os"
	"os/exec"
)

// takeTerminal does nothing: Windows has no foreground group to take.
func takeTerminal(*os.File) func() { return func() {} }

// ownGroup does nothing: Windows has no process group a signal reaches.
func ownGroup(*exec.Cmd) {}

// interruptGroup ends command, because Windows cannot send it Ctrl+C.
func interruptGroup(command *exec.Cmd) {
	_ = command.Process.Kill() // a process that has gone already is stopped.
}

// endGroup does nothing: command has exited, and it had no group.
func endGroup(*exec.Cmd) {}
