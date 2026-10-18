package e2e

import (
	"os"
	"strings"
	"testing"
)

func writePayload(t *testing.T, w *workspace, job string) {
	t.Helper()
	for _, sub := range []string{"tmp", "cache", "ckpt", "log", "out"} {
		if err := os.MkdirAll(w.path("job", job, sub), 0o755); err != nil {
			t.Fatalf("payload %s: %v", sub, err)
		}
		if err := os.WriteFile(w.path("job", job, sub, "x"), []byte("bytes"), 0o644); err != nil {
			t.Fatalf("payload %s: %v", sub, err)
		}
	}
}

// TestPayloadCollection: the close removes tmp/cache/ckpt; the window collects the
// rest; keep protects everything; status reports which payloads are collectible.
func TestPayloadCollection(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "actor")
	if err := os.MkdirAll(w.path("doc", "convention"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := "# Policy\n\n```values\ncollect.window: 0s\n```\n"
	if err := os.WriteFile(w.path("doc", "convention", "01-policy.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	r.run("commit", "--summary", "collect.window", "doc/convention/01-policy.md").ok(t, "commit policy")
	r.run("verify").ok(t, "verify the convention")

	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	writePayload(t, w, "j1")
	r.run("job", "handoff", "j1", "--status", "done", "--next", "none").ok(t, "handoff")
	r.run("job", "close", "j1", "--status", "done").ok(t, "close")

	for _, sub := range []string{"tmp", "cache", "ckpt"} {
		if _, err := os.Stat(w.path("job", "j1", sub)); !os.IsNotExist(err) {
			t.Fatalf("%s should be collected at the close", sub)
		}
	}
	// log and out are past a 0s window, so they are collectible, not removed by close.
	if _, err := os.Stat(w.path("job", "j1", "log", "x")); err != nil {
		t.Fatalf("log should survive the close: %v", err)
	}
	status := r.run("status").ok(t, "status").stdout
	if !strings.Contains(status, "j1") || !strings.Contains(status, "collectible") {
		t.Fatalf("status does not report the collectible payload:\n%s", status)
	}
	out := r.run("clean", "--payloads").ok(t, "clean").stdout
	if !strings.Contains(out, "removed job/j1") {
		t.Fatalf("the payload was not collected:\n%s", out)
	}
	if _, err := os.Stat(w.path("job", "j1")); !os.IsNotExist(err) {
		t.Fatalf("the payload directory should be gone")
	}
}

func TestPayloadKeep(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "actor")
	r.run("job", "open", "j2", "--repo", "code", "--label", "keep", "--goal", "g", "--keep").ok(t, "open")
	writePayload(t, w, "j2")
	r.run("job", "handoff", "j2", "--status", "done", "--next", "none").ok(t, "handoff")
	r.run("job", "close", "j2", "--status", "done").ok(t, "close")
	for _, sub := range []string{"tmp", "cache", "ckpt"} {
		if _, err := os.Stat(w.path("job", "j2", sub)); err != nil {
			t.Fatalf("keep should protect %s: %v", sub, err)
		}
	}
	if status := r.run("status").ok(t, "status").stdout; !strings.Contains(status, "kept") {
		t.Fatalf("status does not report the kept payload:\n%s", status)
	}
	r.run("clean", "--payloads").ok(t, "clean")
	if _, err := os.Stat(w.path("job", "j2")); err != nil {
		t.Fatalf("a kept payload was collected: %v", err)
	}
}

func TestPayloadDefaultWindow(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "actor")
	r.run("job", "open", "j3", "--repo", "code", "--label", "wait", "--goal", "g").ok(t, "open")
	writePayload(t, w, "j3")
	r.run("job", "handoff", "j3", "--status", "done", "--next", "none").ok(t, "handoff")
	r.run("job", "close", "j3", "--status", "done").ok(t, "close")
	if status := r.run("status").ok(t, "status").stdout; !strings.Contains(status, "waiting") {
		t.Fatalf("the default 14d window should keep it waiting:\n%s", status)
	}
	r.run("clean", "--payloads").ok(t, "clean")
	if _, err := os.Stat(w.path("job", "j3")); err != nil {
		t.Fatalf("a waiting payload was collected: %v", err)
	}
}

func TestInvalidCollectWindow(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	if err := os.MkdirAll(w.path("doc", "convention"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := "# Policy\n\n```values\ncollect.window: banana\n```\n"
	if err := os.WriteFile(w.path("doc", "convention", "01-policy.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	r.run("commit", "--summary", "bad window", "doc/convention/01-policy.md").ok(t, "commit")
	r.run("status").fails(t, "status with a bad window", "collect.window")
	r.run("clean", "--payloads").fails(t, "clean with a bad window", "collect.window")
	r.run("verify").fails(t, "verify with a bad window", "collect.window")
}
