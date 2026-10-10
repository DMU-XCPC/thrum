package jobs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/validate"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

// fixture is a workspace with one declared working directory and a code repository
// that has a commit in it, which is what a project looks like before any job exists.
func fixture(t *testing.T) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "demo",
		Actor: "tester",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	registry := "worktree:\n" +
		"  - name: uploader\n" +
		"    purpose: Uploader reliability work\n" +
		"    ephemeral: false\n" +
		"  - name: spike\n" +
		"    purpose: A one-off look at something\n" +
		"    ephemeral: true\n"
	if err := os.WriteFile(l.ResourceKind("worktree"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCode(t, l)
	return l
}

// seedCode puts one commit on the code repository's default branch. The repository is
// bare, so the commit is made in a clone and pushed, which is also how a project
// would start one.
func seedCode(t *testing.T, l workspace.Layout) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	if _, err := gitx.Run("", "clone", "--quiet", l.CodeRepo(), clone); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "--quiet", "-m", "project: first commit"},
		{"push", "--quiet", "origin", "HEAD:refs/heads/main"},
	} {
		all := append(gitx.IdentityArgs(clone, "tester"), args...)
		if _, err := gitx.Run(clone, all...); err != nil {
			t.Fatalf("seeding the code repository (%v): %v", args, err)
		}
	}
}

func TestTheLifeOfAJob(t *testing.T) {
	l := fixture(t)

	// Open.
	id, err := jobs.Open(l, jobs.OpenOptions{
		Job:        "add-retry-to-uploader",
		Owner:      "claude-opus",
		OwnedPaths: []string{"src/upload/**"},
		Label:      "uploader",
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := subjectOf(t, l, id); got != "brief add-retry-to-uploader: opened in worktree uploader" {
		t.Errorf("commit subject: %q", got)
	}

	b, _, err := workspace.ReadBrief(l, "add-retry-to-uploader")
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != format.StatusActive || b.Owner != "claude-opus" || b.Branch != "job/add-retry-to-uploader" {
		t.Errorf("brief: %+v", b)
	}
	if held, materialised, err := worktrees.Holder(l, "uploader"); err != nil || !materialised || held != "add-retry-to-uploader" {
		t.Errorf("the label should hold the job: %q %v %v", held, materialised, err)
	}
	// Whether a job has started is derived, not stored: it is read from the handoff
	// directory being empty.
	if jobs.Started(l, "add-retry-to-uploader") {
		t.Error("a job with no handoff has not started")
	}

	// A job that has not started is not blocked, and has no last handoff.
	if last, _, _ := jobs.LastHandoff(l, "add-retry-to-uploader"); last != nil {
		t.Error("no handoff should exist yet")
	}

	// Hand off.
	started := "2026-10-09T02:10:00Z"
	if _, err := jobs.WriteHandoff(l, jobs.HandoffOptions{
		Job:       "add-retry-to-uploader",
		Agent:     "claude-opus",
		Status:    format.HandoffPartial,
		Next:      "fix the flaky timeout",
		Started:   started,
		Worktrees: []string{"uploader"},
	}); err != nil {
		t.Fatalf("handoff: %v", err)
	}

	last, path, err := jobs.LastHandoff(l, "add-retry-to-uploader")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "2026-10-09T02-10-00Z.md" {
		t.Errorf("handoff file name: %q", filepath.Base(path))
	}
	if last.Started != started {
		t.Errorf("started: %q", last.Started)
	}
	if last.Base != last.Head {
		t.Errorf("nothing was committed, so base and head should agree: %q %q", last.Base, last.Head)
	}

	// The same second twice is a collision, not an edit.
	if _, err := jobs.WriteHandoff(l, jobs.HandoffOptions{
		Job: "add-retry-to-uploader", Agent: "claude-opus",
		Status: format.HandoffPartial, Next: "again", Started: started,
	}); err == nil {
		t.Fatal("a second handoff with the same start should be refused")
	}

	// Blocked is derived from the latest handoff, not stored.
	if _, err := jobs.WriteHandoff(l, jobs.HandoffOptions{
		Job: "add-retry-to-uploader", Agent: "claude-opus",
		Status:    format.HandoffBlocked,
		BlockedBy: []string{"fix-ci-timeouts"},
		Started:   "2026-10-09T04:00:00Z",
	}); err != nil {
		t.Fatalf("blocked handoff: %v", err)
	}
	if !jobs.Blocked(l, "add-retry-to-uploader") {
		t.Error("the job should be blocked now")
	}

	// Close.
	if _, err := jobs.Close(l, "add-retry-to-uploader", format.StatusDone, ""); err != nil {
		t.Fatalf("close: %v", err)
	}
	b, path, err = workspace.ReadBrief(l, "add-retry-to-uploader")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, filepath.Join("doc", "archive", "task")) {
		t.Errorf("a closed brief belongs in the archive, found %s", path)
	}
	if b.Status != format.StatusDone {
		t.Errorf("status after closing: %q", b.Status)
	}
	if _, err := os.Stat(l.Brief("add-retry-to-uploader")); err == nil {
		t.Error("the brief should have left doc/task/")
	}
	if e, ok, _ := worktrees.Find(l, "uploader"); !ok || !e.Detached || e.Job() != "" {
		t.Errorf("closing should leave the label idle, got %+v", e)
	}
	if got := subjectOf(t, l, "HEAD"); !strings.HasPrefix(got, "close add-retry-to-uploader") {
		t.Errorf("closing commit subject: %q", got)
	}

	// An id is taken forever.
	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "add-retry-to-uploader", Label: "uploader"}); err == nil {
		t.Fatal("reopening a closed job should be refused")
	}

	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

// A job branches from the project's history. Without any, there is no branch to make,
// and saying so now beats a failure at the first handoff that mentions ancestors.
func TestOpenRefusesAnEmptyCodeRepository(t *testing.T) {
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "demo",
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := "worktree:\n  - name: uploader\n    purpose: work\n    ephemeral: false\n"
	if err := os.WriteFile(l.ResourceKind("worktree"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = jobs.Open(l, jobs.OpenOptions{Job: "add-retry", Label: "uploader"})
	if err == nil {
		t.Fatal("opening a job in a project with no commits should be refused")
	}
	if !strings.Contains(err.Error(), "no commits") {
		t.Errorf("the refusal should say what is missing: %v", err)
	}
	// And nothing was left behind by the attempt.
	if _, err := os.Stat(l.TaskDir("add-retry")); err == nil {
		t.Error("a refused open left a brief behind")
	}
}

func TestOpenRefusesWhatItMust(t *testing.T) {
	l := fixture(t)

	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "job-one", Label: "never-declared"}); err == nil {
		t.Error("an undeclared label should be refused")
	}
	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "Not A Slug", Label: "uploader"}); err == nil {
		t.Error("a job id that is not a slug should be refused")
	}
	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "job-one"}); err == nil {
		t.Error("a job with no working directory should be refused")
	}

	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "job-one", Label: "uploader"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	// The label is occupied, so a second job may not take it.
	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "job-two", Label: "uploader"}); err == nil {
		t.Error("an occupied label should be refused")
	}
	// A different area is not a conflict at all.
	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "job-two", Label: "spike"}); err != nil {
		t.Errorf("a free label should be usable: %v", err)
	}

	// A failed open leaves nothing behind.
	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "job-three", Label: "hidden"}); err == nil {
		t.Fatal("an undeclared label should be refused")
	}
	if _, err := os.Stat(l.TaskDir("job-three")); err == nil {
		t.Error("a refused open left a brief behind")
	}
}

func TestOwnershipCanBeGivenUpAndClaimed(t *testing.T) {
	l := fixture(t)
	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "job-one", Label: "uploader"}); err != nil {
		t.Fatal(err)
	}

	// Nothing owns it, so anybody may take it without asking.
	if _, err := jobs.SetOwner(l, "job-one", "claude-opus", ""); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := workspace.ReadBrief(l, "job-one"); b.Owner != "claude-opus" {
		t.Errorf("owner: %q", b.Owner)
	}

	if _, err := jobs.SetOwner(l, "job-one", "", ""); err != nil {
		t.Fatal(err)
	}
	b, _, err := workspace.ReadBrief(l, "job-one")
	if err != nil {
		t.Fatal(err)
	}
	if b.Owner != "" {
		t.Errorf("a job that was given up should be unowned, got %q", b.Owner)
	}
	if got := subjectOf(t, l, "HEAD"); got != "brief job-one: ownership given up" {
		t.Errorf("commit subject: %q", got)
	}
	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

// The ordinary path: a label freed by one job is the label the next one takes. The
// next job's branch has never existed before, so acquiring the directory has to make
// it.
func TestALabelReleasedByOneJobIsTheLabelTheNextOneTakes(t *testing.T) {
	l := fixture(t)

	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "first-job", Label: "uploader"}); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Close(l, "first-job", format.StatusDone, ""); err != nil {
		t.Fatal(err)
	}
	if e, ok, err := worktrees.Find(l, "uploader"); err != nil || !ok || !e.Detached {
		t.Fatalf("closing should leave the label idle: %+v %v", e, err)
	}

	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "second-job", Label: "uploader"}); err != nil {
		t.Fatalf("a released label should be usable again: %v", err)
	}
	held, materialised, err := worktrees.Holder(l, "uploader")
	if err != nil || !materialised || held != "second-job" {
		t.Errorf("the label should hold the new job: %q %v %v", held, materialised, err)
	}
	if e, ok, _ := worktrees.Find(l, "uploader"); !ok || e.Branch != "job/second-job" {
		t.Errorf("the directory should be on the new job's branch: %+v", e)
	}
	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestHandoffRefusesABadStatus(t *testing.T) {
	l := fixture(t)
	if _, err := jobs.Open(l, jobs.OpenOptions{Job: "job-one", Label: "uploader"}); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.WriteHandoff(l, jobs.HandoffOptions{
		Job: "job-one", Agent: "tester", Status: "nearly",
	}); err == nil {
		t.Error("an invented status should be refused")
	}
	// A finished handoff says next: none.
	if _, err := jobs.WriteHandoff(l, jobs.HandoffOptions{
		Job: "job-one", Agent: "tester", Status: format.HandoffDone, Next: "something else",
	}); err == nil {
		t.Error("a finished handoff with work remaining should be refused")
	}
	if _, err := jobs.WriteHandoff(l, jobs.HandoffOptions{
		Job: "job-one", Agent: "tester", Status: format.HandoffDone,
	}); err != nil {
		t.Errorf("a finished handoff should be accepted: %v", err)
	}
	if last, _, _ := jobs.LastHandoff(l, "job-one"); last.Next != "none" {
		t.Errorf("next: %q", last.Next)
	}
}

func subjectOf(t *testing.T, l workspace.Layout, rev string) string {
	t.Helper()
	out, err := gitx.Run(l.Root, "log", "-1", "--pretty=format:%s", rev)
	if err != nil {
		t.Fatalf("log %s: %v", rev, err)
	}
	return out
}
