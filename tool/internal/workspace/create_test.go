package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/validate"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

func create(t *testing.T) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "demo",
		Actor: "tester",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return l
}

func TestCreateLeavesBehindWhatTheDesignNames(t *testing.T) {
	l := create(t)

	for _, p := range []string{
		".gitignore", ".git", ".bare", "store", "var/site",
		"doc/convention", "doc/task", "doc/topic", "doc/asset", "doc/archive", "doc/resource",
		"doc/template/brief.md", "doc/template/handoff.md", "doc/template/manifest.md",
		"handoff", "var/lease", "var/occupy", "var/state", "var/env", "var/cache",
		"var/config", "var/secrets", "job", "worktree",
	} {
		if _, err := os.Stat(filepath.Join(l.Root, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, sub := range workspace.JobSubdirs {
		if _, err := os.Stat(filepath.Join(l.Root, "job", sub)); err != nil {
			t.Errorf("job/%s: %v", sub, err)
		}
	}

	site, err := workspace.ReadSite(l)
	if err != nil || site != "demo" {
		t.Fatalf("var/site: %q, %v", site, err)
	}
}

func TestTheIgnoreRulesAreInBothPlaces(t *testing.T) {
	l := create(t)

	gi, err := os.ReadFile(l.GitIgnore())
	if err != nil {
		t.Fatal(err)
	}
	ex, err := os.ReadFile(filepath.Join(l.Root, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gi) != string(ex) {
		t.Error("`.gitignore` and `.git/info/exclude` should hold the same list")
	}
	for _, want := range []string{".gitignore", "/.bare/", "/store/", "/var/", "/job/", "/worktree/"} {
		if !strings.Contains(string(gi), want) {
			t.Errorf("the ignore rules do not cover %s", want)
		}
	}
}

func TestTheFirstCommitHoldsTheTemplateAndNothingElse(t *testing.T) {
	l := create(t)

	out, err := gitx.Run(l.Root, "show", "--name-only", "--pretty=format:%s", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "init: created from the template") {
		t.Errorf("first commit subject: %q", out)
	}
	for _, want := range []string{"doc/template/brief.md", "doc/template/handoff.md", "doc/template/manifest.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("the first commit does not track %s", want)
		}
	}
	for _, unwanted := range []string{"var/", "job/", "worktree/", ".gitignore", ".bare"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the first commit should not track %s", unwanted)
		}
	}

	// Only doc/ and handoff/ are tracked at all.
	tracked, err := gitx.Run(l.Root, "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(tracked), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "doc/") && !strings.HasPrefix(line, "handoff/") {
			t.Errorf("tracked file outside doc/ and handoff/: %s", line)
		}
	}
}

func TestCreateRefusesAnExistingWorkspace(t *testing.T) {
	l := create(t)
	if _, err := workspace.Create(workspace.CreateOptions{Root: l.Root, Site: "other"}); err == nil {
		t.Fatal("creating over a workspace should fail")
	}
}

func TestCreateNeedsASite(t *testing.T) {
	if _, err := workspace.Create(workspace.CreateOptions{Root: filepath.Join(t.TempDir(), "w")}); err == nil {
		t.Fatal("creating without a site name should fail")
	}
}

// A relative root used to put the two bare repositories one level too deep, because
// git resolves a relative path argument against the directory -C names rather than
// against the caller's.
func TestCreateWorksFromARelativeRoot(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)

	l, err := workspace.Create(workspace.CreateOptions{Root: "nested/workspace", Site: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(l.Root) {
		t.Errorf("the layout root should be absolute, got %q", l.Root)
	}
	for _, p := range []string{".bare", "store", "doc/template/brief.md"} {
		if _, err := os.Stat(filepath.Join(parent, "nested", "workspace", p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(parent, "nested", "workspace", "nested")); err == nil {
		t.Error("the repositories were created one level too deep")
	}
}

func TestAFreshWorkspaceValidates(t *testing.T) {
	l := create(t)
	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestValidationCatchesABriefThatDisagreesWithItsDirectory(t *testing.T) {
	l := create(t)
	writeBrief(t, l, "good-name", "wrong-name", "## Goal\n\n## Scope\n\n## Acceptance criteria\n")

	findings := validate.Validate(l)
	if !mentions(findings, "`job` equals its directory name") {
		t.Fatalf("expected a finding about the job id, got %v", findings)
	}
}

func TestValidationCatchesAMissingSection(t *testing.T) {
	l := create(t)
	writeBrief(t, l, "short-brief", "short-brief", "## Goal\n\n## Scope\n")

	findings := validate.Validate(l)
	if !mentions(findings, "required section") {
		t.Fatalf("expected a finding about the missing section, got %v", findings)
	}
}

func TestValidationCatchesADirectoryWithNoResourceEntry(t *testing.T) {
	l := create(t)
	if err := os.MkdirAll(l.Worktree("uploader"), 0o755); err != nil {
		t.Fatal(err)
	}

	findings := validate.Validate(l)
	if !mentions(findings, "worktree.yaml") {
		t.Fatalf("expected a finding about the undeclared worktree, got %v", findings)
	}
}

func TestValidationAcceptsADeclaredWorktreeWithNoDirectory(t *testing.T) {
	l := create(t)
	// A declaration is a recipe: not being materialised here is normal, so the
	// reverse direction must not be reported.
	registry := []byte("worktree:\n  - name: uploader\n    purpose: Uploader reliability work\n    ephemeral: false\n")
	if err := os.WriteFile(l.ResourceKind("worktree"), registry, 0o644); err != nil {
		t.Fatal(err)
	}
	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestValidationCatchesADuplicateConventionNumber(t *testing.T) {
	l := create(t)
	for _, name := range []string{"01-placement.md", "01-naming.md"} {
		write(t, filepath.Join(l.Convention(), name), "# Title\n\nbody\n")
	}
	findings := validate.Validate(l)
	if !mentions(findings, "share a number") {
		t.Fatalf("expected a duplicate-number finding, got %v", findings)
	}
}

func TestValidationCatchesADocumentWithoutAnH1(t *testing.T) {
	l := create(t)
	write(t, filepath.Join(l.Convention(), "01-placement.md"), "no title here\n\nbody\n")
	findings := validate.Validate(l)
	if !mentions(findings, "H1") {
		t.Fatalf("expected an H1 finding, got %v", findings)
	}
}

func TestValidationCatchesALeaseWithNoDeclaration(t *testing.T) {
	l := create(t)
	lease := []byte("resource: gpu\nunit: \"1\"\noccupant: tester\njob: some-job\nsince: 2026-10-09T10:00:00Z\nexpires: 2026-10-09T12:00:00Z\n")
	write(t, l.LeaseFile("gpu", "1"), string(lease))

	findings := validate.Validate(l)
	if !mentions(findings, "names a declaration") {
		t.Fatalf("expected a finding about the missing declaration, got %v", findings)
	}
}

// writeBrief writes a brief straight into a job directory, bypassing the tool, which
// is exactly how a hand-written brief arrives.
func writeBrief(t *testing.T, l workspace.Layout, dir, job, body string) {
	t.Helper()
	if err := os.MkdirAll(l.TaskDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	front, err := format.Encode(format.Brief{
		Job:    job,
		Status: format.StatusActive,
		Branch: "job/" + dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, l.Brief(dir), string(format.Join(front, []byte(body))))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mentions(findings []validate.Finding, needle string) bool {
	for _, f := range findings {
		if strings.Contains(f.Rule+" "+f.What, needle) {
			return true
		}
	}
	return false
}
