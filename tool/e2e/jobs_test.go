package e2e

import (
	"os"
	"path/filepath"
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
