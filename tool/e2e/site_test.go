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

func TestCreateRecordsTheSitesAddress(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	src := filepath.Join(dir, "src")
	initRepo(t, src)
	(&runner{t: t, root: root}).run("create", root, "--site", "s1",
		"--at", "127.0.0.1:7700", "--code", "code="+src).ok(t, "create")
	w := &workspace{t: t, root: root, src: src}
	if !strings.Contains(w.file("doc/site/s1.yaml"), "127.0.0.1:7700") {
		t.Fatalf("the roster does not carry the address: %s", w.file("doc/site/s1.yaml"))
	}
	show := w.r().run("site", "show", "s1").ok(t, "show")
	if !strings.Contains(show.stdout, "127.0.0.1:7700") {
		t.Fatalf("site show does not report the address: %s", show.stdout)
	}
}

// TestSiteAnnounce: the declared address can change, as one line event, and a
// peer's own local view still wins over it.
func TestSiteAnnounce(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	w1, w2 := ts.w1, ts.w2

	w1.r().run("site", "announce", "--at", "127.0.0.1:9911").ok(t, "announce")
	if show := w1.r().run("site", "show", "s1").ok(t, "show").stdout; !strings.Contains(show, "127.0.0.1:9911") {
		t.Fatalf("announce did not take: %s", show)
	}
	if log := w1.git("log", "-1", "--format=%s"); !strings.Contains(log, "site s1: address 127.0.0.1:9911") {
		t.Fatalf("the announcement is not a line event: %s", log)
	}

	w1.r().run("sync").ok(t, "w1 sync")
	w2.r().run("sync").ok(t, "w2 sync")
	if show := w2.r().run("site", "show", "s1").ok(t, "show").stdout; !strings.Contains(show, "127.0.0.1:9911") {
		t.Fatalf("the peer did not learn the declared address: %s", show)
	}
	// The peer's own view is the real address, so its sync still reaches the site.
	if out := w2.r().run("sync").ok(t, "w2 sync still works").stdout; !strings.Contains(out, "exchanged") {
		t.Fatalf("the local view did not keep the peer reachable: %s", out)
	}

	w1.r().run("site", "announce", "--clear").ok(t, "clear")
	w1.r().run("sync").ok(t, "w1 sync clear")
	w2.r().run("sync").ok(t, "w2 sync clear")
	if show := w2.r().run("site", "show", "s1").ok(t, "show").stdout; !strings.Contains(show, "reach  none") {
		t.Fatalf("the declaration was not cleared: %s", show)
	}

	w1.r().run("site", "announce").fails(t, "announce nothing", "give")
	w1.r().run("site", "announce", "--at", "nonsense").fails(t, "bad address", "host:port")
	w1.r().run("site", "announce", "--at", "127.0.0.1:9911", "--clear").fails(t, "both", "not both")
}

// TestSiteLeaveAndStay: a site can leave without destroying its copy, the peers
// see the cleared declaration, and it can return.
func TestSiteLeaveAndStay(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	w1, w2 := ts.w1, ts.w2

	w2.r().run("site", "leave").ok(t, "leave")
	if log := w2.git("log", "-1", "--format=%s"); !strings.Contains(log, "site s2: left") {
		t.Fatalf("the departure is not a line event: %s", log)
	}
	show := w2.r().run("site", "show", "s2").ok(t, "show").stdout
	if !strings.Contains(show, "state  left") || !strings.Contains(show, "reach  none") {
		t.Fatalf("the site is not marked left with no address: %s", show)
	}
	w2.r().run("site", "leave").fails(t, "left twice", "already left")

	// The endpoint is gone and the commands that need it refuse.
	w2.r().run("sync").fails(t, "a left site", "has left")
	w2.r().as("a2").run("message", "send", "a1", "--subject", "x").fails(t, "a left message", "has left")

	// The w1 merges the cleared declaration; its own local view still wins, so it
	// dials the stopped endpoint and reports it.
	if out := w1.r().run("sync").ok(t, "w1 sync").stdout; !strings.Contains(out, "unreachable: s2") {
		t.Fatalf("the w1's local view did not keep its old address: %s", out)
	}
	if show := w1.r().run("site", "show", "s2").ok(t, "show").stdout; !strings.Contains(show, "reach  none") {
		t.Fatalf("the w1 did not see the cleared declaration: %s", show)
	}

	w2.r().run("site", "stay", "--at", ts.p2).ok(t, "stay")
	if log := w2.git("log", "-1", "--format=%s"); !strings.Contains(log, "site s2: serving at "+ts.p2) {
		t.Fatalf("the return is not a line event: %s", log)
	}
	if show := w2.r().run("site", "show", "s2").ok(t, "show").stdout; !strings.Contains(show, "state  serving") {
		t.Fatalf("the site is not serving again: %s", show)
	}
	w2.r().run("site", "stay", "--at", ts.p2).fails(t, "staying while serving", "has not left")
	w2.r().run("sync").ok(t, "w2 sync back")
	w1.r().run("sync").ok(t, "w1 sync back")
	w2.r().as("a2").run("message", "send", "a1", "--subject", "back").ok(t, "message back")
	if out := w1.r().as("a1").run("message", "list").ok(t, "a1 list").stdout; !strings.Contains(out, "back") {
		t.Fatalf("the message after the return did not arrive: %s", out)
	}

	// A site holding a job's worktree does not leave.
	w2.r().run("job", "open", "j2", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "job")
	w2.r().run("site", "leave").fails(t, "a busy site", "holds a job")
}

// TestSiteDiscoverRestartsTheDaemon: discover on/off changes how the daemon starts,
// so the command must bring it back rather than wait for an unrelated next command.
// The join that needs the LAN may run on another machine.
func TestSiteDiscoverRestartsTheDaemon(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("site", "discover", "on").ok(t, "discover on")
	r.run("daemon", "status").ok(t, "the daemon answers after discover on")
	r.run("site", "discover", "off").ok(t, "discover off")
	r.run("daemon", "status").ok(t, "the daemon answers after discover off")
	r.run("daemon", "stop").ok(t, "stop")
}
