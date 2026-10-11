package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/validate"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// A workspace that was just created is the tooling's template, so there is nothing to
// derive and nothing to write.
func TestAFreshWorkspaceIsUpToDate(t *testing.T) {
	l := workspaceFor(t, "build-01")
	drift, err := workspace.Compare(l)
	if err != nil {
		t.Fatal(err)
	}
	if !drift.Empty() {
		t.Fatalf("a fresh workspace should have no drift: %+v", drift)
	}
	res, err := workspace.Update(l, workspace.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DirsCreated) != 0 || res.IgnoreWritten || len(res.Templates) != 0 {
		t.Errorf("nothing should have been written: %+v", res)
	}
}

// What the tooling carries and a workspace has lost is derived, not remembered: a
// directory that went away is reported, and bringing it back loses nothing.
func TestDriftIsDerivedAndTheSkeletonIsRestored(t *testing.T) {
	l := workspaceFor(t, "build-01")
	if err := os.RemoveAll(l.Lease()); err != nil {
		t.Fatal(err)
	}

	drift, err := workspace.Compare(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift.MissingDirs) != 1 || drift.MissingDirs[0] != "var/lease" {
		t.Fatalf("the missing directory should be derived: %+v", drift.MissingDirs)
	}
	// And it is a finding, because nothing else would ever notice.
	found := false
	for _, f := range validate.Validate(l) {
		if strings.Contains(f.String(), "var/lease") {
			found = true
		}
	}
	if !found {
		t.Error("a missing directory should be reported by verify")
	}

	if _, err := workspace.Update(l, workspace.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if drift, err = workspace.Compare(l); err != nil || !drift.Empty() {
		t.Errorf("after updating, there should be nothing missing: %+v %v", drift, err)
	}
}

// The agent entry point and the skill it names are the tooling's, like the ignore rules:
// losing one is derived rather than remembered, reported by verify, and restored by
// update. Nothing reads what a workspace wrote there, which is what keeps the map a map.
func TestTheAgentSkeletonIsRestoredAndNeverMerged(t *testing.T) {
	l := workspaceFor(t, "build-01")

	entry := filepath.Join(l.Root, "AGENTS.md")
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(l.Root, ".agents", "skills", "workspace-actor", "SKILL.md")
	mine := []byte("---\nname: workspace-actor\ndescription: ours\n---\n\nWe say it differently here.\n")
	if err := os.WriteFile(skill, mine, 0o644); err != nil {
		t.Fatal(err)
	}

	drift, err := workspace.Compare(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift.Skeleton) != 2 {
		t.Fatalf("both files should have drifted: %+v", drift.Skeleton)
	}

	// verify reports it, and says which one is simply gone.
	findings := validate.Validate(l)
	if !mentions(findings, "AGENTS.md") || !mentions(findings, "SKILL.md") {
		t.Errorf("verify should report the skeleton: %v", findings)
	}
	if !mentions(findings, "is not there") {
		t.Errorf("verify should say the entry point is not there: %v", findings)
	}

	res, err := workspace.Update(l, workspace.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SkeletonWritten) != 2 {
		t.Fatalf("update should restore the skeleton: %+v", res)
	}
	if got, err := os.ReadFile(skill); err != nil || string(got) == string(mine) {
		t.Errorf("update should have replaced the workspace's copy: %v", err)
	}
	if drift, err = workspace.Compare(l); err != nil || !drift.Empty() {
		t.Errorf("after updating there should be no drift: %+v %v", drift, err)
	}
	if findings := validate.Validate(l); len(findings) != 0 {
		t.Errorf("after updating verify should be quiet: %v", findings)
	}
}

// Losing an ignore rule is the safety half: it is reported, restored, and the workspace's
// own extra rules survive unless narrowing is asked for.
func TestTheIgnoreRulesAreRestoredAndExtrasAreKept(t *testing.T) {
	l := workspaceFor(t, "build-01")

	// Somebody removed a rule, and added one of their own.
	content, err := os.ReadFile(l.GitIgnore())
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.Replace(string(content), "/store/\n", "", 1)
	trimmed += "\n# my own\n/build-output/\n"
	if err := os.WriteFile(l.GitIgnore(), []byte(trimmed), 0o644); err != nil {
		t.Fatal(err)
	}

	drift, err := workspace.Compare(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift.IgnoreMissing) != 1 || drift.IgnoreMissing[0] != "/store/" {
		t.Fatalf("the missing rule should be derived: %+v", drift.IgnoreMissing)
	}
	if len(drift.IgnoreExtra) != 1 || drift.IgnoreExtra[0] != "/build-output/" {
		t.Fatalf("the extra rule should be derived: %+v", drift.IgnoreExtra)
	}

	res, err := workspace.Update(l, workspace.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IgnoreWritten || len(res.IgnoreKept) != 1 {
		t.Fatalf("the rules should be rewritten and the extra kept: %+v", res)
	}
	for _, path := range []string{l.GitIgnore(), filepath.Join(l.Root, ".git", "info", "exclude")} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "/store/") {
			t.Errorf("%s should have the restored rule:\n%s", path, got)
		}
		if !strings.Contains(string(got), "/build-output/") {
			t.Errorf("%s should keep this workspace's own rule:\n%s", path, got)
		}
	}

	// Narrowing is a decision, so it takes asking.
	if _, err := workspace.Update(l, workspace.UpdateOptions{Narrow: true}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(l.GitIgnore())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "/build-output/") {
		t.Errorf("--narrow should drop the extra rule:\n%s", got)
	}
	if drift, err = workspace.Compare(l); err != nil || drift.Empty() == false {
		t.Errorf("after narrowing, the rules are the tooling's: %+v %v", drift, err)
	}
}

// A copy-me file is the project's, so the tooling reports the difference and does not
// write it unless it is asked to.
func TestTemplateDifferencesAreReportedAndOnlyWrittenOnRequest(t *testing.T) {
	l := workspaceFor(t, "build-01")
	paths, err := filepath.Glob(filepath.Join(l.DocTemplate(), "*.md"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("the template should carry copy-me files: %v %v", paths, err)
	}
	mine := "# Our own brief\n\nWe say it differently here.\n"
	brief := filepath.Join(l.DocTemplate(), "brief.md")
	if err := os.WriteFile(brief, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	// The workspace's own version is recorded, which is what a project that keeps a
	// different template would have done.
	gitRun(t, l.Root, "-c", "user.email=a@b", "-c", "user.name=a", "add", "--", "doc/template/brief.md")
	gitRun(t, l.Root, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "template brief: our own")

	drift, err := workspace.Compare(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift.Templates) != 1 || drift.Templates[0].Path != "doc/template/brief.md" {
		t.Fatalf("the difference should be derived: %+v", drift.Templates)
	}

	// Updating without asking leaves the workspace's own file exactly as it was.
	res, err := workspace.Update(l, workspace.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TemplatesAdopted) != 0 {
		t.Errorf("nothing should be adopted without being asked: %v", res.TemplatesAdopted)
	}
	if got, _ := os.ReadFile(brief); string(got) != mine {
		t.Errorf("the workspace's own template was changed:\n%s", got)
	}

	// Asking writes the tooling's copy, and leaves it for a person to record.
	res, err = workspace.Update(l, workspace.UpdateOptions{AdoptTemplate: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TemplatesAdopted) != 1 {
		t.Fatalf("adopting should have written the tooling's copy: %+v", res)
	}
	got, _ := os.ReadFile(brief)
	if strings.Contains(string(got), "Our own brief") {
		t.Errorf("adopting should have replaced it:\n%s", got)
	}
	if drift, err = workspace.Compare(l); err != nil || len(drift.Templates) != 0 {
		t.Errorf("after adopting there is no difference: %+v %v", drift, err)
	}
	// And it is uncommitted: adopting is not recording.
	out, _ := gitx.Run(l.Root, "status", "--porcelain", "--", "doc/template/brief.md")
	if strings.TrimSpace(out) == "" {
		t.Error("an adopted template should be a change waiting to be recorded")
	}
}

// A dry run says what would happen and writes nothing.
func TestADryRunWritesNothing(t *testing.T) {
	l := workspaceFor(t, "build-01")
	if err := os.RemoveAll(l.Lease()); err != nil {
		t.Fatal(err)
	}
	res, err := workspace.Update(l, workspace.UpdateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DirsCreated) != 1 {
		t.Fatalf("the dry run should say what it would create: %+v", res)
	}
	if _, err := os.Stat(l.Lease()); err == nil {
		t.Error("a dry run should write nothing")
	}
}
