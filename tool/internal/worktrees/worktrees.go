// Package worktrees owns the labelled working directories: which one is
// materialised, which branch it carries, and releasing it cleanly.
package worktrees

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/repos"
)

// Entry is one label, declared or materialised or both.
type Entry struct {
	Label     string `json:"label"`
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Branch    string `json:"branch,omitempty"`
	Job       string `json:"job,omitempty"`
	Detached  bool   `json:"detached"`
	Declared  bool   `json:"declared"`
	Ephemeral bool   `json:"ephemeral,omitempty"`
}

// Find returns a label's declaration, live or archived.
func Find(l layout.Layout, label string) (records.Worktree, bool, error) {
	var w records.Worktree
	ok, err := records.Read(l.WorktreeDecl(label), &w)
	if err != nil || ok {
		return w, ok, err
	}
	ok, err = records.Read(l.ArchivedWorktree(label), &w)
	return w, ok, err
}

// Declare writes a label's declaration if it is new, as one event.
func Declare(l layout.Layout, label, repo, purpose string, ephemeral bool) (created bool, err error) {
	if !format.ValidSlug(label) {
		return false, fmt.Errorf("%q is not a label: a label is a slug", label)
	}
	w, ok, err := Find(l, label)
	if err != nil {
		return false, err
	}
	if ok {
		if w.Retired != "" {
			return false, fmt.Errorf("label %q is retired", label)
		}
		if w.Repo != repo {
			return false, fmt.Errorf("label %q belongs to repository %q, not %q", label, w.Repo, repo)
		}
		return false, nil
	}
	if purpose == "" {
		purpose = "an area of work"
	}
	w = records.Worktree{Label: label, Repo: repo, Purpose: purpose, Ephemeral: ephemeral}
	if err := os.MkdirAll(l.WorktreeDecls(), 0o755); err != nil {
		return false, err
	}
	if err := records.Write(l.WorktreeDecl(label), w); err != nil {
		return false, err
	}
	if err := line.Commit(l.Root, []string{l.WorktreeDecl(label)},
		fmt.Sprintf("worktree %s: declared for %s", label, repo)); err != nil {
		return false, err
	}
	return true, nil
}

// Add materialises a declared label, carrying a job's branch or a detached head.
func Add(l layout.Layout, label, repo, job string) (string, error) {
	w, ok, err := Find(l, label)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("label %q is not declared", label)
	}
	if w.Repo != repo {
		return "", fmt.Errorf("label %q belongs to repository %q, not %q", label, w.Repo, repo)
	}
	path := l.Worktree(label)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("label %q is already materialised at %s", label, path)
	}
	gitdir := l.CodeRepo(repo)
	if _, err := os.Stat(gitdir); err != nil {
		return "", fmt.Errorf("repository %q is not materialised here", repo)
	}

	if job == "" {
		if err := gitx.OK("", "--git-dir", gitdir, "worktree", "add", "--detach", path, "HEAD"); err != nil {
			return "", err
		}
		return path, nil
	}

	branch := "job/" + job
	exists, err := refExists(gitdir, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	switch {
	case exists:
		err = gitx.OK("", "--git-dir", gitdir, "worktree", "add", path, branch)
	case headExists(gitdir):
		head, runErr := gitx.Run("", "--git-dir", gitdir, "rev-parse", "HEAD")
		if runErr != nil {
			return "", runErr
		}
		err = gitx.OK("", "--git-dir", gitdir, "worktree", "add", "-b", branch, path, strings.TrimSpace(head))
	default:
		err = gitx.OK("", "--git-dir", gitdir, "worktree", "add", "--orphan", "-b", branch, path)
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

// Where finds the directory carrying a job's branch.
func Where(l layout.Layout, repo, job string) (string, bool, error) {
	gitdir := l.CodeRepo(repo)
	out, err := gitx.Run("", "--git-dir", gitdir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false, err
	}
	for _, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var path, branch string
		for _, ln := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(ln, "worktree "):
				path = strings.TrimPrefix(ln, "worktree ")
			case strings.HasPrefix(ln, "branch "):
				branch = strings.TrimPrefix(ln, "branch ")
			}
		}
		if branch == "refs/heads/job/"+job {
			return path, true, nil
		}
	}
	return "", false, nil
}

// List reports every declared label and every materialised working directory.
func List(l layout.Layout) ([]Entry, error) {
	byLabel := map[string]Entry{}

	entries, err := os.ReadDir(l.WorktreeDecls())
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
				continue
			}
			var w records.Worktree
			ok, err := records.Read(filepath.Join(l.WorktreeDecls(), e.Name()), &w)
			if err != nil {
				return nil, err
			}
			if ok && w.Retired == "" {
				byLabel[w.Label] = Entry{
					Label: w.Label, Repo: w.Repo, Path: l.Worktree(w.Label),
					Declared: true, Ephemeral: w.Ephemeral,
				}
			}
		}
	}

	decls, err := repos.List(l)
	if err != nil {
		return nil, err
	}
	for _, r := range decls {
		gitdir := l.CodeRepo(r.Repo)
		if _, err := os.Stat(gitdir); err != nil {
			continue
		}
		out, err := gitx.Run("", "--git-dir", gitdir, "worktree", "list", "--porcelain")
		if err != nil {
			continue
		}
		for _, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
			var path, branch string
			detached := false
			for _, ln := range strings.Split(block, "\n") {
				switch {
				case strings.HasPrefix(ln, "worktree "):
					path = strings.TrimPrefix(ln, "worktree ")
				case strings.HasPrefix(ln, "branch "):
					branch = strings.TrimPrefix(ln, "branch ")
				case ln == "detached":
					detached = true
				}
			}
			if path == "" || filepath.Dir(path) != l.Worktrees() {
				continue
			}
			label := filepath.Base(path)
			en := byLabel[label]
			en.Label, en.Repo, en.Path, en.Detached = label, r.Repo, path, detached
			if strings.HasPrefix(branch, "refs/heads/") {
				en.Branch = strings.TrimPrefix(branch, "refs/heads/")
				if strings.HasPrefix(en.Branch, "job/") {
					en.Job = strings.TrimPrefix(en.Branch, "job/")
				}
			}
			byLabel[label] = en
		}
	}

	out := make([]Entry, 0, len(byLabel))
	for _, e := range byLabel {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// Release leaves a label clean and detached, and destroys it when it is one-off.
func Release(l layout.Layout, label string) error {
	w, ok, err := Find(l, label)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("label %q is not declared", label)
	}
	if w.Retired != "" {
		return fmt.Errorf("label %q is retired", label)
	}
	path := l.Worktree(label)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	out, err := gitx.Run(path, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("%s has changes; commit or clean it before releasing", path)
	}
	gitdir := l.CodeRepo(w.Repo)
	if w.Ephemeral {
		return gitx.OK("", "--git-dir", gitdir, "worktree", "remove", path)
	}
	return gitx.OK(path, "checkout", "--detach")
}

// Discard removes a working directory and its branch, for an opening that failed.
func Discard(l layout.Layout, label, repo, job string) {
	path := l.Worktree(label)
	gitdir := l.CodeRepo(repo)
	_ = gitx.OK("", "--git-dir", gitdir, "worktree", "remove", "--force", path)
	if job != "" {
		_ = gitx.OK("", "--git-dir", gitdir, "branch", "-D", "job/"+job)
	}
}

func refExists(gitdir, ref string) (bool, error) {
	_, err := gitx.Run("", "--git-dir", gitdir, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		if gitx.IsErr(err, "Needed a single revision", "unknown revision", "invalid object") {
			return false, nil
		}
		return false, nil
	}
	return true, nil
}

func headExists(gitdir string) bool {
	_, err := gitx.Run("", "--git-dir", gitdir, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}
