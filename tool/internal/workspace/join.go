package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	root "github.com/multiagentworkspace/thrum"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
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
	// A site that cannot name itself cannot find its own half of a declaration, and a name that is
	// not a slug is one no record can carry. Refused before anything is created: a workspace must
	// not be born with a finding of its own making, and a refusal that leaves a half-made
	// directory behind is work nobody asked for.
	if opts.Site != "" && !format.ValidSlug(opts.Site) {
		return Layout{}, fmt.Errorf("the site name %q is not a slug: lowercase letters, digits and hyphens, like every other name here", opts.Site)
	}
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
	existed := true
	if _, err := os.Stat(l.Root); os.IsNotExist(err) {
		existed = false
	}
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return l, err
	}
	// A site that could not join is not half a site. What was made for it goes away, so
	// that the same directory can be joined once the reason has been dealt with — and so
	// that nothing is left that looks like a workspace and is not one.
	done := false
	defer func() {
		if done {
			return
		}
		if existed {
			_ = os.RemoveAll(filepath.Join(l.Root, ".git"))
			return
		}
		_ = os.RemoveAll(l.Root)
	}()

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

	// The roster is what says which sites exist, so a site that is not in it is a site the
	// others would refuse to talk to. It comes before the repositories, and the rollback
	// above takes care of what was already written when it refuses.
	if err := declared(l, opts.Site); err != nil {
		return l, err
	}

	if err := repositories(l, opts.Code, opts.Store); err != nil {
		return l, err
	}
	done = true
	return l, nil
}

// remoteHead is the branch the shared line is on, which is what this site works on too.
func remoteHead(l Layout) (string, error) {
	if out, err := gitx.Run(l.Root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		// It answers `origin/main`: the remote's name is on the front, and the branch is
		// what is wanted, because the caller already knows which remote it asked.
		if name := strings.TrimPrefix(trimSpace(out), "origin/"); name != "" {
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

// declared refuses a site the roster does not declare, because the roster is what the
// other sites read to decide who exists.
//
// An empty roster declares nothing, so it declares nobody: the first site is declared by
// whoever is already in the workspace, and that is a step rather than an oversight.
func declared(l Layout, site string) error {
	entries, err := ReadResources(l, "site")
	if err != nil {
		return err
	}
	if _, ok := entries[site]; !ok {
		return fmt.Errorf("the roster does not declare %q: declare it at a site that is already in the workspace, and sync, before joining with it", site)
	}
	return nil
}

// writeExclude fills in the ignore rules a repository cannot carry, in both copies. The
// list is composed rather than read back from `.gitignore`, because the rules that keep
// the tooling's own materialised files out of `git status` are part of it — and because
// the two copies must agree.
func writeExclude(l Layout) error {
	known := map[string]bool{}
	for _, rule := range skeletonIgnoreRules() {
		known[rule] = true
	}
	if tooling, err := toolingIgnore(); err == nil {
		for _, rule := range ignoreRules(tooling) {
			known[rule] = true
		}
	}
	// Anything this workspace already carries that the tooling does not is kept: joining
	// is not the moment to narrow what a site ignores.
	var extra []string
	if current, err := os.ReadFile(l.GitIgnore()); err == nil {
		for _, rule := range ignoreRules(current) {
			if !known[rule] {
				known[rule] = true
				extra = append(extra, rule)
			}
		}
	}
	content, err := ignoreContent(extra)
	if err != nil {
		return fmt.Errorf("the workspace has no .gitignore: %w", err)
	}
	return writeIgnore(l, content)
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
