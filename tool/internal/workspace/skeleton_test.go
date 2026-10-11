package workspace_test

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"

	root "github.com/multiagentworkspace/thrum"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// The template and the register of what the tooling owns have to agree.
//
// A template file that no one claims is a file no workspace is ever told about: `create` copies
// it and nothing else knows it exists, so `verify` would not report it missing and `update`
// would not put it back. That is the failure this test exists to make loud, and it is why the
// register is written out rather than derived.
func TestTheSkeletonIsEveryTemplateFileTheProjectDoesNotOwn(t *testing.T) {
	claimed := map[string]bool{}
	for _, file := range workspace.SkeletonFiles() {
		if claimed[file.Path] {
			t.Errorf("%s is claimed twice", file.Path)
		}
		claimed[file.Path] = true
		if _, err := fs.Stat(root.Template, root.TemplateDir+"/"+file.Path); err != nil {
			t.Errorf("the register claims %s, and the template does not carry it: %v", file.Path, err)
		}
	}

	var unclaimed []string
	err := fs.WalkDir(root.Template, root.TemplateDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root.TemplateDir+"/")
		if claimed[rel] {
			return nil
		}
		// The project's copy-me files are reported rather than restored, and the ignore
		// list is written into two places rather than one, so neither is a skeleton file.
		if strings.HasPrefix(rel, "doc/template/") || rel == ".gitignore" {
			return nil
		}
		unclaimed = append(unclaimed, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(unclaimed)
	for _, rel := range unclaimed {
		t.Errorf("the template carries %s and no one claims it: nothing would restore it, or report it missing", rel)
	}
}

// A skeleton file that is not ignored is a skeleton file `clean` would delete, so the rule each
// one derives is part of the register rather than a detail.
func TestEverySkeletonFileDerivesAnIgnoreRule(t *testing.T) {
	for _, file := range workspace.SkeletonFiles() {
		want := "/" + file.Path
		if i := strings.IndexByte(file.Path, '/'); i >= 0 {
			want = "/" + file.Path[:i] + "/"
		}
		if got := file.Ignore(); got != want {
			t.Errorf("%s should be ignored by %q, and says %q", file.Path, want, got)
		}
	}
}

// A reference file the entry point never names is a file no actor will read. The skill is an
// index as well as a manual, so its index is checked the way every other cross-reference in a
// workspace is: mechanically.
func TestTheSkillNamesEveryFileBesideIt(t *testing.T) {
	const dir = ".agents/skills/workspace-actor/"
	skill, err := fs.ReadFile(root.Template, root.TemplateDir+"/"+dir+"SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range workspace.SkeletonFiles() {
		if !strings.HasPrefix(file.Path, dir) {
			continue // the entry point at the workspace root is not beside the skill
		}
		name := path.Base(file.Path)
		if name == "SKILL.md" || path.Ext(name) != ".md" {
			continue
		}
		if !strings.Contains(string(skill), name) {
			t.Errorf("SKILL.md never names %s, so no actor would read it", name)
		}
	}
}
