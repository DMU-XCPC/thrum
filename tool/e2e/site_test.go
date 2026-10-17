package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiteCommands(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	res := r.run("site", "list", "--json").ok(t, "site list")
	var sites []struct {
		Site string `json:"site"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &sites); err != nil {
		t.Fatalf("site list json: %v\n%s", err, res.stdout)
	}
	if len(sites) != 1 || sites[0].Site != "s1" {
		t.Fatalf("roster: %s", res.stdout)
	}

	r.run("site", "reach", "s1", "--at", "127.0.0.1:9999").ok(t, "reach")
	if out := w.git("status", "--short"); strings.TrimSpace(out) != "" {
		t.Fatalf("site reach dirtied the line:\n%s", out)
	}
	if show := r.run("site", "show", "s1").ok(t, "show"); !strings.Contains(show.stdout, "127.0.0.1:9999") {
		t.Fatalf("reach did not take: %s", show.stdout)
	}
	if doctor := r.run("site", "doctor").ok(t, "doctor"); !strings.Contains(doctor.stdout, "this site") {
		t.Fatalf("doctor: %s", doctor.stdout)
	}

	r.run("site", "invite", "s1").fails(t, "invite this site", "is this site")
	token := strings.TrimSpace(r.run("site", "invite", "s2", "--at", "127.0.0.1:9998").ok(t, "invite").stdout)
	if token == "" {
		t.Fatal("invite printed no token")
	}
	r.run("site", "invite", "s2").fails(t, "invite twice", "already declared")

	r.run("site", "key", "revoke", "s1").fails(t, "revoke this site", "refusing to revoke")
	r.run("site", "key", "revoke", "s2").ok(t, "revoke s2")
	before := r.run("site", "show", "s1").ok(t, "show").stdout
	r.run("site", "key", "rotate").ok(t, "rotate")
	after := r.run("site", "show", "s1").ok(t, "show").stdout
	if before == after {
		t.Fatal("rotate did not change the key")
	}

	r.run("site", "forget", "s1").fails(t, "forget this site", "refusing to forget")
	r.run("site", "forget", "s2").ok(t, "forget s2")
	r.run("site", "show", "s2").fails(t, "show a forgotten site", "no site")
	r.run("verify").ok(t, "verify after the roster changes")
}

func TestJoinRefusals(t *testing.T) {
	w := newWorkspace(t)
	w.r().run("site", "reach", "s1", "--at", "127.0.0.1:0").ok(t, "reach")
	token := strings.TrimSpace(w.r().run("site", "invite", "s2", "--at", "127.0.0.1:0").ok(t, "invite").stdout)

	nonempty := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonempty, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &runner{t: t, root: nonempty}
	r.run("join", token, "--dir", nonempty).fails(t, "join into a non-empty directory", "not empty")
	r.run("join", "not-a-token", "--dir", filepath.Join(t.TempDir(), "new")).
		fails(t, "join with a bad token", "not an invitation token")
}

func TestAssetDirectory(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")

	dir := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"a.txt": "a", "sub/b.txt": "b"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r.run("asset", "ingest", dir).ok(t, "ingest a directory")
	r.run("asset", "publish", "files", "2026-10-18", "--description", "a tree").ok(t, "publish")
	r.run("verify").ok(t, "verify a tree manifest")
	r.run("asset", "materialise", "files", "2026-10-18", "--job", "j1").ok(t, "materialise")
	if got := w.file("job/j1/data/a.txt"); got != "a" {
		t.Fatalf("a.txt = %q", got)
	}
	if got := w.file("job/j1/data/sub/b.txt"); got != "b" {
		t.Fatalf("sub/b.txt = %q", got)
	}
}

func TestBundleNeedsTheRecords(t *testing.T) {
	w1 := newWorkspace(t)
	r1 := w1.r()
	r1.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r1.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	r1.run("job", "handoff", "j1", "--status", "partial", "--next", "x").ok(t, "handoff")
	bundle := filepath.Join(t.TempDir(), "j1.tgz")
	r1.run("bundle", "export", "j1", "--out", bundle).ok(t, "export")

	w2 := newWorkspace(t)
	w2.r().run("bundle", "import", bundle).fails(t, "import without the records", "not here")
}
