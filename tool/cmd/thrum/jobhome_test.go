package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"os/exec"

	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

// A claim belongs where the job is, so a job no site holds cannot be claimed: there is
// nothing to claim, and a claim that nobody can read authoritatively is worse than none.
func TestAJobNoSiteHoldsCannotBeClaimed(t *testing.T) {
	l := materialised(t, "build-01", "add-retry")

	// A job whose working directory was released is held nowhere.
	if _, err := worktrees.Release(l, "uploader"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveJob(l, "add-retry"); err == nil {
		t.Fatal("a job held nowhere should not resolve to a home")
	} else if !strings.Contains(err.Error(), "no site holds") {
		t.Errorf("the refusal should say what is missing: %v", err)
	}

	// And one that was never materialised is the same case.
	if _, err := resolveJob(l, "never-materialised"); err == nil {
		t.Error("a job that was never materialised should not resolve")
	}
}

// The site that holds the job is the one whose claim counts, and silence from a site that
// might hold it is never read as absence.
func TestTheClaimIsReadWhereTheJobIs(t *testing.T) {
	l := materialised(t, "build-01", "add-retry")

	home, err := resolveJob(l, "add-retry")
	if err != nil {
		t.Fatal(err)
	}
	if !home.local || home.remote != "" {
		t.Fatalf("the job is materialised here: %+v", home)
	}
	if home.claim.Occupant != "" {
		t.Errorf("nobody has claimed it yet: %+v", home.claim)
	}

	// A claim written here is what the resolution reads back.
	if err := occupancy.Occupy(l, "add-retry", "claude-opus", false, now()); err != nil {
		t.Fatal(err)
	}
	home, err = resolveJob(l, "add-retry")
	if err != nil {
		t.Fatal(err)
	}
	if home.claim.Occupant != "claude-opus" {
		t.Errorf("the claim should be the answer: %+v", home.claim)
	}
	line, err := whoIsOn(l, "add-retry")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "claude-opus") || !strings.Contains(line, "build-01") {
		t.Errorf("who is on it: %s", line)
	}

	// Asking is a query, so a job held nowhere is answered rather than refused: the
	// gate is occupy, not this.
	if _, err := worktrees.Release(l, "uploader"); err != nil {
		t.Fatal(err)
	}
	line, err = whoIsOn(l, "add-retry")
	if err != nil {
		t.Fatalf("asking should answer: %v", err)
	}
	if !strings.Contains(line, "nobody is on") || !strings.Contains(line, "no site holds") {
		t.Errorf("the answer should say nobody is on it: %s", line)
	}
}

// A claim at a site that no longer holds the job is stale, because a claim follows the
// working directory.
func TestASweptClaimIsOneThisSiteHasNoBusinessHolding(t *testing.T) {
	l := materialised(t, "build-01", "add-retry")
	if err := occupancy.Occupy(l, "add-retry", "claude-opus", false, now()); err != nil {
		t.Fatal(err)
	}
	// While the job is here, the claim is live.
	swept, err := occupancy.Sweep(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(swept) != 0 {
		t.Errorf("a live claim should survive a sweep: %v", swept)
	}

	// Once the working directory is released, this site holds nothing, and neither does
	// its claim.
	if _, err := worktrees.Release(l, "uploader"); err != nil {
		t.Fatal(err)
	}
	swept, err = occupancy.Sweep(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(swept) != 1 || swept[0] != "add-retry" {
		t.Errorf("a claim at a site that no longer holds the job should be swept: %v", swept)
	}
	if _, ok, _ := occupancy.Get(l, "add-retry"); ok {
		t.Error("the stale claim is still there")
	}
}

// materialised is a workspace with a job checked out in a working directory, which is
// what having a home means.
func materialised(t *testing.T, site, job string) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  site,
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := "worktree:\n  - name: uploader\n    purpose: work\n    ephemeral: false\n"
	if err := os.WriteFile(l.ResourceKind("worktree"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	// A code repository with a commit, so a branch can exist.
	clone := filepath.Join(t.TempDir(), "clone")
	if err := runGit("", "clone", "--quiet", l.CodeRepo(), clone); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "project: first commit"},
		{"push", "--quiet", "origin", "HEAD:refs/heads/main"},
	} {
		if err := runGit(clone, args...); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.MkdirAll(l.TaskDir(job), 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: " + job + "\nstatus: active\nbranch: job/" + job + "\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(l.Brief(job), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := worktrees.Add(l, "uploader", job, ""); err != nil {
		t.Fatal(err)
	}
	return l
}

// runGit runs one git command, for the setup these tests need.
func runGit(dir string, args ...string) error {
	all := args
	if dir != "" {
		all = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", all...)
	return cmd.Run()
}

// A name is what a handoff records, so where it comes from is behaviour rather than a detail:
// the manual an actor reads states this order, and a silent change to it would let every agent
// on a machine be recorded as the person logged into it.
func TestTheActorNameComesFromTheEnvironmentAndNowhereElse(t *testing.T) {
	t.Setenv("THRUM_ACTOR", "")
	t.Setenv("USER", "Ada.Lovelace")
	t.Setenv("LOGNAME", "ignored")
	if got := defaultActor(); got != "ada-lovelace" {
		t.Errorf("$USER should be the fallback, slugified, and it gave %q", got)
	}

	t.Setenv("THRUM_ACTOR", "Claude Opus")
	if got := defaultActor(); got != "claude-opus" {
		t.Errorf("$THRUM_ACTOR should win over $USER, and it gave %q", got)
	}

	// An actor that is not a name at all is not a name: the workspace would have nothing to
	// record, and every actor would collide on the same word.
	t.Setenv("THRUM_ACTOR", "!!!")
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	if got := defaultActor(); got != "unknown" {
		t.Errorf("nothing usable should fall back to unknown, and it gave %q", got)
	}
}
