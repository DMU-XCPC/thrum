//go:build unix

package daemon

import "syscall"

// Signal zero asks whether a process is there without disturbing it, which is how a
// supervisor answers a question no file can.
var (
	sigZero = syscall.Signal(0)
	sigTerm = syscall.SIGTERM
)
