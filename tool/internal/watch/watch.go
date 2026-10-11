// Package watch reports that a directory has changed.
//
// Where the system can tell us, it does: a mailbox that appears, a message that arrives,
// and a message that is removed are all events, and nothing looks at the directory in
// between. Where it cannot — a filesystem that does not report changes, a platform with
// no watching primitive — the same channel is fed by looking again on a timer, and the
// caller cannot tell the difference. That is the point of the shape: what to do about a
// change is written once, and only the waiting differs.
package watch

import (
	"errors"
	"os"
	"sync"
	"time"
)

// pollInterval is how often a directory is looked at again when it cannot be watched.
const pollInterval = 500 * time.Millisecond

// errNoWatch is what a platform with no watching primitive answers, and it sends the
// follower to its timer instead. Saying so is better than a second copy of the waiting
// loop: the loop is written once, and only the waiting differs.
var errNoWatch = errors.New("this system does not report changes to a directory")

// Follower reports that one of several directories inside one workspace has changed.
//
// A mailbox is created by the first message delivered into it, so the directories that
// have to gain it are worth watching too. Every one of them that exists is watched at
// once, and that is deliberate: a watch that *moves* down as each level appears can miss
// the level that appeared while it was moving — the kernel reports a directory being
// created to whoever watches its parent, and only to them. Watching the whole chain has no
// such window: a level appearing later appears under a parent already watched, and the
// event that reports it is the reason to take up the watch for it in turn.
type Follower struct {
	// Candidates are the directories on the way down to what matters, in any order.
	candidates []string

	changed chan struct{}
	stop    chan struct{}
	events  <-chan struct{}
	once    sync.Once

	mu      sync.Mutex
	refresh func() error
	stopW   func()
}

// Follow watches every candidate that exists, and every one that appears later.
//
// The watches are in place before it returns. That is not tidiness: a caller looks at the
// directory and then follows it, and a change landing between the two would otherwise
// never be reported at all.
func Follow(candidates []string) (*Follower, error) {
	if len(candidates) == 0 {
		return nil, errors.New("nothing to watch")
	}
	f := &Follower{
		candidates: candidates,
		changed:    make(chan struct{}, 1),
		stop:       make(chan struct{}),
	}
	if events, refresh, stop, err := watchMany(candidates); err == nil {
		f.events, f.refresh, f.stopW = events, refresh, stop
	} else {
		// Nothing here can be watched. Looking again is not as good as being told, and it
		// is much better than being told nothing.
		f.stopW = pollWatch(&f.events)
		f.refresh = func() error { return nil }
	}
	go f.run()
	return f, nil
}

// Changed reports that something may have happened. It is closed when the follower stops.
func (f *Follower) Changed() <-chan struct{} { return f.changed }

// Close stops watching. It is safe to call more than once.
func (f *Follower) Close() {
	f.once.Do(func() {
		close(f.stop)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.stopW != nil {
			f.stopW()
			f.stopW = nil
		}
	})
}

func (f *Follower) run() {
	defer close(f.changed)
	for {
		select {
		case <-f.stop:
			return
		case _, live := <-f.events:
			if !live {
				// A watch that ended is not news about the mailbox, and one that we
				// ended is not news at all.
				select {
				case <-f.stop:
					return
				default:
				}
				// The filesystem withdrew it, or the kernel ran out of room for watches.
				// Looking again on a timer is worse than an event and much better than
				// silence.
				f.mu.Lock()
				if f.stopW != nil {
					f.stopW()
				}
				f.refresh = func() error { return nil }
				f.stopW = pollWatch(&f.events)
				f.mu.Unlock()
			} else {
				// A directory appeared, so a deeper one may have: take up its watch
				// before reporting, so that what is written next is seen rather than
				// arriving under a directory nobody is watching yet.
				f.mu.Lock()
				refresh := f.refresh
				f.mu.Unlock()
				_ = refresh()
			}

			select {
			case <-f.stop:
				return
			case f.changed <- struct{}{}:
			default:
			}
		}
	}
}

// pollWatch feeds a channel on a timer, and sets it on the follower.
func pollWatch(into *<-chan struct{}) func() {
	changed := make(chan struct{}, 1)
	*into = changed
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(changed)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				select {
				case changed <- struct{}{}:
				default:
				}
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }) }
}

// exists is what a candidate is filtered by.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
