package commit

import (
	"fmt"
	"os"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// The record line is linear, and that is a property of the protocol rather than a hope.
//
// The tip of the shared branch is the only piece of exclusion state a coordination write
// needs: a commit is in the line if and only if its parent was, so a commit that loses a
// race is taken back out before it is ever published, and one that wins is published as a
// fast-forward. Nothing published is ever rewritten, nothing is ever merged, and the two
// costs are explicit — a site brings itself up to date *before* it writes, and an event
// is published or withdrawn rather than kept and reconciled later.

// Current makes the record line current, before anything is written to it.
//
// It runs first in every operation that records something, so that what the operation
// reads is what the others have already published. Records this site has committed but
// not published are put on top of theirs: they were never in the line, so moving them
// rewrites nothing anyone else has seen. If that cannot be done without a conflict, this
// does nothing at all and says so.
func Current(l workspace.Layout) error {
	if !hasRemote(l) {
		// A workspace nobody else has cannot diverge from anybody.
		return nil
	}
	if _, err := gitx.Run(l.Root, "fetch", "--quiet", "origin"); err != nil {
		// Unreachable is not divergence: work continues here, and what is recorded
		// while the site is alone is published when it is not.
		return nil
	}
	branch, err := branchOf(l)
	if err != nil {
		return err
	}
	up := "origin/" + branch
	if !gitx.Resolves(l.Root, up) {
		return nil // the line has nothing on it yet
	}
	if countOut, err := count(l, "HEAD.."+up); err != nil || countOut == 0 {
		return err
	}

	ahead, err := count(l, up+"..HEAD")
	if err != nil {
		return err
	}
	if ahead == 0 {
		if _, err := gitx.Run(l.Root, "merge", "--ff-only", "--quiet", up); err != nil {
			return fmt.Errorf("the record line moved and this site cannot fast-forward onto it: %w", err)
		}
		return nil
	}

	if dirty := trackedDirty(l); dirty != "" {
		return fmt.Errorf("this site has %d record commits that are not published, and %s is uncommitted: records that are not yet in the line are brought up to date before files are written, and that cannot be done with a change in the way", ahead, dirty)
	}
	if _, err := gitx.Run(l.Root, "rebase", "--quiet", up); err != nil {
		_, _ = gitx.Run(l.Root, "rebase", "--abort")
		return fmt.Errorf("the %d record commits this site has not published do not go together with what was published elsewhere: nothing was changed here, and nothing was merged", ahead)
	}
	return nil
}

// publishOrUndo puts the event in the line, or takes it back out.
//
// A rejected push is a race that was lost, and the event never reached the line, so
// redoing it rewrites nothing. What is never done is the third option: keeping both and
// reconciling later, which is how a line stops being one line.
func publishOrUndo(l workspace.Layout, paths []string, message, id string) (string, error) {
	if !hasRemote(l) {
		return id, nil
	}
	branch, err := branchOf(l)
	if err != nil {
		return id, err
	}
	up := "origin/" + branch

	for attempt := 0; attempt < 4; attempt++ {
		if _, err := gitx.Run(l.Root, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch); err == nil {
			return id, nil
		}
		// A push that failed because the other site is not there is not a lost race:
		// this record stays here, and goes into the line when the site is reachable.
		if _, err := gitx.Run(l.Root, "ls-remote", "--quiet", "origin"); err != nil {
			fmt.Fprintf(os.Stderr, "thrum: %s cannot be reached, so this record is here and not yet in the line\n", l.Root)
			return id, nil
		}
		if _, err := gitx.Run(l.Root, "fetch", "--quiet", "origin"); err != nil {
			return id, err
		}
		if !gitx.Resolves(l.Root, up) {
			return id, fmt.Errorf("the record line vanished while this event was being written")
		}

		ahead, err := count(l, up+"..HEAD")
		if err != nil {
			return id, err
		}
		if ahead != 1 {
			return id, fmt.Errorf("the line moved while this event was being written, and this site has %d records of its own that are not in it: publish those first, and nothing here was merged", ahead)
		}
		moved, err := gitx.Run(l.Root, "diff", "--name-only", "HEAD^", up)
		if err != nil {
			return id, err
		}
		if clash := overlaps(moved, paths); clash != "" {
			// Refusing is not enough: the event's own commit has to come back out too,
			// or this site is left holding a commit that will never be in the line and
			// that the next event would have to reconcile. The working tree keeps what
			// the event wrote.
			if _, err := gitx.Run(l.Root, "reset", "--mixed", "--quiet", up); err != nil {
				return id, err
			}
			return id, fmt.Errorf("%s was changed by a record published at another site while this event was being written: nothing was put in the line, and what this event wrote is in the working tree, uncommitted — read what the other site wrote and record the event again", clash)
		}

		// Ours was never in the line, so redoing it on top of theirs rewrites nothing
		// that anybody has seen. The working tree keeps what the event wrote.
		if _, err := gitx.Run(l.Root, "reset", "--mixed", "--quiet", up); err != nil {
			return id, err
		}
		if err := stage(l, paths); err != nil {
			return id, err
		}
		if id, err = commitNow(l, paths, message); err != nil {
			return id, err
		}
	}
	return id, fmt.Errorf("the record line moved four times while this event was being written: nothing was put in it, and what this event wrote is in the working tree, uncommitted")
}

// overlaps names the first path of the event that somebody else moved, judging by whole
// path elements so that a directory the event owns covers the files under it.
func overlaps(moved string, paths []string) string {
	changed := strings.Fields(moved)
	for _, mine := range paths {
		for _, theirs := range changed {
			if mine == theirs || strings.HasPrefix(theirs, strings.TrimSuffix(mine, "/")+"/") ||
				strings.HasPrefix(mine, strings.TrimSuffix(theirs, "/")+"/") {
				return theirs
			}
		}
	}
	return ""
}

func hasRemote(l workspace.Layout) bool {
	_, err := gitx.Run(l.Root, "remote", "get-url", "origin")
	return err == nil
}

func branchOf(l workspace.Layout) (string, error) {
	out, err := gitx.Run(l.Root, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("the record line is not on a branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func count(l workspace.Layout, rng string) (int, error) {
	out, err := gitx.Run(l.Root, "rev-list", "--count", rng)
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

// trackedDirty names one change that is in the way of putting unpublished records on top
// of the line, or nothing when there is none. Untracked files are not in the way.
func trackedDirty(l workspace.Layout) string {
	out, err := gitx.Run(l.Root, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return ""
	}
	if line, _, ok := strings.Cut(strings.TrimSpace(out), "\n"); ok {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(out)
}

func stage(l workspace.Layout, paths []string) error {
	args := append([]string{"add", "-A", "--"}, paths...)
	_, err := gitx.Run(l.Root, args...)
	return err
}

func commitNow(l workspace.Layout, paths []string, message string) (string, error) {
	// The paths are named on the commit as well as on the add, so that a file another
	// actor on this machine has staged cannot ride along in this event.
	args := gitx.IdentityArgs(l.Root, "thrum")
	args = append(args, "commit", "--quiet", "-m", message, "--")
	args = append(args, paths...)
	if _, err := gitx.Run(l.Root, args...); err != nil {
		return "", err
	}
	out, err := gitx.Run(l.Root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
