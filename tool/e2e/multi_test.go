package e2e

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type twoSites struct {
	t      *testing.T
	w1, w2 *workspace
	origin string
	ledger string
}

// setupTwoSites creates a workspace, an origin for each shared history, invites a
// second site, and joins it. The second site is a separate workspace on this machine.
func setupTwoSites(t *testing.T) *twoSites {
	t.Helper()
	dir := t.TempDir()
	w1 := newWorkspace(t)
	origin := filepath.Join(dir, "origin.git")
	ledger := filepath.Join(dir, "ledger.git")
	runGit(t, w1.root, "init", "--bare", origin)
	runGit(t, w1.root, "remote", "add", "origin", origin)
	runGit(t, w1.root, "push", "-q", "-u", "origin", "main")
	runGit(t, w1.root, "init", "--bare", ledger)
	runGit(t, w1.path("handoff"), "remote", "add", "origin", ledger)
	runGit(t, w1.path("handoff"), "push", "-q", "-u", "origin", "main")

	w1.r().run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	w1.r().run("sync").ok(t, "the first sync")
	token := strings.TrimSpace(w1.r().run("site", "invite", "s2",
		"--at", "127.0.0.1:0",
		"--line", origin, "--ledger", ledger,
		"--store", w1.path("repos", "store.git"),
		"--code", "code="+w1.path("repos", "code.git")).ok(t, "invite").stdout)
	w1.r().run("sync").ok(t, "sync the invitation")

	root2 := filepath.Join(dir, "w2")
	r2 := &runner{t: t, root: root2}
	r2.run("join", token, "--dir", root2).ok(t, "join")
	w2 := &workspace{t: t, root: root2}
	w2.r().run("verify").ok(t, "verify the joined site")
	w1.r().run("sync").ok(t, "sync after the join")
	w2.r().run("sync").ok(t, "sync the joined site")
	return &twoSites{t: t, w1: w1, w2: w2, origin: origin, ledger: ledger}
}

func TestTwoSitesJoinSync(t *testing.T) {
	ts := setupTwoSites(t)
	w1, w2 := ts.w1, ts.w2

	if !strings.Contains(w2.file("doc/site/s2.yaml"), "ssh-ed25519") {
		t.Fatalf("the joined site did not register a key: %s", w2.file("doc/site/s2.yaml"))
	}
	show := w1.r().run("site", "show", "s2").ok(t, "w1 shows s2")
	if !strings.Contains(show.stdout, "ssh-ed25519") {
		t.Fatalf("w1 did not learn s2's key: %s", show.stdout)
	}
	if !strings.Contains(w2.file("var/site"), "s2") {
		t.Fatal("the joined site does not know its own name")
	}

	w1.r().run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open on w1")
	w1.r().run("sync").ok(t, "sync the job")
	w2.r().run("sync").ok(t, "sync the joined site")
	if board := w2.r().run("job", "list").ok(t, "board at the joined site"); !strings.Contains(board.stdout, "j1") {
		t.Fatalf("the joined site does not see the job: %s", board.stdout)
	}
	w2.r().run("verify").ok(t, "verify the joined site with a job")
}

func TestCrossSiteMessage(t *testing.T) {
	ts := setupTwoSites(t)
	w1, w2 := ts.w1, ts.w2
	p1 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	p2 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	w1.r().run("site", "reach", "s2", "--at", p2).ok(t, "w1 reaches s2")
	w2.r().run("site", "reach", "s1", "--at", p1).ok(t, "w2 reaches s1")

	startDaemon(t, w1, p1)
	startDaemon(t, w2, p2)
	w1.r().as("a1").run("actor", "hello").ok(t, "a1 hello")
	w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "declare a2")
	w2.r().run("sync").ok(t, "sync a2")
	w1.r().run("sync").ok(t, "w1 sync")

	id := strings.TrimSpace(w2.r().as("a2").withStdin("it crossed sites\n", "message", "send", "a1",
		"--subject", "cross-site hello", "--body", "-").ok(t, "send").stdout)
	if id == "" {
		t.Fatal("send printed no id")
	}
	mail := w1.r().as("a1").run("message", "list").ok(t, "w1 mail")
	if !strings.Contains(mail.stdout, "cross-site hello") || !strings.Contains(mail.stdout, "a2") {
		t.Fatalf("the message did not arrive: %s", mail.stdout)
	}
	read := w1.r().as("a1").run("message", "read", id).ok(t, "read")
	if !strings.Contains(read.stdout, "it crossed sites") {
		t.Fatalf("the body did not cross: %s", read.stdout)
	}
	if doctor := w2.r().run("site", "doctor").ok(t, "doctor"); !strings.Contains(doctor.stdout, "answered") {
		t.Fatalf("doctor did not reach s1: %s", doctor.stdout)
	}

	replyID := strings.TrimSpace(ts.w1.r().as("a1").withStdin("replied\n", "message", "reply", id,
		"--subject", "re:", "--body", "-").ok(t, "reply").stdout)
	back := jsonLines[struct {
		ID      string `json:"id"`
		ReplyTo string `json:"reply_to"`
	}](t, ts.w2.r().as("a2").run("message", "list", "--json").ok(t, "a2 list").stdout)
	found := false
	for _, m := range back {
		if m.ID == replyID && m.ReplyTo == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("the reply did not carry reply_to: %s", strings.TrimSpace(fmt.Sprint(back)))
	}
}

func TestSettlement(t *testing.T) {
	ts := setupTwoSites(t)
	w1, w2 := ts.w1, ts.w2

	w1.r().run("repo", "add", "shared", "--about", "from w1").ok(t, "add on w1")
	w1.r().run("sync").ok(t, "publish w1's claim")
	time.Sleep(1100 * time.Millisecond)
	w2.r().run("repo", "add", "shared", "--about", "from w2").ok(t, "add on w2")
	settled := w2.r().run("sync").ok(t, "settle")
	if !strings.Contains(settled.stdout, "settled") {
		t.Fatalf("the sync did not settle the claim: %s", settled.stdout)
	}
	w1.r().run("sync").ok(t, "w1 sync")

	a, b := w1.file("doc/repo/shared.yaml"), w2.file("doc/repo/shared.yaml")
	if a != b {
		t.Fatalf("the sites disagree:\n--- w1 ---\n%s\n--- w2 ---\n%s", a, b)
	}
	if !strings.Contains(a, "from w1") {
		t.Fatalf("the earlier claim did not keep the name:\n%s", a)
	}
	w1.r().run("verify").ok(t, "verify w1")
	w2.r().run("verify").ok(t, "verify w2")
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestSendWaitsForTheSite(t *testing.T) {
	ts := setupTwoSites(t)
	p1 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ts.w2.r().run("site", "reach", "s1", "--at", p1).ok(t, "reach")
	ts.w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	ts.w2.r().run("sync").ok(t, "sync a2")
	ts.w1.r().run("sync").ok(t, "w1 sync")

	done := make(chan result, 1)
	go func() {
		done <- ts.w2.r().as("a2").run("message", "send", "a1",
			"--subject", "waited", "--wait", "5s")
	}()
	time.Sleep(700 * time.Millisecond)
	startDaemon(t, ts.w1, p1)
	res := <-done
	res.ok(t, "the send waited for the site")
	if mail := ts.w1.r().as("a1").run("message", "list").ok(t, "mail"); !strings.Contains(mail.stdout, "waited") {
		t.Fatalf("the message did not arrive: %s", mail.stdout)
	}
}
