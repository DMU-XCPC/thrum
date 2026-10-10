package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	root "github.com/multiagentworkspace/thrum"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
)

// JoinOptions are what joining a workspace that already exists needs.
type JoinOptions struct {
	// Root is the directory to join into. It may not already hold a workspace.
	Root string
	// Remote is the coordination repository: the shared line this site is joining.
	Remote string
	// Site is this machine's name, which the roster has to declare.
	Site string
	// Code and Store are this workspace's other two repositories, if this site is to
	// have them. Empty means they are initialised empty, and the command says so.
	Code  string
	Store string
}

// Join makes this machine another site of a workspace that already exists.
//
// Creating and joining are different acts, and only one of them begins a history. A new
// site takes the shared line rather than making its own: a second first commit would be
// a second workspace, with its own idea of which names are taken and which jobs are
// done, and the two would never agree because neither is wrong.
//
// So joining commits nothing. It fetches, checks out, and fills in what a machine needs
// that a repository cannot carry: the ignore rules, the empty directories, this site's
// name, and its own copies of the code and asset repositories.
func Join(opts JoinOptions) (Layout, error) {
	l := New(opts.Root)
	if opts.Site == "" {
		return l, fmt.Errorf("a workspace needs a site name: a site that cannot name itself cannot find its own half of a declaration")
	}
	if opts.Remote == "" {
		return l, fmt.Errorf("joining needs the coordination repository to join: name it with --remote")
	}
	if _, err := os.Stat(filepath.Join(l.Root, ".git")); err == nil {
		return l, fmt.Errorf("%s already holds a repository: joining is for a machine that has none", l.Root)
	}
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return l, err
	}

	if err := gitx.OK(l.Root, "init", "-b", "main", "--quiet"); err != nil {
		return l, fmt.Errorf("initialising the coordination repository: %w", err)
	}
	if err := gitx.OK(l.Root, "remote", "add", "origin", opts.Remote); err != nil {
		return l, fmt.Errorf("pointing at %s: %w", opts.Remote, err)
	}
	if err := gitx.OK(l.Root, "fetch", "--quiet", "origin"); err != nil {
		return l, fmt.Errorf("fetching the records from %s: %w", opts.Remote, err)
	}

	// The shared line is the authority on what the records are, so it is taken before
	// anything is written into the working tree: a template file that the line already
	// tracks must not be overwritten by the copy a new machine happens to carry.
	branch, err := remoteHead(l)
	if err != nil {
		return l, err
	}
	if err := gitx.OK(l.Root, "checkout", "--quiet", "-B", branch, "origin/"+branch); err != nil {
		return l, fmt.Errorf("taking %s from %s: %w", branch, opts.Remote, err)
	}

	if err := fillGaps(root.Template, root.TemplateDir, l.Root); err != nil {
		return l, err
	}
	for _, dir := range l.EmptyDirs() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return l, err
		}
	}
	if err := writeExclude(l); err != nil {
		return l, err
	}
	if err := os.WriteFile(l.SiteFile(), []byte(opts.Site+"\n"), 0o644); err != nil {
		return l, err
	}

	// The roster is what says which sites exist, so a site that is not in it is a site
	// the others would refuse to talk to. Only a workspace that has never declared one
	// may be joined without a declaration, because then there is nothing to be in.
	if err := declaredOrNew(l, opts.Site); err != nil {
		return l, err
	}

	if err := repositories(l, opts); err != nil {
		return l, err
	}
	return l, nil
}

// remoteHead is the branch the shared line is on, which is what this site works on too.
func remoteHead(l Layout) (string, error) {
	if out, err := gitx.Run(l.Root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := trimSpace(out); name != "" {
			return name, nil
		}
	}
	for _, candidate := range []string{"main", "master"} {
		if gitx.Resolves(l.Root, "origin/"+candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("the coordination repository has no branch to join: an empty shared line is not a workspace yet")
}

// declaredOrNew refuses a site the roster does not declare, because the roster is what
// the other sites read to decide who exists.
func declaredOrNew(l Layout, site string) error {
	entries, err := ReadResources(l, "site")
	if err != nil || len(entries) == 0 {
		return nil // nothing declared yet: this is the first site to be named here
	}
	if _, ok := entries[site]; !ok {
		return fmt.Errorf("the roster does not declare %q: declare it at a site that is already in the workspace, and sync, before joining with it", site)
	}
	return nil
}

// repositories gives this site its own copies of the code and asset repositories.
//
// They are bare on every site, so nothing duplicates a checkout, and each site holds its
// own: the code repository is reached the way the project reaches it, and the asset
// repository carries the versions a site has fetched.
func repositories(l Layout, opts JoinOptions) error {
	if err := os.MkdirAll(filepath.Dir(l.CodeRepo()), 0o755); err != nil {
		return err
	}
	for _, repo := range []struct{ path, remote string }{
		{l.CodeRepo(), opts.Code},
		{l.Store(), opts.Store},
	} {
		if repo.remote == "" {
			args := []string{"init", "--bare", "--quiet", repo.path}
			if repo.path == l.CodeRepo() {
				args = []string{"init", "--bare", "-b", "main", "--quiet", repo.path}
			}
			if _, err := gitx.Run(l.Root, args...); err != nil {
				return fmt.Errorf("initialising %s: %w", repo.path, err)
			}
			continue
		}
		if _, err := gitx.Run(l.Root, "clone", "--bare", "--quiet", repo.remote, repo.path); err != nil {
			return fmt.Errorf("cloning %s into %s: %w", repo.remote, repo.path, err)
		}
	}
	return nil
}

func writeExclude(l Layout) error {
	ignore, err := os.ReadFile(l.GitIgnore())
	if err != nil {
		return fmt.Errorf("the workspace has no .gitignore: %w", err)
	}
	return os.WriteFile(filepath.Join(l.Root, ".git", "info", "exclude"), ignore, 0o644)
}

// fillGaps copies the template's files into an existing tree, and leaves alone anything
// the shared line already tracks.
func fillGaps(fsys fs.FS, src, dst string) error {
	return fs.WalkDir(fsys, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, err := os.Stat(target); err == nil {
			return nil // the line already has it, and the line wins
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
