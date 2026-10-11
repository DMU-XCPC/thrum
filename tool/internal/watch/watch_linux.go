//go:build linux

package watch

import (
	"os"
	"sync"
	"syscall"
)

// The events that mean the directories are worth looking at again. A message is delivered
// by writing a temporary neighbour and renaming it into place, so IN_MOVED_TO is the one
// that matters — and the others are there because a mailbox can also be created, emptied
// or removed, and every one of those is a reason to look.
const mask = syscall.IN_CREATE | syscall.IN_MOVED_TO | syscall.IN_MOVED_FROM |
	syscall.IN_DELETE | syscall.IN_CLOSE_WRITE | syscall.IN_ATTRIB

// watchMany watches every candidate that exists, on one kernel queue.
//
// It uses the standard library's inotify directly rather than a watching library: this is
// the whole of what is needed, it is a handful of system calls, and a workspace tool that
// has to be droppable onto a machine with nothing but git should not grow a dependency for
// it.
//
// refresh is what makes the chain work: it adds a watch for any candidate that has
// appeared since the last pass, and the caller runs it on every event. Everything is on
// one file descriptor, so this costs a watch descriptor rather than a process.
func watchMany(paths []string) (<-chan struct{}, func() error, func(), error) {
	// Non-blocking, and wrapped in an *os.File, for one reason: a blocking read in one
	// goroutine is *not* woken by closing the descriptor in another, so the old reader
	// stays parked in the kernel — and when the kernel hands the same descriptor number
	// to the next watcher, that stale reader consumes its events. Reading through the
	// runtime's poller removes the race: closing wakes the reader instead of abandoning
	// it, so no reader outlives the descriptor it was reading.
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, nil, nil, err
	}
	queue := os.NewFile(uintptr(fd), "inotify")

	var mu sync.Mutex
	watched := map[string]bool{}
	refresh := func() error {
		mu.Lock()
		defer mu.Unlock()
		// A directory that has gone takes its watch descriptor with it, so it has to be
		// forgotten here or it would never be watched again when it comes back.
		for path := range watched {
			if !exists(path) {
				delete(watched, path)
			}
		}
		// Adding a watch is not instantaneous, and a directory can be created underneath
		// one in the meantime — which the kernel reports to the parent's watch, added a
		// moment too late. Going round until a pass adds nothing is what closes that
		// window: whatever exists when the last pass ends is watched, and whatever appears
		// later appears under something that was.
		for {
			added := 0
			for _, path := range paths {
				if watched[path] || !exists(path) {
					continue
				}
				if _, err := syscall.InotifyAddWatch(fd, path, mask); err != nil {
					continue
				}
				watched[path] = true
				added++
			}
			if added == 0 {
				break
			}
		}
		if len(watched) == 0 {
			return errNoWatch
		}
		return nil
	}
	if err := refresh(); err != nil {
		_ = queue.Close()
		return nil, nil, nil, err
	}

	changed := make(chan struct{}, 1)
	var once sync.Once
	go func() {
		defer close(changed)
		buffer := make([]byte, 4096)
		for {
			n, err := queue.Read(buffer)
			if n > 0 {
				// One signal per batch is enough: the caller looks at the directories
				// rather than at what the kernel said, so the events are not needed.
				select {
				case changed <- struct{}{}:
				default:
				}
				continue
			}
			if err != nil {
				return // the watch was closed, or the queue will not report any more
			}
		}
	}()

	return changed, refresh, func() { once.Do(func() { _ = queue.Close() }) }, nil
}
