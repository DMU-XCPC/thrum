package commit

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// One machine, one writer at a time.
//
// Two actors recording at once is a race the tooling settles rather than reports. The lock
// covers the whole of an event — the read that decides what to record, the write, the
// commit and the publication — so a second writer waits and then records its own event on
// top of what the first one did, rather than colliding with it over git's index and
// reporting a failure that did not happen.
//
// It is an advisory lock on a file under var/, held for the life of the process. The
// kernel releases it when the process goes, so a tool that is killed leaves nothing to
// clean up and a lock file left behind is not a lock. Two processes are what has to be
// serialised, because a command is a process; a caller that records from several
// goroutines at once is not covered, and the flag below makes a second call from the same
// process re-entrant rather than a deadlock.
//
// Waiting is bounded. A command that never returns is worse than one that says it could
// not get in, and the work it is waiting for is a commit that takes milliseconds.

const (
	lockWait  = 30 * time.Second
	lockPause = 25 * time.Millisecond
)

var coordination struct {
	mu   sync.Mutex
	file *os.File
	held bool
}

// acquire takes the coordination lock unless this process already has it.
func acquire(l workspace.Layout) error {
	coordination.mu.Lock()
	defer coordination.mu.Unlock()
	if coordination.held {
		return nil
	}

	file, err := os.OpenFile(l.RecordsLock(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening the coordination lock: %w", err)
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			coordination.file, coordination.held = file, true
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			file.Close()
			return fmt.Errorf("taking the coordination lock: %w", err)
		}
		if time.Now().After(deadline) {
			file.Close()
			return fmt.Errorf("another thrum has been recording here for %s: nothing was written, and the records are as they were", lockWait)
		}
		time.Sleep(lockPause)
	}
}
