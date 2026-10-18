package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestJobLifecycle(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("work", "j1", "--repo", "code", "--label", "up", "--goal", "ship it").ok(t, "work")
	wt := w.path("worktree", "up")
	w.mustExist("doc/task/j1/brief.md")
	w.mustExist("worktree/up")

	if err := os.WriteFile(filepath.Join(wt, "README"), []byte("changed by the job\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wt, "add", "-A")
	runGit(t, wt, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "work")

	w.write("job/j1/log/note.txt", "evidence")
	r.run("job", "handoff", "j1", "--status", "partial", "--next", "finish",
		"--artifact", "job/j1/log/note.txt").ok(t, "handoff")

	board := r.run("job", "list").ok(t, "board")
	if !strings.Contains(board.stdout, "j1") || !strings.Contains(board.stdout, "partial") {
		t.Fatalf("board does not show the handoff: %s", board.stdout)
	}
	show := r.run("job", "show", "j1").ok(t, "show")
	if !strings.Contains(show.stdout, "partial") {
		t.Fatalf("show does not report the handoff: %s", show.stdout)
	}

	r.run("job", "close", "j1", "--status", "done").ok(t, "close")
	w.mustExist("doc/archive/task/j1/brief.md")
	w.mustNotExist("doc/task/j1/brief.md")
	if head := strings.TrimSpace(runGit(t, wt, "rev-parse", "--abbrev-ref", "HEAD")); head != "HEAD" {
		t.Fatalf("the worktree is on %q, want a detached head", head)
	}
	if !w.exists("handoff/j1") {
		t.Fatal("the handoff is not in the ledger")
	}
	r.run("verify").ok(t, "verify after closing")

	if out := w.git("status", "--short"); strings.TrimSpace(out) != "" {
		t.Fatalf("the line is not clean after close:\n%s", out)
	}
}

func TestJobRefusals(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	r.run("job", "open", "j1", "--repo", "code", "--label", "two", "--goal", "g").
		fails(t, "reuse a job id", "has been used before")
	r.run("job", "open", "j2", "--repo", "nope", "--label", "two", "--goal", "g").
		fails(t, "unknown repository", "no repository named")

	w.write("worktree/up/dirty.txt", "dirty")
	r.run("job", "close", "j1", "--status", "done").fails(t, "close a dirty worktree", "has changes")
	if err := os.Remove(w.path("worktree", "up", "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	r.run("job", "close", "j1", "--status", "weird").fails(t, "bad closing status", "not a closing status")
	r.run("job", "close", "j1", "--status", "abandoned").ok(t, "close")

	r.run("job", "handoff", "j1", "--status", "partial", "--next", "x").
		fails(t, "handoff a closed job", "no job named")

	r.run("job", "open", "j3", "--repo", "code", "--label", "three", "--goal", "g").ok(t, "open j3")
	r.as("ghost").run("job", "handoff", "j3", "--status", "partial", "--next", "x").
		fails(t, "handoff by an undeclared actor", "not declared")
}

func TestClaimReassignRelinquish(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("actor", "declare", "a2", "--kind", "human").ok(t, "declare a2")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g", "--owner", "a1").ok(t, "open")

	r.as("a2").run("job", "claim", "j1").fails(t, "claim an owned job", "is owned by")
	r.as("a2").run("job", "reassign", "j1", "--to", "a2").ok(t, "reassign")
	if show := r.run("job", "show", "j1").ok(t, "show"); !strings.Contains(show.stdout, "a2") {
		t.Fatalf("reassign did not take: %s", show.stdout)
	}
	r.as("a2").run("job", "relinquish", "j1").ok(t, "relinquish")
	r.as("a2").run("job", "claim", "j1").ok(t, "claim")
	r.as("a2").run("job", "claim", "j1").fails(t, "claim twice", "is owned by")
}

func TestWorktrees(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("worktree", "add", "tmp", "--repo", "code", "--ephemeral").ok(t, "add")
	w.mustExist("worktree/tmp")
	if list := r.run("worktree", "list").ok(t, "list"); !strings.Contains(list.stdout, "tmp") {
		t.Fatalf("list does not show the label: %s", list.stdout)
	}
	w.write("worktree/tmp/dirty.txt", "dirty")
	r.run("worktree", "release", "tmp").fails(t, "release a dirty worktree", "has changes")
	if err := os.Remove(w.path("worktree", "tmp", "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	r.run("worktree", "release", "tmp").ok(t, "release")
	w.mustNotExist("worktree/tmp")
	r.run("worktree", "remove", "tmp").ok(t, "remove")
	w.mustNotExist("doc/worktree/tmp.yaml")
	w.mustExist("doc/archive/worktree/tmp.yaml")
	r.run("verify").ok(t, "verify after removing the label")
}

func TestWhereEnv(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")

	if got := strings.TrimSpace(r.run("where", "--job", "j1").ok(t, "where").stdout); got != w.path("worktree", "up") {
		t.Fatalf("where = %q, want %q", got, w.path("worktree", "up"))
	}
	if got := strings.TrimSpace(r.run("where").ok(t, "where root").stdout); got != w.root {
		t.Fatalf("where = %q, want %q", got, w.root)
	}
	env := r.run("env", "--job", "j1").ok(t, "env")
	for _, want := range []string{"THRUM_JOB=j1", "THRUM_ROOT=" + w.root, "THRUM_ACTOR=a1"} {
		if !strings.Contains(env.stdout, want) {
			t.Fatalf("env does not contain %q:\n%s", want, env.stdout)
		}
	}
}

// TestBriefBody: the brief's body can come from stdin or a file, for job open and
// for work's open path.
func TestBriefBody(t *testing.T) {
	w := newWorkspace(t)
	w.r().run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	w.r().withStdin("## Goal\n\nfrom stdin\n\n## Scope\n\nnone\n\n## Acceptance criteria\n\ngreen\n",
		"job", "open", "body-1", "--repo", "code", "--label", "body",
		"--body", "-").ok(t, "open with stdin")
	if out := w.file("doc/task/body-1/brief.md"); !strings.Contains(out, "from stdin") {
		t.Fatalf("the brief body did not come from stdin:\n%s", out)
	}
	if out := w.r().run("job", "show", "body-1").ok(t, "show").stdout; !strings.Contains(out, "from stdin") {
		t.Fatalf("job show does not print the body: %s", out)
	}

	brief := w.path("brief.md")
	if err := os.WriteFile(brief, []byte("## Goal\n\nfrom a file\n\n## Scope\n\nnone\n\n## Acceptance criteria\n\ngreen\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.r().run("work", "body-2", "--repo", "code", "--label", "body2",
		"--body", brief).ok(t, "work with a body file")
	if out := w.file("doc/task/body-2/brief.md"); !strings.Contains(out, "from a file") {
		t.Fatalf("work did not use the brief body file:\n%s", out)
	}
	w.r().run("verify").ok(t, "verify")
}

// TestWorkUnderAnOwner: the design says a job with an owner is worked under that
// owner, not claimed. The owner does not change.
func TestWorkUnderAnOwner(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g", "--owner", "a1").ok(t, "open")
	r.as("a2").run("work", "j1", "--repo", "code", "--label", "up2").ok(t, "work under the owner")
	if show := r.run("job", "show", "j1").ok(t, "show"); !strings.Contains(show.stdout, "a1") {
		t.Fatalf("working under the owner changed the owner: %s", show.stdout)
	}
}

// TestHandoffSameSecond: two handoffs in one second still write two files. The
// name carries the start, and a handoff is never rewritten.
func TestHandoffSameSecond(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	r.run("job", "handoff", "j1", "--status", "partial", "--next", "one").ok(t, "first")
	r.run("job", "handoff", "j1", "--status", "partial", "--next", "two").ok(t, "second")
	paths, err := filepath.Glob(w.path("handoff", "j1", "*.md"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("handoff files: %v %v", paths, err)
	}
	r.run("verify").ok(t, "verify")
}

// TestHandoffBase: the first handoff starts at the branch point, not at its own
// head; base equals head only when nothing was committed.
func TestHandoffBase(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	dir := strings.TrimSpace(strings.Split(r.run("work", "j1", "--repo", "code", "--label", "up").ok(t, "work").stdout, "\n")[0])
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "work")
	r.run("job", "handoff", "j1", "--status", "partial", "--next", "x").ok(t, "handoff")
	paths, err := filepath.Glob(w.path("handoff", "j1", "*.md"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("handoff files: %v %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	base := regexp.MustCompile(`(?m)^base: (.+)$`).FindStringSubmatch(string(data))
	head := regexp.MustCompile(`(?m)^head: (.+)$`).FindStringSubmatch(string(data))
	if base == nil || head == nil {
		t.Fatalf("handoff has no base/head:\n%s", data)
	}
	if base[1] != head[1] {
		return
	}
	// base == head is only right when nothing was committed; the open commits the
	// branch from the default, so here they must differ.
	t.Fatalf("first handoff base equals head: %s", base[1])
}
