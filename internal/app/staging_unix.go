//go:build unix

package app

import "syscall"

// openNoWait makes an open of a FIFO return at once instead of waiting for a
// writer. A regular file reads the same with it.
const openNoWait = syscall.O_NONBLOCK
