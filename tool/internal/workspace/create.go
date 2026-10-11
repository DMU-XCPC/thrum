package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"io"

	root "github.com/multiagentworkspace/thrum"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
)

// CreateOptions are what creating a workspace needs to know.
type CreateOptions struct {
	// Root is the directory to create. It may not already hold a workspace.
	Root string
	// Site is the name of the site this machine is, written to var/site.
	Site string
	// Actor is who is creating it, used for the first commit's identity.
	Actor string
	// Code is where the project keeps its code repository. Empty means this workspace
	// starts an empty one, which is what a project that has pushed nothing anywhere yet
	// has — and a job branches from history, so nothing can be opened until there is some.
	Code string
	// Store is where the project keeps its asset repository, if it keeps one already.
	Store string
}

// Create instantiates a workspace from the template.
//
// This is not a file copy, and the design says so: the two repositories have to be
// initialised rather than duplicated, and the empty directories have to be created
// rather than copied, because a repository cannot carry an empty directory. What the
// template does hold is the files that are copied verbatim.
func Create(opts CreateOptions) (Layout, error) {
	l := New(opts.Root)
	if opts.Site == "" {
		return l, fmt.Errorf("a workspace needs a site name: a site that cannot name itself cannot find its own half of a declaration")
	}

	if _, err := os.Stat(filepath.Join(l.Root, ".git")); err == nil {
		return l, fmt.Errorf("%s already holds a workspace", l.Root)
	}
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return l, err
	}

	// The files the template carries, copied verbatim. The all: prefix on the embed
	// directive is what brings the ignore rules along.
	if err := copyTemplate(root.Template, root.TemplateDir, l.Root); err != nil {
		return l, err
	}

	for _, dir := range l.EmptyDirs() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return l, err
		}
	}

	// The coordination repository. Only doc/ and handoff/ are ever tracked.
	if err := gitx.OK(l.Root, "init", "-b", "main", "--quiet"); err != nil {
		return l, fmt.Errorf("initialising the coordination repository: %w", err)
	}
	// The list the tooling carries, written into both copies — the one the coordination
	// repository would track, and the one git keeps out of it — so that one bad edit to a
	// tracked file cannot expose the source history. It is composed rather than copied
	// because the skeleton's own files are part of it, and because it is the same list
	// that `update` restores.
	content, err := ignoreContent(nil)
	if err != nil {
		return l, fmt.Errorf("the template carries no .gitignore: %w", err)
	}
	if err := writeIgnore(l, content); err != nil {
		return l, err
	}

	// This site's own name, which is a one-line site-local fact.
	if err := os.WriteFile(l.SiteFile(), []byte(opts.Site+"\n"), 0o644); err != nil {
		return l, err
	}

	// The code and asset repositories, both bare so that no checkout duplicates them, and
	// taken from where the project keeps them when it says where that is.
	if err := repositories(l, opts.Code, opts.Store); err != nil {
		return l, err
	}

	if err := firstCommit(l, opts.Actor); err != nil {
		return l, err
	}
	return l, nil
}

// firstCommit records the workspace as it begins: the template's files under doc/,
// and nothing else. Its kind is init, which no other event uses.
func firstCommit(l Layout, actor string) error {
	if _, err := gitx.Run(l.Root, "add", "--", "doc", "handoff"); err != nil {
		return fmt.Errorf("staging the template: %w", err)
	}

	args := gitx.IdentityArgs(l.Root, actor)
	args = append(args, "commit", "--quiet", "-m", "init: created from the template")
	if _, err := gitx.Run(l.Root, args...); err != nil {
		return fmt.Errorf("making the first commit: %w", err)
	}
	return nil
}

// copyTemplate writes every file the template carries into dst, at the same relative
// paths.
func copyTemplate(fsys fs.FS, src, dst string) error {
	return fs.WalkDir(fsys, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

// ReadSite returns the name this machine's workspace goes by.
func ReadSite(l Layout) (string, error) {
	b, err := os.ReadFile(l.SiteFile())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
