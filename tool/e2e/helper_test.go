package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
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

// TestMigrateReachToEndpoint: an old line with `reach` is moved to `endpoint` in one
// commit, and the old name still reads before the migration.
func TestMigrateReachToEndpoint(t *testing.T) {
	w := newWorkspace(t)
	// Make the record look like one written by the previous version.
	w.write("doc/site/s1.yaml", "site: s1\nkey: "+siteKeyLine(t, w)+"reach: 127.0.0.1:7700\n")
	w.git("add", "-A")
	w.git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "old record")

	if out := w.r().run("migrate", "--dry-run").ok(t, "dry run").stdout; !strings.Contains(out, "reach -> endpoint") {
		t.Fatalf("the dry run did not report the change: %s", out)
	}
	if !strings.Contains(w.file("doc/site/s1.yaml"), "reach:") {
		t.Fatalf("the dry run wrote the file")
	}
	w.r().run("migrate").ok(t, "migrate")
	got := w.file("doc/site/s1.yaml")
	if strings.Contains(got, "reach:") || !strings.Contains(got, "endpoint: 127.0.0.1:7700") {
		t.Fatalf("the record was not migrated: %s", got)
	}
	if subject := w.git("log", "-1", "--format=%s"); !strings.Contains(subject, "reach") {
		t.Fatalf("the migration was not committed: %s", subject)
	}
	w.r().run("verify").ok(t, "verify after migration")
	w.r().run("migrate").ok(t, "second migrate")
	if out := w.r().run("migrate").ok(t, "third migrate").stdout; !strings.Contains(out, "nothing to migrate") {
		t.Fatalf("migrate is not idempotent: %s", out)
	}
}

// TestHelperDaemonDoesNotIdleOut: a site that declared itself a helper stays up.
func TestHelperDaemonDoesNotIdleOut(t *testing.T) {
	t.Setenv("THRUM_ROUTE_INTERVAL", "100ms")
	w := newWorkspace(t)
	w.r().run("site", "run", "on").ok(t, "run on")
	w.r().run("sync").ok(t, "sync")
	l := layout.New(w.root)
	time.Sleep(1200 * time.Millisecond) // twelve intervals: past the idle threshold
	if _, err := daemon.Status(l); err != nil {
		t.Fatalf("a helper daemon idled out: %v", err)
	}
}

// TestDaemonStartsWithoutDeclaredReach: no address is declared, so the daemon
// chooses its own default listen point.
func TestDaemonStartsWithoutDeclaredReach(t *testing.T) {
	w := newWorkspace(t)
	w.r().run("sync").ok(t, "sync")
	if _, err := daemon.Status(layout.New(w.root)); err != nil {
		t.Fatalf("the daemon did not start without a declared endpoint: %v", err)
	}
}

// siteKeyLine returns the public key line from the existing site record.
func siteKeyLine(t *testing.T, w *workspace) string {
	t.Helper()
	for _, ln := range strings.Split(w.file("doc/site/s1.yaml"), "\n") {
		if strings.HasPrefix(ln, "key:") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "key:")) + "\n"
		}
	}
	t.Fatal("no key in the site record")
	return ""
}

// TestRunRefusesAClonedSnapshot: data/src is a source snapshot, not a repository.
func TestRunRefusesAClonedSnapshot(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	ts.w1.r().run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	ts.w2.r().run("site", "code", "on").ok(t, "code on")
	ts.w2.r().run("site", "run", "on").ok(t, "run on")
	ts.w2.r().run("sync").ok(t, "helper sync")
	ts.w1.r().run("sync", "--code", "--job", "j1", "--site", "s2").ok(t, "sync code")
	if err := os.MkdirAll(filepath.Join(ts.w2.path("job", "j1", "data", "src", ".git")), 0o755); err != nil {
		t.Fatal(err)
	}
	ts.w1.r().run("run", "--site", "s2", "--job", "j1", "--", "echo", "hello").
		fails(t, "a cloned snapshot must be refused", "not a repository")
}

// TestCodeSendAndCopy: code send is the snapshot; copy moves ordinary files.
func TestCodeSendAndCopy(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	ts.w1.r().run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	ts.w2.r().run("site", "code", "on").ok(t, "code on")
	ts.w2.r().run("site", "run", "on").ok(t, "run on")
	ts.w2.r().run("sync").ok(t, "helper sync")
	ts.w1.r().run("code", "send", "j1", "--site", "s2").ok(t, "code send")
	if !ts.w2.exists(filepath.Join("job", "j1", "data", "src")) {
		t.Fatalf("code send did not materialise data/src")
	}

	// An ordinary file, to the helper's payload data/ and back.
	local := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(local, []byte("input\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts.w1.r().run("copy", "--to", "s2", "--job", "j1", local).ok(t, "copy to")
	if got, err := os.ReadFile(ts.w2.path("job", "j1", "data", "input.txt")); err != nil || string(got) != "input\n" {
		t.Fatalf("copy --to did not land: %q %v", got, err)
	}
	back := filepath.Join(t.TempDir(), "back.txt")
	ts.w1.r().run("copy", "--from", "s2", "--job", "j1", "input.txt", back).ok(t, "copy from")
	if got, err := os.ReadFile(back); err != nil || string(got) != "input\n" {
		t.Fatalf("copy --from did not return: %q %v", got, err)
	}
}
