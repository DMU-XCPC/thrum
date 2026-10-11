package commit

import (
	"fmt"
	"os"
	"sort"
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
	// The lock is taken here because this is where a record write begins: what follows
	// reads the workspace and decides what to record, and all of it has to happen with
	// no other writer in between.
	if err := acquire(l); err != nil {
		return err
	}
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
	args := append(gitx.IdentityArgs(l.Root, ""), "rebase", "--quiet", up)
	if _, err := gitx.Run(l.Root, args...); err != nil {
		_, _ = gitx.Run(l.Root, append(gitx.IdentityArgs(l.Root, ""), "rebase", "--abort")...)
		return clash(l, ahead, up)
	}
	return nil
}

// Begin makes a record write's way clear: the line is brought current, and the index is
// checked for changes that are not this event's.
func Begin(l workspace.Layout, paths ...string) error {
	if err := Current(l); err != nil {
		return err
	}
	return Guard(l, paths...)
}

// Guard refuses when the index holds changes this event did not make.
//
// It is meant to run *before* the event writes anything. An event that discovers the index
// is in its way only when it goes to record itself has already written its own half — a
// file on disk and in the index that never reaches the line, which validation does not see
// and sync does not carry — and the refusal then blocks the very remedy it recommends.
func Guard(l workspace.Layout, paths ...string) error {
	foreign, err := foreignStaged(l, paths)
	if err != nil {
		return err
	}
	if len(foreign) > 0 {
		return fmt.Errorf("the index holds changes this event did not make — %s — and an event records exactly what it staged, so it would put somebody else's change under this event's name: record them as their own event, or unstage them, and then ask again. This event has written nothing",
			strings.Join(foreign, ", "))
	}
	return nil
}

// Hold takes the coordination lock for a change to the records that is not an event.
func Hold(l workspace.Layout) error {
	return acquire(l)
}

// clash explains records that cannot go into one line, and says the way out.
//
// The way out is not to force anything. A record that is unpublished was never in the line,
// so it can be taken out of the working copy without rewriting anything anyone has seen,
// and the files it wrote stay where they are: a brief or a handoff is somebody's work, and
// the one command that would make this moment painless is the one that would destroy it.
func clash(l workspace.Layout, ahead int, up string) error {
	where := ""
	if shared := clashPaths(l, up); len(shared) > 0 {
		where = fmt.Sprintf(": %s changed on both sides", strings.Join(shared, ", "))
	}
	return fmt.Errorf("this site's %d unpublished record(s) do not go together with what was published elsewhere%s, and nothing was changed here or merged, because dropping a record is worse than stopping. To get out of it: read what the other site published with `git show %s:<path>`, then `thrum sync --withdraw` takes this site's records back out of the line and leaves what they wrote in the working tree, and from there the file can be put right and the event recorded again. Do not reach for `git reset --hard`: the thing it destroys is exactly the file in question",
		ahead, where, up)
}

// clashPaths names the paths this site and the line both changed.
func clashPaths(l workspace.Layout, up string) []string {
	out, err := gitx.Run(l.Root, "merge-base", "HEAD", up)
	if err != nil {
		return nil
	}
	base := strings.TrimSpace(out)
	mine, err := gitx.Run(l.Root, "diff", "--name-only", base, "HEAD")
	if err != nil {
		return nil
	}
	theirs, err := gitx.Run(l.Root, "diff", "--name-only", base, up)
	if err != nil {
		return nil
	}
	both := map[string]bool{}
	for _, p := range strings.Fields(theirs) {
		both[p] = true
	}
	var shared []string
	for _, p := range strings.Fields(mine) {
		if both[p] {
			shared = append(shared, p)
		}
	}
	sort.Strings(shared)
	return shared
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
		if err := prepare(l, paths); err != nil {
			return id, err
		}
		if id, err = commitNow(l, message); err != nil {
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

// prepare stages one event's paths, and refuses when the index holds anything else.
//
// Naming the paths on the commit as well as on the add is not enough to keep an event to
// its own changes: git takes the working tree's content for a named path, so a hand edit
// that arrived after the event staged its paths would be recorded as part of it. Committing
// the index instead records the bytes that were staged — which only holds if the index is
// exactly this event's paths, and that is what this checks.
func prepare(l workspace.Layout, paths []string) error {
	if err := stage(l, paths); err != nil {
		return fmt.Errorf("staging: %w", err)
	}
	foreign, err := foreignStaged(l, paths)
	if err != nil {
		return err
	}
	if len(foreign) > 0 {
		// Something arrived between the guard and the write, which only a hand can do
		// while the lock is held. Take this event's own paths back out of the index, so
		// that what is left is what the guard saw, and the caller is not left holding
		// half an event.
		_, _ = gitx.Run(l.Root, append([]string{"reset", "--quiet", "--"}, paths...)...)
		return fmt.Errorf("the index holds changes this event did not make — %s — and nothing was recorded: what this event wrote is in the working tree, uncommitted, and the paths it staged have been taken back out of the index",
			strings.Join(foreign, ", "))
	}
	anything, err := anythingStaged(l, paths)
	if err != nil {
		return err
	}
	if !anything {
		return fmt.Errorf("nothing to record: %s is already what the line has", strings.Join(paths, ", "))
	}
	return nil
}

// foreignStaged names the staged changes that are not this event's.
func foreignStaged(l workspace.Layout, paths []string) ([]string, error) {
	out, err := gitx.Run(l.Root, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	var foreign []string
	for _, staged := range strings.Fields(out) {
		if !coveredBy(paths, staged) {
			foreign = append(foreign, staged)
		}
	}
	return foreign, nil
}

// anythingStaged reports whether the index holds a change among these paths.
func anythingStaged(l workspace.Layout, paths []string) (bool, error) {
	args := append([]string{"diff", "--cached", "--name-only", "--"}, paths...)
	out, err := gitx.Run(l.Root, args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// coveredBy reports whether a staged path is one of the event's paths, or inside one of
// them — a directory an event owns covers the files under it.
func coveredBy(paths []string, staged string) bool {
	for _, mine := range paths {
		mine = strings.TrimSuffix(mine, "/")
		if staged == mine || strings.HasPrefix(staged, mine+"/") {
			return true
		}
	}
	return false
}

func commitNow(l workspace.Layout, message string) (string, error) {
	args := gitx.IdentityArgs(l.Root, "thrum")
	args = append(args, "commit", "--quiet", "-m", message)
	if _, err := gitx.Run(l.Root, args...); err != nil {
		return "", err
	}
	out, err := gitx.Run(l.Root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
