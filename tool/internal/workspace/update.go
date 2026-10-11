package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	root "github.com/multiagentworkspace/thrum"
)

// The template is what the tooling carries, and a workspace materialises it once. After
// that the two can drift — the tooling learns to ignore something new, or grows a
// directory — and the drift is invisible, because none of it is in a repository: a
// repository cannot carry an empty directory, and the ignore rules are deliberately
// untracked so that one bad edit to a tracked file cannot expose the source history.
//
// So nothing is stored to make this checkable. The tooling carries the template, so
// what a workspace is missing is *derived* by comparing the two, rather than remembered
// in a version number that would go stale on every site separately.
//
// What may be written is decided by who owns it, and it splits the template in two. The
// unversioned skeleton — the empty directories, and the ignore rules in both places — is
// the tooling's, and bringing it up to date loses nothing. `doc/template/` is the
// project's: it is versioned, in the shared line, and a workspace may legitimately keep
// a different one, so a difference there is reported for a person to record rather than
// written.

// Drift is what a workspace is missing relative to the tooling's template, or has
// beyond it.
type Drift struct {
	// MissingDirs are directories the template carries and this workspace does not,
	// relative to the workspace root.
	MissingDirs []string
	// IgnoreMissing are ignore rules the tooling has and this workspace does not. They
	// are the safety half of the comparison: a rule that is gone is a file that could
	// be committed.
	IgnoreMissing []string
	// IgnoreExtra are rules this workspace has and the tooling does not. They are kept
	// rather than dropped, because narrowing what a workspace ignores is not something
	// to do on its own initiative.
	IgnoreExtra []string
	// Templates are the copy-me files that differ from the tooling's, or are absent.
	Templates []TemplateDrift
	// Skeleton is the tooling's own materialised files that are absent, or are not the
	// copy the tooling carries.
	Skeleton []SkeletonDrift
}

// TemplateDrift is one `doc/template/` file that is not the tooling's.
type TemplateDrift struct {
	// Path is relative to the workspace root.
	Path      string
	Tooling   []byte
	Workspace []byte // nil when the workspace has none
}

// SkeletonDrift is one file the tooling materialises outside the repositories that is not
// the copy the tooling carries.
type SkeletonDrift struct {
	// Path is relative to the workspace root.
	Path      string
	Tooling   []byte
	Workspace []byte // nil when the workspace has none
}

// Empty reports whether there is nothing to bring up to date. Template differences are
// not part of it: they are never applied on the tooling's initiative.
func (d Drift) Empty() bool {
	return len(d.MissingDirs) == 0 && len(d.IgnoreMissing) == 0 && len(d.IgnoreExtra) == 0 &&
		len(d.Templates) == 0 && len(d.Skeleton) == 0
}

// Compare reports what this workspace is missing relative to the tooling's template.
func Compare(l Layout) (Drift, error) {
	var d Drift

	for _, dir := range l.EmptyDirs() {
		if _, err := os.Stat(dir); err != nil {
			rel, relErr := filepath.Rel(l.Root, dir)
			if relErr != nil {
				return d, relErr
			}
			d.MissingDirs = append(d.MissingDirs, rel)
		}
	}
	sort.Strings(d.MissingDirs)

	tooling, err := toolingIgnore()
	if err != nil {
		return d, err
	}
	rules := append(ignoreRules(tooling), skeletonIgnoreRules()...)
	// A rule counts as missing when either copy has lost it. Both copies carry the whole
	// list on purpose — one bad edit to one of them must not expose the source history —
	// so a rule that survives in only one is half the protection it is meant to be.
	missing := map[string]bool{}
	for _, path := range ignoreCopies(l) {
		have := map[string]bool{}
		if content, err := os.ReadFile(path); err == nil {
			for _, rule := range ignoreRules(content) {
				have[rule] = true
			}
		}
		for _, rule := range rules {
			if !have[rule] {
				missing[rule] = true
			}
		}
	}
	for rule := range missing {
		d.IgnoreMissing = append(d.IgnoreMissing, rule)
	}
	mine := map[string]bool{}
	for _, rule := range rules {
		mine[rule] = true
	}
	seen := map[string]bool{}
	for _, path := range ignoreCopies(l) {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, rule := range ignoreRules(content) {
			if !mine[rule] && !seen[rule] {
				seen[rule] = true
				d.IgnoreExtra = append(d.IgnoreExtra, rule)
			}
		}
	}
	sort.Strings(d.IgnoreMissing)
	sort.Strings(d.IgnoreExtra)

	templates, err := templateFiles()
	if err != nil {
		return d, err
	}
	for _, rel := range templates {
		tooling, err := fs.ReadFile(root.Template, root.TemplateDir+"/"+rel)
		if err != nil {
			return d, err
		}
		target := filepath.Join(l.Root, filepath.FromSlash(rel))
		current, err := os.ReadFile(target)
		if err == nil && string(current) == string(tooling) {
			continue
		}
		if err != nil {
			current = nil
		}
		d.Templates = append(d.Templates, TemplateDrift{Path: rel, Tooling: tooling, Workspace: current})
	}

	// The skeleton is the tooling's, so it is compared the same way a lost ignore rule
	// is: what is derived is only whether this workspace holds the copy the tooling
	// carries. Nothing reads what a workspace has written there, which is what makes the
	// entry point a map rather than a second version of the truth.
	for _, file := range SkeletonFiles() {
		tooling, err := fs.ReadFile(root.Template, root.TemplateDir+"/"+file.Path)
		if err != nil {
			return d, err
		}
		current, err := os.ReadFile(filepath.Join(l.Root, filepath.FromSlash(file.Path)))
		if err == nil && string(current) == string(tooling) {
			continue
		}
		if err != nil {
			current = nil
		}
		d.Skeleton = append(d.Skeleton, SkeletonDrift{Path: file.Path, Tooling: tooling, Workspace: current})
	}
	return d, nil
}

// UpdateOptions are what bringing a workspace up to date is allowed to do.
type UpdateOptions struct {
	// Narrow also drops ignore rules the tooling no longer has. Without it they are
	// kept: widening what a workspace ignores is safe on its own, and narrowing it is a
	// decision.
	Narrow bool
	// AdoptTemplate writes the tooling's copy-me files over this workspace's. It is off
	// by default because those files are the project's, and a workspace may keep a
	// different one on purpose. Whatever it writes is left uncommitted, for a person to
	// record as the event it is.
	AdoptTemplate bool
	// DryRun reports what would happen and writes nothing.
	DryRun bool
}

// UpdateResult is what bringing a workspace up to date did, or would do.
type UpdateResult struct {
	DirsCreated      []string
	IgnoreWritten    bool
	IgnoreKept       []string
	Templates        []TemplateDrift
	TemplatesAdopted []string
	SkeletonWritten  []string
}

// Update brings the unversioned skeleton up to date, and reports the rest.
//
// It never touches a versioned file unless it is asked to, and it never removes a
// directory: a directory with something in it is somebody's work.
func Update(l Layout, o UpdateOptions) (UpdateResult, error) {
	drift, err := Compare(l)
	if err != nil {
		return UpdateResult{}, err
	}
	res := UpdateResult{Templates: drift.Templates}

	for _, rel := range drift.MissingDirs {
		res.DirsCreated = append(res.DirsCreated, rel)
		if o.DryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Join(l.Root, filepath.FromSlash(rel)), 0o755); err != nil {
			return res, err
		}
	}

	keep := drift.IgnoreExtra
	if o.Narrow {
		keep = nil
	}
	// The skeleton's own rules are part of the list, so a workspace that has lost one
	// gets it back here — the same path that restores a rule the workspace has lost.
	if len(drift.IgnoreMissing) > 0 || (o.Narrow && len(drift.IgnoreExtra) > 0) {
		content, err := ignoreContent(keep)
		if err != nil {
			return res, err
		}
		res.IgnoreWritten = true
		res.IgnoreKept = keep
		if !o.DryRun {
			if err := writeIgnore(l, content); err != nil {
				return res, err
			}
		}
	}

	// The skeleton belongs to the tooling, so it is restored rather than reported: a map
	// that has fallen behind is replaced whole, and nothing here is the project's to
	// keep.
	for _, file := range drift.Skeleton {
		res.SkeletonWritten = append(res.SkeletonWritten, file.Path)
		if o.DryRun {
			continue
		}
		target := filepath.Join(l.Root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(target, file.Tooling, 0o644); err != nil {
			return res, err
		}
	}

	if o.AdoptTemplate && !o.DryRun {
		for _, t := range drift.Templates {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(l.Root, filepath.FromSlash(t.Path))), 0o755); err != nil {
				return res, err
			}
			if err := os.WriteFile(filepath.Join(l.Root, filepath.FromSlash(t.Path)), t.Tooling, 0o644); err != nil {
				return res, err
			}
			res.TemplatesAdopted = append(res.TemplatesAdopted, t.Path)
		}
	}
	return res, nil
}

// toolingIgnore is the ignore rules the tooling carries.
func toolingIgnore() ([]byte, error) {
	return fs.ReadFile(root.Template, root.TemplateDir+"/.gitignore")
}

// skeletonIgnoreRules is the rules that keep the tooling's own materialised files out of
// the coordination repository's sight. They live here rather than in the template's
// `.gitignore` because that file is payload — see SkeletonFiles.
func skeletonIgnoreRules() []string {
	var rules []string
	seen := map[string]bool{}
	for _, file := range SkeletonFiles() {
		rule := file.Ignore()
		if seen[rule] {
			continue // one directory, however many files the tooling puts in it
		}
		seen[rule] = true
		rules = append(rules, rule)
	}
	return rules
}

// ignoreContent is the file to write: the tooling's rules, the rules that keep the
// tooling's own materialised files out of `git status`, and any of this workspace's own
// that are being kept rather than dropped.
func ignoreContent(keep []string) ([]byte, error) {
	tooling, err := toolingIgnore()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.Write(tooling)
	if !strings.HasSuffix(string(tooling), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n# The tooling's own materialised files. They are restored rather than committed, so\n")
	b.WriteString("# an unignored one is a file `clean` would remove.\n")
	for _, rule := range skeletonIgnoreRules() {
		b.WriteString(rule + "\n")
	}
	if len(keep) > 0 {
		b.WriteString("\n# Kept here: rules this workspace had and the tooling does not. Removing one\n")
		b.WriteString("# is a decision rather than an update, so they are reported until it is made.\n")
		for _, rule := range keep {
			b.WriteString(rule + "\n")
		}
	}
	return []byte(b.String()), nil
}

// writeIgnore writes both copies, which is the whole point of having two: one bad edit
// to either cannot expose the source history.
func writeIgnore(l Layout, content []byte) error {
	if err := os.WriteFile(l.GitIgnore(), content, 0o644); err != nil {
		return err
	}
	exclude := filepath.Join(l.Root, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return err
	}
	return os.WriteFile(exclude, content, 0o644)
}

// ignoreCopies are the two files that carry the ignore rules: the one the coordination
// repository would track, and the one git keeps out of it. They are written together and
// compared together.
func ignoreCopies(l Layout) []string {
	return []string{l.GitIgnore(), filepath.Join(l.Root, ".git", "info", "exclude")}
}

// ignoreRules is the meaning of an ignore file: its rules, without the comments and the
// blank lines. Rewriting a comment is not a change worth making.
func ignoreRules(content []byte) []string {
	var out []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// templateFiles are the copy-me files the tooling carries, workspace-relative.
func templateFiles() ([]string, error) {
	dir := root.TemplateDir + "/doc/template"
	entries, err := fs.ReadDir(root.Template, dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, "doc/template/"+e.Name())
	}
	sort.Strings(out)
	return out, nil
}
