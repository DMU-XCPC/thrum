// Package update brings the unversioned skeleton up to date with the template the
// tooling carries. It never touches a versioned file.
package update

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	thrum "github.com/multiagentworkspace/thrum"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Result is what update found and what it did.
type Result struct {
	Updated     []string `json:"updated"`
	Created     []string `json:"created"`
	AddedRules  []string `json:"added_rules"`
	CreatedDirs []string `json:"created_dirs"`
	Templates   []string `json:"templates"`
}

// Run updates the skeleton and the ignore rules, and reports the copy-me files
// that differ. A differing template is written only when force is set.
func Run(l layout.Layout, force, dryRun bool) (Result, error) {
	var r Result

	// The skeleton is the tooling's: bring it up to date.
	for _, f := range layout.SkeletonFiles() {
		want, err := fs.ReadFile(thrum.Template, thrum.TemplateDir+"/"+f.Path)
		if err != nil {
			return r, fmt.Errorf("template: %w", err)
		}
		path := filepath.Join(l.Root, filepath.FromSlash(f.Path))
		have, err := os.ReadFile(path)
		switch {
		case os.IsNotExist(err):
			r.Created = append(r.Created, f.Path)
		case err != nil:
			return r, err
		case bytes.Equal(have, want):
			continue
		default:
			r.Updated = append(r.Updated, f.Path)
		}
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return r, err
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return r, err
		}
	}

	// The ignore rules: add what is missing, never remove what a person added.
	required, err := workspace.IgnoreList()
	if err != nil {
		return r, err
	}
	var want []string
	for _, ln := range strings.Split(string(required), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			want = append(want, ln)
		}
	}
	for _, path := range []string{l.GitIgnore(), l.InfoExclude()} {
		have, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return r, err
		}
		haveRules := map[string]bool{}
		for _, ln := range strings.Split(string(have), "\n") {
			haveRules[strings.TrimSpace(ln)] = true
		}
		var added []string
		for _, rule := range want {
			if !haveRules[rule] {
				added = append(added, rule)
			}
		}
		if len(added) == 0 {
			continue
		}
		r.AddedRules = append(r.AddedRules, added...)
		if dryRun {
			continue
		}
		out := string(have)
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		for _, rule := range added {
			out += rule + "\n"
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return r, err
		}
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			return r, err
		}
	}

	// The empty directories a repository cannot carry.
	for _, dir := range l.EmptyDirs() {
		if _, err := os.Stat(dir); err == nil {
			continue
		}
		r.CreatedDirs = append(r.CreatedDirs, dir)
		if dryRun {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return r, err
		}
	}

	// The copy-me files belong to the project: report, and write only when asked.
	entries, err := fs.ReadDir(thrum.Template, thrum.TemplateDir+"/doc/template")
	if err != nil {
		return r, fmt.Errorf("template: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		want, err := fs.ReadFile(thrum.Template, thrum.TemplateDir+"/doc/template/"+e.Name())
		if err != nil {
			return r, fmt.Errorf("template: %w", err)
		}
		path := filepath.Join(l.DocTemplates(), e.Name())
		have, err := os.ReadFile(path)
		if err == nil && bytes.Equal(have, want) {
			continue
		}
		r.Templates = append(r.Templates, "doc/template/"+e.Name())
		if !force || dryRun {
			continue
		}
		if err := os.MkdirAll(l.DocTemplates(), 0o755); err != nil {
			return r, err
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return r, err
		}
	}
	return r, nil
}
