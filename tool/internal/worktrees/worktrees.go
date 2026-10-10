// Package worktrees manages the labelled working directories: the one resource the
// design specifies, and the place a job is actually worked.
package worktrees

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/multiagentworkspace/ws/tool/internal/format"
	"github.com/multiagentworkspace/ws/tool/internal/gitx"
	"github.com/multiagentworkspace/ws/tool/internal/workspace"
)

// Entry is one working directory this site has materialised.
type Entry struct {
	Path     string
	Label    string // empty when the directory is not under worktree/
	Branch   string // empty when the directory is detached
	Detached bool
}

// Job returns the job a directory is carrying, if any.
func (e Entry) Job() string {
	if strings.HasPrefix(e.Branch, "job/") {
		return strings.TrimPrefix(e.Branch, "job/")
	}
	return ""
}

// List reports what this site has materialised, by asking git rather than a file: the
// design keeps the current occupant out of the registry for exactly this reason.
//
// The question goes to the code repository, not to the coordination repository at the
// workspace root: the working directories are the code repository's.
func List(l workspace.Layout) ([]Entry, error) {
	out, err := gitx.Run(l.CodeRepo(), "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, block := range strings.Split(out, "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		e := Entry{}
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				e.Path = strings.TrimPrefix(line, "worktree ")
				if rel, err := filepath.Rel(l.Worktrees(), e.Path); err == nil &&
					!strings.HasPrefix(rel, "..") && rel != "." {
					e.Label = rel
				}
			case strings.HasPrefix(line, "branch "):
				e.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
			case line == "detached":
				e.Detached = true
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Find returns the entry for one label.
func Find(l workspace.Layout, label string) (Entry, bool, error) {
	entries, err := List(l)
	if err != nil {
		return Entry{}, false, err
	}
	want := filepath.Join(l.Worktrees(), label)
	for _, e := range entries {
		if e.Path == want {
			return e, true, nil
		}
	}
	return Entry{}, false, nil
}

// Holder reports the job holding a label, and whether the label's directory is
// materialised at all.
func Holder(l workspace.Layout, label string) (job string, materialised bool, err error) {
	e, ok, err := Find(l, label)
	if err != nil || !ok {
		return "", false, err
	}
	return e.Job(), true, nil
}

// DefaultBranch is the branch the code repository points at, which is what an idle
// working directory is left detached on.
func DefaultBranch(l workspace.Layout) (string, error) {
	out, err := gitx.Run(l.CodeRepo(), "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("the code repository has no default branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Add materialises a label, either on a job's branch or detached for comparison.
//
// It refuses a label that already holds a job branch: an occupied label is never
// taken from under the job holding it.
func Add(l workspace.Layout, label, job, rev string) error {
	e, exists, err := Find(l, label)
	if err != nil {
		return err
	}
	if exists {
		if held := e.Job(); held != "" && held != job {
			return fmt.Errorf("worktree %s already holds job %s", label, held)
		}
		if e.Detached || e.Job() != job {
			// An existing directory is re-acquired in place rather than re-added,
			// because git refuses to add a worktree where files already are.
			target := e.Path
			if job == "" {
				return checkoutWorktree(l, target, rev)
			}
			// The branch is made here when it does not exist yet, which is the
			// ordinary case: a label released by one job is the label the next one
			// takes, and the next job's branch has never existed before.
			branch := "job/" + job
			if !gitx.Resolves(l.CodeRepo(), branch) {
				base := rev
				if base == "" {
					base = "HEAD"
				}
				if _, err := gitx.Run(l.CodeRepo(), "branch", branch, base); err != nil {
					return fmt.Errorf("creating %s: %w", branch, err)
				}
			}
			return checkoutWorktree(l, target, branch)
		}
		return nil
	}

	dir := l.Worktree(label)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s exists but is not a working directory git knows about", dir)
	}
	if err := os.MkdirAll(l.Worktrees(), 0o755); err != nil {
		return err
	}

	args := []string{"worktree", "add", "--quiet", dir}
	switch {
	case job != "":
		branch := "job/" + job
		if gitx.Resolves(l.CodeRepo(), branch) {
			args = append(args, branch)
		} else {
			args = append(args, "-b", branch)
			if rev != "" {
				args = append(args, rev)
			}
		}
	case rev != "":
		args = append(args, "--detach", rev)
	default:
		return fmt.Errorf("a working directory needs either a job or a revision")
	}
	_, err = gitx.Run(l.CodeRepo(), args...)
	return err
}

func checkoutWorktree(l workspace.Layout, dir, rev string) error {
	if _, err := gitx.Run(dir, "checkout", "--quiet", rev); err != nil {
		return err
	}
	return nil
}

// Release leaves a working directory clean and detached — what an idle directory
// looks like — and reports every path it discarded, because nothing here is removed
// silently.
func Release(l workspace.Layout, label string) ([]string, error) {
	e, ok, err := Find(l, label)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	branch, err := DefaultBranch(l)
	if err != nil {
		return nil, err
	}
	if _, err := gitx.Run(e.Path, "checkout", "--quiet", "--detach", branch); err != nil {
		return nil, err
	}
	discarded, err := GuardedClean(l, e.Path)
	if err != nil {
		return discarded, err
	}
	// A one-off directory exists for a single use, so releasing it destroys it. That is
	// what the declaration says, and it is read here rather than assumed.
	if ephemeral(l, label) {
		return discarded, Remove(l, label)
	}
	return discarded, nil
}

// ephemeral reports whether a declaration calls a working directory a single use.
func ephemeral(l workspace.Layout, label string) bool {
	declared, err := workspace.ReadResources(l, format.WorktreeKind)
	if err != nil {
		return false
	}
	entry, ok := declared[label]
	return ok && entry.Ephemeral != nil && *entry.Ephemeral
}

// GuardedClean removes what a working directory should not be carrying, and only ever
// inside one: a bare git clean at the workspace root would delete the code
// repository and the entire source history with it.
func GuardedClean(l workspace.Layout, dir string) ([]string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(l.Worktrees(), abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return nil, fmt.Errorf("refusing to clean %s: not a working directory under worktree/", dir)
	}
	if !gitx.IsRepo(abs) {
		return nil, fmt.Errorf("refusing to clean %s: not a git working tree", dir)
	}

	discarded, err := dirtyPaths(abs)
	if err != nil {
		return nil, err
	}
	if _, err := gitx.Run(abs, "reset", "--hard", "--quiet"); err != nil {
		return nil, err
	}
	if _, err := gitx.Run(abs, "clean", "-xffdq"); err != nil {
		return nil, err
	}
	return discarded, nil
}

// dirtyPaths lists what a working directory is carrying that its commit does not.
func dirtyPaths(dir string) ([]string, error) {
	out, err := gitx.Run(dir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		paths = append(paths, strings.TrimSpace(line[3:]))
	}
	return paths, nil
}

// Remove destroys a one-off working directory. The design lets an ephemeral label be
// destroyed on release without further checks.
func Remove(l workspace.Layout, label string) error {
	dir := l.Worktree(label)
	_, err := gitx.Run(l.CodeRepo(), "worktree", "remove", "--force", dir)
	if err != nil && !strings.Contains(err.Error(), "is not a working tree") {
		return err
	}
	return os.RemoveAll(dir)
}

// Prune forgets the working directories git can no longer find.
func Prune(l workspace.Layout) error {
	_, err := gitx.Run(l.CodeRepo(), "worktree", "prune")
	return err
}
