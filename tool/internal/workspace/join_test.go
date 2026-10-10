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

// Joining takes the shared line rather than beginning one, so the joined site's history
// is the workspace's history: one first commit, not two.
func TestJoiningTakesTheSharedLineAndCommitsNothing(t *testing.T) {
	first := workspaceFor(t, "build-01")
	shared := filepath.Join(t.TempDir(), "shared.git")
	gitRun(t, first.Root, "init", "--bare", "--quiet", shared)
	gitRun(t, first.Root, "remote", "add", "origin", shared)
	gitRun(t, first.Root, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	// The roster is what the other sites read, so the new site is declared before it
	// joins rather than after.
	if err := os.WriteFile(first.ResourceKind("site"),
		[]byte("site:\n  - name: build-01\n    purpose: The build machine\n  - name: lab-02\n    purpose: The lab machine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(first.Root, "add", "-A", "--", "doc"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(first.Root, append(gitx.IdentityArgs(first.Root, "tester"),
		"commit", "--quiet", "-m", "resource: declared the sites")...); err != nil {
		t.Fatal(err)
	}
	gitRun(t, first.Root, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	second, err := workspace.Join(workspace.JoinOptions{
		Root:   filepath.Join(t.TempDir(), "lab"),
		Remote: shared,
		Site:   "lab-02",
	})
	if err != nil {
		t.Fatalf("join: %v", err)
	}

	// The history is the shared one, all of it, and no new commit was made here.
	out, err := gitx.Run(second.Root, "log", "--pretty=%s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "init: created from the template") {
		t.Errorf("the joined site should have the workspace's history:\n%s", out)
	}
	if strings.Count(out, "init: created from the template") != 1 {
		t.Errorf("a joined site must not begin a second history:\n%s", out)
	}
	if !strings.Contains(out, "resource: declared the sites") {
		t.Errorf("and everything committed since it began:\n%s", out)
	}

	// What a repository cannot carry, the join fills in.
	site, err := workspace.ReadSite(second)
	if err != nil || site != "lab-02" {
		t.Errorf("the site should name itself: %q %v", site, err)
	}
	for _, path := range []string{".gitignore", ".git/info/exclude", "var/site", "job", "worktree", "handoff"} {
		if _, err := os.Stat(filepath.Join(second.Root, path)); err != nil {
			t.Errorf("%s should exist after joining: %v", path, err)
		}
	}
	if !gitx.IsRepo(second.CodeRepo()) || !gitx.IsRepo(second.Store()) {
		t.Error("a joined site needs its own code and asset repositories, even empty ones")
	}

	// And it is a workspace that passes its own checks.
	if findings := validate.Validate(second); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

// A site the roster does not declare is refused: the roster is what the others read.
func TestJoiningASiteTheRosterDoesNotDeclareIsRefused(t *testing.T) {
	first := workspaceFor(t, "build-01")
	shared := filepath.Join(t.TempDir(), "shared.git")
	gitRun(t, first.Root, "init", "--bare", "--quiet", shared)
	gitRun(t, first.Root, "remote", "add", "origin", shared)
	if err := os.WriteFile(first.ResourceKind("site"),
		[]byte("site:\n  - name: build-01\n    purpose: The build machine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(first.Root, "add", "-A", "--", "doc"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(first.Root, append(gitx.IdentityArgs(first.Root, "tester"),
		"commit", "--quiet", "-m", "resource: declared the site")...); err != nil {
		t.Fatal(err)
	}
	gitRun(t, first.Root, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	_, err := workspace.Join(workspace.JoinOptions{
		Root:   filepath.Join(t.TempDir(), "nowhere"),
		Remote: shared,
		Site:   "lab-02",
	})
	if err == nil {
		t.Fatal("a site the roster does not declare should be refused")
	}
	if !strings.Contains(err.Error(), "roster does not declare") {
		t.Errorf("the refusal should say what is missing: %v", err)
	}
}

func TestJoiningNeedsARemoteAndASiteName(t *testing.T) {
	if _, err := workspace.Join(workspace.JoinOptions{Root: filepath.Join(t.TempDir(), "x"), Site: "s"}); err == nil {
		t.Error("joining without a remote should be refused")
	}
	if _, err := workspace.Join(workspace.JoinOptions{Root: filepath.Join(t.TempDir(), "y"), Remote: "somewhere"}); err == nil {
		t.Error("joining without a site name should be refused")
	}
}

// workspaceFor is a workspace that has just been created, which is what a site is before
// anybody joins it.
func workspaceFor(t *testing.T, site string) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  site,
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(dir, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}
