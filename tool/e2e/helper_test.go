package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCodeSyncAndRun: the actor stays at s1; s2 is a helper that receives the
// working tree (no .git, no ignored files) and runs commands there.
func TestCodeSyncAndRun(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	w1, w2 := ts.w1, ts.w2

	w1.r().run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	dir := w1.path("worktree", "up")
	for name, body := range map[string]string{
		"src.txt":       "hello\n",
		"untracked.txt": "new\n",
		".gitignore":    "ignored.bin\n",
		"ignored.bin":   "no\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	w2.r().run("site", "code", "on").ok(t, "code on")
	w2.r().run("site", "run", "on").ok(t, "run on")
	// bring the helper's daemon up with the new flags, then copy the source
	w2.r().run("sync").ok(t, "helper sync")
	w1.r().run("sync", "--code", "--job", "j1", "--site", "s2").ok(t, "sync code")

	src := w2.path("job", "j1", "data", "src")
	for _, f := range []string{"src.txt", "untracked.txt"} {
		if _, err := os.Stat(filepath.Join(src, f)); err != nil {
			t.Fatalf("%s did not reach the helper: %v", f, err)
		}
	}
	for _, f := range []string{"ignored.bin", ".git"} {
		if _, err := os.Stat(filepath.Join(src, f)); !os.IsNotExist(err) {
			t.Fatalf("%s should not have been copied", f)
		}
	}

	out := w1.r().run("run", "--site", "s2", "--job", "j1", "--",
		"sh", "-c", "cat src.txt; echo built > out.txt").ok(t, "run")
	if !strings.Contains(out.stdout, "hello") {
		t.Fatalf("the command's output did not come back: %s", out.stdout)
	}
	if _, err := os.Stat(filepath.Join(src, "out.txt")); err != nil {
		t.Fatalf("the command did not run in the snapshot: %v", err)
	}
	if logs, err := os.ReadDir(w2.path("job", "j1", "log")); err != nil || len(logs) == 0 {
		t.Fatalf("the helper wrote no run log: %v", err)
	}
	// the helper's repository is untouched: no thrum refs, no remote
	if refs := runGit(t, w2.path("repos", "code.git"), "show-ref"); strings.Contains(refs, "thrum") {
		t.Fatalf("a thrum ref appeared in the helper's repository: %s", refs)
	}

	// the exit status comes back
	w1.r().run("run", "--site", "s2", "--job", "j1", "--", "sh", "-c", "exit 3").
		fails(t, "a nonzero exit", "")

	// both opt-ins are required, and off means off
	w2.r().run("site", "code", "off").ok(t, "code off")
	w1.r().run("sync", "--code", "--job", "j1", "--site", "s2").fails(t, "code off", "refused")
	w2.r().run("site", "run", "off").ok(t, "run off")
	w1.r().run("run", "--site", "s2", "--job", "j1", "--", "true").fails(t, "run off", "refused")
}

// TestHeldAt: the job's writer site is recorded, and another site refuses to work it.
func TestHeldAt(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	ts.w1.r().run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	if show := ts.w1.r().run("job", "show", "j1").ok(t, "show").stdout; !strings.Contains(show, "held at s1") {
		t.Fatalf("the holder was not recorded: %s", show)
	}
	ts.w1.r().run("sync").ok(t, "sync")
	ts.w2.r().run("sync").ok(t, "w2 sync")
	if show := ts.w2.r().run("job", "show", "j1").ok(t, "show").stdout; !strings.Contains(show, "held at s1") {
		t.Fatalf("the helper did not learn the holder: %s", show)
	}
	ts.w2.r().run("work", "j1").fails(t, "working a job held elsewhere", "held at s1")
}

// TestActorPlace: the actor's mail place is visible, and status shows it.
func TestActorPlace(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare")
	r.run("actor", "hello").ok(t, "hello")
	if out := r.run("actor", "show", "a1").ok(t, "show").stdout; !strings.Contains(out, "at s1") {
		t.Fatalf("actor show does not report the place: %s", out)
	}
	if out := r.run("status").ok(t, "status").stdout; !strings.Contains(out, "mail  at s1") {
		t.Fatalf("status does not report the mail place: %s", out)
	}
}

// TestOneWayLinkGivesTheReverseRoute: one direction is enough. The helper reaches
// the holder; the holder then answers on the same live link, without an address for
// the helper.
func TestOneWayLinkGivesTheReverseRoute(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "hello", "--body", "-").ok(t, "b to a")
	time.Sleep(400 * time.Millisecond) // let the holder's route poll see the link
	if out := ts.w1.r().run("site", "routes").ok(t, "routes").stdout; !strings.Contains(out, "s2") {
		t.Fatalf("no reverse route: %s", out)
	}
	ts.w1.r().as("a1").run("message", "send", "a2", "--subject", "reply", "--body", "-").ok(t, "a to b")
	if out := ts.w2.r().as("a2").run("message", "list").ok(t, "list").stdout; !strings.Contains(out, "reply") {
		t.Fatalf("the reverse message did not arrive: %s", out)
	}
}
