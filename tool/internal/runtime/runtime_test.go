package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/leases"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/payload"
	"github.com/multiagentworkspace/thrum/tool/internal/resources"
	"github.com/multiagentworkspace/thrum/tool/internal/validate"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// openJob writes a brief straight into doc/task/, which is what a live job looks like
// to everything under var/.
func openJob(t *testing.T, l workspace.Layout, job string) {
	t.Helper()
	if err := os.MkdirAll(l.TaskDir(job), 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: " + job + "\nstatus: active\nbranch: job/" + job + "\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(l.Brief(job), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
}

func workspaceFor(t *testing.T) workspace.Layout {
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

func TestDeclareAndRemoveKeepTheProjectsOwnFields(t *testing.T) {
	l := workspaceFor(t)

	if err := resources.Declare(l, "worktree", resources.Entry{
		Name:    "uploader",
		Purpose: "Uploader reliability work",
		Extra:   map[string]string{"owned-by": "the platform team", "capacity-note": "just the one"},
	}); err != nil {
		t.Fatal(err)
	}
	// A person adds a field of their own, which the tooling does not read.
	path := l.ResourceKind("worktree")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	handEdited := string(raw) + "  - name: handwritten\n    purpose: added by hand\n    contact: somebody@example.com\n"
	if err := os.WriteFile(path, []byte(handEdited), 0o644); err != nil {
		t.Fatal(err)
	}

	// Declaring another entry must not throw the hand-written one, or its fields,
	// away.
	if err := resources.Declare(l, "worktree", resources.Entry{
		Name:    "spike",
		Purpose: "A one-off look at something",
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(after)
	for _, want := range []string{"uploader", "the platform team", "handwritten", "somebody@example.com", "spike"} {
		if !strings.Contains(text, want) {
			t.Errorf("declaring dropped %q:\n%s", want, text)
		}
	}

	entries, err := workspace.ReadResources(l, format.WorktreeKind)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("want three entries, got %d", len(entries))
	}

	// Removing one leaves the rest.
	if err := resources.Remove(l, "worktree", "spike"); err != nil {
		t.Fatal(err)
	}
	entries, _ = workspace.ReadResources(l, format.WorktreeKind)
	if _, still := entries["spike"]; still {
		t.Error("the removed entry is still declared")
	}
	if len(entries) != 2 {
		t.Errorf("want two entries, got %d", len(entries))
	}

	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestRemoveRefusesWhileADirectoryIsThere(t *testing.T) {
	l := workspaceFor(t)
	if err := resources.Declare(l, "worktree", resources.Entry{Name: "uploader", Purpose: "work"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l.Worktree("uploader"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := resources.Remove(l, "worktree", "uploader")
	if err == nil {
		t.Fatal("removing a materialised worktree should be refused")
	}
	if !strings.Contains(err.Error(), "materialised") {
		t.Errorf("the refusal should say why: %v", err)
	}
}

func TestTheLastRemovalTakesTheFileWithIt(t *testing.T) {
	l := workspaceFor(t)
	if err := resources.Declare(l, "worktree", resources.Entry{Name: "only", Purpose: "work"}); err != nil {
		t.Fatal(err)
	}
	if err := resources.Remove(l, "worktree", "only"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.ResourceKind("worktree")); err == nil {
		t.Error("a kind with nothing declared should have no file")
	}
	// A file left holding an empty mapping would fail validation for having no kind key.
	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestOccupyRefusesToOverwriteSomebodyElse(t *testing.T) {
	l := workspaceFor(t)
	openJob(t, l, "job-one")
	now := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)

	if err := occupancy.Occupy(l, "job-one", "claude-opus", false, now); err != nil {
		t.Fatal(err)
	}
	// Somebody else may not simply overwrite it.
	if err := occupancy.Occupy(l, "job-one", "claude-sonnet", false, now); err == nil {
		t.Fatal("overwriting another actor's claim should be refused")
	}
	// Taking it over is a decision, so it has to be asked for.
	if err := occupancy.Occupy(l, "job-one", "claude-sonnet", true, now); err != nil {
		t.Fatalf("an explicit take-over should be allowed: %v", err)
	}
	// The same actor carrying on keeps the original claim's start.
	if err := occupancy.Occupy(l, "job-one", "claude-sonnet", false, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	o, ok, err := occupancy.Get(l, "job-one")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if o.Since != now.Format(format.RFC3339UTC) {
		t.Errorf("since: %q", o.Since)
	}

	// A note for a job that has left doc/task/ is stale.
	if err := os.RemoveAll(l.TaskDir("job-one")); err != nil {
		t.Fatal(err)
	}
	swept, err := occupancy.Sweep(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(swept) != 1 || swept[0] != "job-one" {
		t.Errorf("swept: %v", swept)
	}
	if _, ok, _ := occupancy.Get(l, "job-one"); ok {
		t.Error("a stale note should be gone")
	}
}

func TestLeasesExcludeAndExpire(t *testing.T) {
	l := workspaceFor(t)
	if err := resources.Declare(l, "gpu", resources.Entry{Name: "gpu", Purpose: "the only GPU"}); err != nil {
		t.Fatal(err)
	}
	openJob(t, l, "job-one")
	openJob(t, l, "job-two")
	// A lease always belongs to a job the workspace knows.
	if _, err := leases.Take(l, leases.Request{
		Resource: "gpu", Occupant: "claude-opus", Job: "no-such-job", TTL: time.Hour,
	}, time.Now().UTC()); err == nil {
		t.Error("a lease for a job the workspace does not know should be refused")
	}
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)

	first, err := leases.Take(l, leases.Request{
		Resource: "gpu", Occupant: "claude-opus", Job: "job-one", TTL: time.Hour,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Unit != "1" {
		t.Errorf("an exclusive resource has only unit 1, got %q", first.Unit)
	}

	// A second holder is refused while the first is live.
	if _, err := leases.Take(l, leases.Request{
		Resource: "gpu", Occupant: "claude-sonnet", Job: "job-two", TTL: time.Hour,
	}, now); err == nil {
		t.Fatal("taking a held unit should be refused")
	}

	// Once its time has passed it is free, and taking it overwrites the record.
	taken, err := leases.Take(l, leases.Request{
		Resource: "gpu", Occupant: "claude-sonnet", Job: "job-two", TTL: time.Hour,
	}, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("an expired lease should be free: %v", err)
	}
	if taken.Occupant != "claude-sonnet" {
		t.Errorf("occupant: %q", taken.Occupant)
	}

	// Renewal belongs to the holder.
	if _, err := leases.Renew(l, leases.Request{
		Resource: "gpu", Unit: "1", Occupant: "claude-opus", Job: "job-one", TTL: time.Hour,
	}, now); err == nil {
		t.Error("renewing somebody else's lease should be refused")
	}

	if err := leases.Release(l, "gpu", "1", "claude-sonnet"); err != nil {
		t.Fatal(err)
	}
	held, err := leases.Held(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 0 {
		t.Errorf("want nothing held, got %v", held)
	}
}

func TestCapacityAllowsSeveralHolders(t *testing.T) {
	l := workspaceFor(t)
	if err := resources.Declare(l, "gpu", resources.Entry{Name: "gpu", Purpose: "two GPUs", Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	openJob(t, l, "job-one")
	openJob(t, l, "job-two")
	openJob(t, l, "job-three")
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)

	a, err := leases.Take(l, leases.Request{Resource: "gpu", Occupant: "one", Job: "job-one", TTL: time.Hour}, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := leases.Take(l, leases.Request{Resource: "gpu", Occupant: "two", Job: "job-two", TTL: time.Hour}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Unit == b.Unit {
		t.Errorf("two holders were given the same unit: %q", a.Unit)
	}
	if _, err := leases.Take(l, leases.Request{Resource: "gpu", Occupant: "three", Job: "job-three", TTL: time.Hour}, now); err == nil {
		t.Error("a full resource should refuse a third holder")
	}
}

func TestALeaseDiesWithItsJob(t *testing.T) {
	l := workspaceFor(t)
	if err := resources.Declare(l, "gpu", resources.Entry{Name: "gpu", Purpose: "the only GPU"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)

	// A job that is live holds its lease.
	openJob(t, l, "job-one")
	openJob(t, l, "job-two")
	if _, err := leases.Take(l, leases.Request{Resource: "gpu", Occupant: "one", Job: "job-one", TTL: time.Hour}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := leases.Take(l, leases.Request{Resource: "gpu", Occupant: "two", Job: "job-two", TTL: time.Hour}, now); err == nil {
		t.Fatal("a live job's lease should hold")
	}

	// Once the brief has left doc/task/, the lease is as free as an expired one.
	if err := os.RemoveAll(l.TaskDir("job-one")); err != nil {
		t.Fatal(err)
	}
	if _, err := leases.Take(l, leases.Request{Resource: "gpu", Occupant: "two", Job: "job-two", TTL: time.Hour}, now); err != nil {
		t.Fatalf("a closed job's lease should be free: %v", err)
	}
}

func TestCollectTakesTheCheapHalfAtOnceAndTheRestAfterTheWindow(t *testing.T) {
	l := workspaceFor(t)

	// A closed job with a payload.
	if err := os.MkdirAll(l.ArchiveTask("old-job"), 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: old-job\nstatus: done\nbranch: job/old-job\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(l.ArchivedBrief("old-job"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"tmp", "cache", "ckpt", "log", "data", "state", "out"} {
		dir := l.JobSub("old-job", sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "x"), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The close has to be in the history, because the window is measured from it.
	commitArchive(t, l)

	// At once: the cheap half goes and the rest stays. The clock is the real one,
	// because the window is measured from a commit that was just made.
	now := time.Now().UTC()
	outcomes, err := payload.Collect(l, payload.Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes: %v", outcomes)
	}
	for _, sub := range payload.OnClose {
		if _, err := os.Stat(l.JobSub("old-job", sub)); err == nil {
			t.Errorf("%s should have gone at once", sub)
		}
	}
	for _, sub := range payload.AfterWindow {
		if _, err := os.Stat(l.JobSub("old-job", sub)); err != nil {
			t.Errorf("%s should still be there: %v", sub, err)
		}
	}

	// Before the window has passed, nothing more goes.
	if _, err := payload.Collect(l, payload.Options{Now: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.JobSub("old-job", "out")); err != nil {
		t.Error("the larger half should survive until the window has passed")
	}

	// A window of zero is not the same instruction as saying nothing: it means
	// everything goes as soon as the job is closed.
	window := time.Duration(0)
	if _, err := payload.Collect(l, payload.Options{Window: &window, Now: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, sub := range payload.AfterWindow {
		if _, err := os.Stat(l.JobSub("old-job", sub)); err == nil {
			t.Errorf("%s should have gone after the window", sub)
		}
	}
	// Idempotent: a second collector finds nothing to do and does not fail.
	if _, err := payload.Collect(l, payload.Options{Window: &window, Now: now.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("collection should be idempotent: %v", err)
	}
}

// commitArchive records the archived briefs, which is what closing a job does.
func commitArchive(t *testing.T, l workspace.Layout) {
	t.Helper()
	if _, err := gitx.Run(l.Root, "add", "-A", "--", "doc"); err != nil {
		t.Fatal(err)
	}
	args := append(gitx.IdentityArgs(l.Root, "tester"), "commit", "--quiet", "-m", "close old-job: done")
	if _, err := gitx.Run(l.Root, args...); err != nil {
		t.Fatal(err)
	}
}

func TestCollectSkipsAJobThatAskedToBeKept(t *testing.T) {
	l := workspaceFor(t)
	if err := os.MkdirAll(l.ArchiveTask("kept-job"), 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: kept-job\nstatus: done\nbranch: job/kept-job\nowned_paths: []\nkeep: true\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(l.ArchivedBrief("kept-job"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l.JobSub("kept-job", "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.JobSub("kept-job", "tmp")+"/x", []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	outcomes, err := payload.Collect(l, payload.Options{Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Skipped != "keep: true" {
		t.Errorf("outcomes: %+v", outcomes)
	}
	if _, err := os.Stat(l.JobSub("kept-job", "tmp")); err != nil {
		t.Error("a payload that asked to be kept should still be there")
	}
}

func TestTheGuardedCleanLeavesTheIgnoredPlanesAlone(t *testing.T) {
	l := workspaceFor(t)

	// Something a person dropped in the coordination repository, which is what a
	// clean is for.
	stray := filepath.Join(l.Root, "scratch.txt")
	if err := os.WriteFile(stray, []byte("left behind\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := workspace.Clean(l, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !strings.Contains(paths[0], "scratch.txt") {
		t.Fatalf("dry run should list the stray file, got %v", paths)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Error("a dry run must not remove anything")
	}

	if _, err := workspace.Clean(l, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); err == nil {
		t.Error("the stray file should be gone")
	}
	// The planes are not candidates at all: git clean runs without -x.
	for _, p := range []string{".bare", "store", "var/site", "job", "worktree", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(l.Root, p)); err != nil {
			t.Errorf("the clean took %s, which it must never do: %v", p, err)
		}
	}
}
