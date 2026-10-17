package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/join"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

// inviteS2 invites s2 on w and returns the token, which now names no repository.
func inviteS2(t *testing.T, w *workspace, at string) join.Token {
	t.Helper()
	out := strings.TrimSpace(w.r().run("site", "invite", "s2", "--at", at).ok(t, "invite").stdout)
	tok, err := join.Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func encodeToken(t *testing.T, tok join.Token) string {
	t.Helper()
	out, err := join.Encode(tok)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestJoinRefusesAnotherWorkspace(t *testing.T) {
	w1 := newWorkspace(t)
	firstSite(t, w1)
	tok := inviteS2(t, w1, "127.0.0.1:0")
	tok.Workspace = strings.Repeat("0", 40)

	dir := filepath.Join(t.TempDir(), "joined")
	r := &runner{t: t, root: dir}
	r.run("join", encodeToken(t, tok), "--dir", dir).
		fails(t, "join another workspace", "different workspace")
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("join left a half-joined workspace at %s", dir)
	}
}

func TestJoinNeedsAnInvitation(t *testing.T) {
	w := newWorkspace(t)
	p := firstSite(t, w)
	ws, err := join.WorkspaceID(w.root)
	if err != nil {
		t.Fatal(err)
	}
	key, err := daemon.PublicKey(layout.New(w.root))
	if err != nil {
		t.Fatal(err)
	}
	token, err := join.Encode(join.Token{
		Workspace: ws, Site: "s2", Peer: "s1", Reach: p,
		PeerKey: key, Nonce: "not-invited",
	})
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "joined")
	r := &runner{t: t, root: dir}
	r.run("join", token, "--dir", dir).
		fails(t, "join without an invitation", "not declared")
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("join left a half-joined workspace at %s", dir)
	}
}

func TestInvitationIsOneTime(t *testing.T) {
	w1 := newWorkspace(t)
	firstSite(t, w1)
	tok := inviteS2(t, w1, "127.0.0.1:0")

	first := filepath.Join(t.TempDir(), "first")
	(&runner{t: t, root: first}).run("join", encodeToken(t, tok), "--dir", first).ok(t, "the first join")

	second := filepath.Join(t.TempDir(), "second")
	r2 := &runner{t: t, root: second}
	r2.run("join", encodeToken(t, tok), "--dir", second).
		fails(t, "reuse the invitation", "has been used")
	if _, err := os.Stat(second); err == nil {
		t.Fatalf("the refused join left a workspace at %s", second)
	}
}

func TestDaemonRecoversFromAStaleLock(t *testing.T) {
	w := newWorkspace(t)
	d := startDaemon(t, w, "127.0.0.1:0")
	if err := d.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = d.cmd.Wait()

	// The lock is still on disk and names a process that is gone; a new daemon must
	// remove it rather than refuse forever.
	startDaemon(t, w, "127.0.0.1:0")
	w.r().run("daemon", "stop").ok(t, "stop")
}

// twoSiteDaemons places the two actors; the setup already started the daemons.
func twoSiteDaemons(t *testing.T, ts *twoSites) string {
	t.Helper()
	ts.w1.r().as("a1").run("actor", "hello").ok(t, "a1 hello")
	ts.w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "declare a2")
	ts.w2.r().run("sync").ok(t, "sync a2")
	ts.w1.r().run("sync").ok(t, "sync site one")
	return ts.p1
}

func TestMessagesSurviveADaemonRestart(t *testing.T) {
	ts := setupTwoSites(t)
	p1 := twoSiteDaemons(t, ts)

	id := strings.TrimSpace(ts.w2.r().as("a2").run("message", "send", "a1",
		"--subject", "survives a restart").ok(t, "send").stdout)
	ts.w1.r().run("daemon", "stop").ok(t, "stop")
	startDaemon(t, ts.w1, p1)

	list := ts.w1.r().as("a1").run("message", "list").ok(t, "list after the restart")
	if !strings.Contains(list.stdout, id) {
		t.Fatalf("the message did not survive the restart: %s", list.stdout)
	}
}

func TestSenderDoesNotQueue(t *testing.T) {
	ts := setupTwoSites(t)
	p1 := twoSiteDaemons(t, ts)

	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "the first").ok(t, "send")
	ts.w1.r().run("daemon", "stop").ok(t, "stop")
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "while down").
		fails(t, "send while the site is down", "did not answer")

	startDaemon(t, ts.w1, p1)
	list := ts.w1.r().as("a1").run("message", "list").ok(t, "list after the restart")
	if strings.Contains(list.stdout, "while down") {
		t.Fatalf("the sender queued a message it could not place: %s", list.stdout)
	}
	if !strings.Contains(list.stdout, "the first") {
		t.Fatalf("the placed message is missing: %s", list.stdout)
	}
}

func TestRetryWithTheSameIDDeliversOnce(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("actor", "declare", "a2", "--kind", "agent").ok(t, "declare a2")
	r.as("a1").run("actor", "hello").ok(t, "a1 hello")
	r.as("a2").run("actor", "hello").ok(t, "a2 hello")

	r.as("a1").run("message", "send", "a2", "--id", "fixed-id", "--subject", "once").ok(t, "send")
	r.as("a1").run("message", "send", "a2", "--id", "fixed-id", "--subject", "once").ok(t, "retry")

	msgs := jsonLines[struct {
		ID string `json:"id"`
	}](t, r.as("a2").run("message", "list", "--json").ok(t, "list").stdout)
	count := 0
	for _, m := range msgs {
		if m.ID == "fixed-id" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the retry delivered %d copies", count)
	}
}
