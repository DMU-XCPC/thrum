// Package repos owns the code repositories' declarations and their materialisation at
// repos/<name>.git.
package repos

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// Add declares a code repository and materialises it.
func Add(l layout.Layout, name, about, origin string) error {
	if !format.ValidSlug(name) {
		return fmt.Errorf("%q is not a repository name: a repository name is a slug", name)
	}
	if _, ok, err := Find(l, name); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("repository %q is already declared", name)
	}
	if err := os.MkdirAll(l.RepoDecls(), 0o755); err != nil {
		return err
	}
	if err := records.Write(l.RepoDecl(name), records.Repo{Repo: name, About: about, Origin: origin}); err != nil {
		return err
	}
	if err := line.Commit(l.Root, []string{l.RepoDecl(name)},
		fmt.Sprintf("repo %s: declared", name)); err != nil {
		return err
	}
	return Materialise(l, name, origin)
}

// Materialise puts a bare repository at this site, from the origin when there is one.
func Materialise(l layout.Layout, name, origin string) error {
	dest := l.CodeRepo(name)
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(l.Repos(), 0o755); err != nil {
		return err
	}
	if origin == "" {
		return gitx.OK("", "init", "--bare", dest)
	}
	return gitx.OK("", "clone", "--bare", origin, dest)
}

// Find returns a repository's declaration, live or archived.
func Find(l layout.Layout, name string) (records.Repo, bool, error) {
	var r records.Repo
	ok, err := records.Read(l.RepoDecl(name), &r)
	if err != nil || ok {
		return r, ok, err
	}
	ok, err = records.Read(l.ArchivedRepo(name), &r)
	return r, ok, err
}

// List returns every declared repository, in name order.
func List(l layout.Layout) ([]records.Repo, error) {
	entries, err := os.ReadDir(l.RepoDecls())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []records.Repo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		var r records.Repo
		ok, err := records.Read(filepath.Join(l.RepoDecls(), e.Name()), &r)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out, nil
}

// Remove retires a declaration. The name stays taken, and the bare repository is left
// alone: deleting somebody's history is not a side effect of a declaration.
func Remove(l layout.Layout, name string, now time.Time) error {
	r, ok, err := Find(l, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no repository named %q", name)
	}
	if r.Retired != "" {
		return fmt.Errorf("repository %q is already retired", name)
	}
	if job, err := usedByBrief(l, name); err != nil {
		return err
	} else if job != "" {
		return fmt.Errorf("repository %q is used by job %q", name, job)
	}
	if label, err := usedByLabel(l, name); err != nil {
		return err
	} else if label != "" {
		return fmt.Errorf("repository %q is used by label %q", name, label)
	}

	r.Retired = now.UTC().Format(format.DayForm)
	archived := l.ArchivedRepo(name)
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		return err
	}
	if err := records.Write(archived, r); err != nil {
		return err
	}
	if err := os.Remove(l.RepoDecl(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return line.Commit(l.Root, []string{l.RepoDecl(name), archived},
		fmt.Sprintf("retire repo/%s: retired", name))
}

// usedByBrief returns a live job that names the repository, if there is one.
func usedByBrief(l layout.Layout, name string) (string, error) {
	entries, err := os.ReadDir(l.Tasks())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		front, _, err := format.LoadDocument(l.Brief(e.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		var b records.Brief
		if err := format.Decode(front, &b); err != nil {
			return "", err
		}
		if b.Repo == name {
			return e.Name(), nil
		}
	}
	return "", nil
}

// usedByLabel returns a label bound to the repository, if there is one.
func usedByLabel(l layout.Layout, name string) (string, error) {
	entries, err := os.ReadDir(l.WorktreeDecls())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		var w records.Worktree
		ok, err := records.Read(filepath.Join(l.WorktreeDecls(), e.Name()), &w)
		if err != nil {
			return "", err
		}
		if ok && w.Repo == name && w.Retired == "" {
			return w.Label, nil
		}
	}
	return "", nil
}
