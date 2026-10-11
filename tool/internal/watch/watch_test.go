package watch_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/watch"
)

// within waits for a change, and fails rather than hanging.
func within(t *testing.T, changed <-chan struct{}, what string) {
	t.Helper()
	select {
	case _, live := <-changed:
		if !live {
			t.Fatalf("%s: the watch stopped", what)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: nothing was reported", what)
	}
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A directory that exists is watched, and something appearing in it is reported.
func TestAChangeIsReported(t *testing.T) {
	dir := t.TempDir()
	follower := follow(t, []string{dir})
	defer follower.Close()

	write(t, filepath.Join(dir, "one"))
	within(t, follower.Changed(), "a file appearing")
}

// A mailbox is created by the first message delivered into it, so there is nothing to watch
// until then. The follower watches the nearest directory that does exist and moves down as
// each level appears — which is what makes the first message ever delivered visible.
func TestAWatchedPathThatDoesNotExistYetIsFollowedDown(t *testing.T) {
	root := t.TempDir()
	mailbox := filepath.Join(root, "msg", "alice")

	follower := follow(t, []string{mailbox, filepath.Join(root, "msg"), root})
	defer follower.Close()

	// The directory appears, which is not itself a message but is a reason to look.
	if err := os.MkdirAll(mailbox, 0o755); err != nil {
		t.Fatal(err)
	}
	within(t, follower.Changed(), "the mailbox appearing")

	// And now the mailbox itself is watched, which is what the message needs.
	write(t, filepath.Join(mailbox, "m-1.md"))
	within(t, follower.Changed(), "a message in the mailbox that appeared")
}

// The guarantee a caller depends on: anything written before a report is there when the
// look that report triggers happens — including a message written while the watch is
// moving. What the caller does is look again after every report, so that is what this
// does, and it never waits between the two writes.
func TestAChangeWhileMovingIsVisibleToTheLookThatFollows(t *testing.T) {
	root := t.TempDir()
	mailbox := filepath.Join(root, "msg", "alice")
	message := filepath.Join(mailbox, "m-1.md")

	follower := follow(t, []string{mailbox, filepath.Join(root, "msg"), root})
	defer follower.Close()

	if err := os.MkdirAll(mailbox, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, message)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, live := <-follower.Changed():
			if !live {
				t.Fatal("the watch stopped")
			}
			if _, err := os.Stat(message); err == nil {
				return // the look this report triggered sees the message
			}
		case <-deadline:
			t.Fatal("a message written while the watch moved was never visible to a look")
		}
	}
}

func follow(t *testing.T, candidates []string) *watch.Follower {
	t.Helper()
	follower, err := watch.Follow(candidates)
	if err != nil {
		t.Fatal(err)
	}
	return follower
}

// Stopping closes the channel, so a reader waiting on it is not left waiting. A change
// that was already on its way may still arrive, which is why this drains rather than
// expecting the very next value to be the close.
func TestClosingStopsTheWatch(t *testing.T) {
	follower := follow(t, []string{t.TempDir()})
	follower.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, live := <-follower.Changed():
			if !live {
				follower.Close() // and closing twice is not a panic
				return
			}
		case <-deadline:
			t.Fatal("closing the watch did not close its channel")
		}
	}
}
