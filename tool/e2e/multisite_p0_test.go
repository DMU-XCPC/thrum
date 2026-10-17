package e2e

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inviteToken is the wire shape of an invitation, mirrored here so a test can
// forge or inspect one without importing the tool.
type inviteToken struct {
	Workspace string            `json:"workspace"`
	Site      string            `json:"site"`
	Line      string            `json:"line"`
	Ledger    string            `json:"ledger"`
	Store     string            `json:"store"`
	Repos     map[string]string `json:"repos"`
	Nonce     string            `json:"nonce"`
}

func decodeInvite(t *testing.T, s string) inviteToken {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		t.Fatalf("decoding the invitation: %v", err)
	}
	var tok inviteToken
	if err := json.Unmarshal(raw, &tok); err != nil {
		t.Fatalf("decoding the invitation: %v", err)
	}
	return tok
}

func encodeInvite(t *testing.T, tok inviteToken) string {
	t.Helper()
	raw, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// origins gives a site a bare origin for each shared history and publishes.
func origins(t *testing.T, w *workspace) (line, ledger string) {
	t.Helper()
	dir := t.TempDir()
	line = filepath.Join(dir, "line.git")
	ledger = filepath.Join(dir, "ledger.git")
	runGit(t, w.root, "init", "--bare", line)
	runGit(t, w.root, "remote", "add", "origin", line)
	runGit(t, w.root, "push", "-q", "-u", "origin", "main")
	runGit(t, w.root, "init", "--bare", ledger)
	runGit(t, w.path("handoff"), "remote", "add", "origin", ledger)
	runGit(t, w.path("handoff"), "push", "-q", "-u", "origin", "main")
	return line, ledger
}

func inviteS2(t *testing.T, w *workspace, line, ledger, at string) string {
	t.Helper()
	out := w.r().run("site", "invite", "s2", "--at", at,
		"--line", line, "--ledger", ledger,
		"--store", w.path("repos", "store.git"),
		"--code", "code="+w.path("repos", "code.git")).ok(t, "invite").stdout
	return strings.TrimSpace(out)
}

func TestJoinRefusesAnotherWorkspace(t *testing.T) {
	w1 := newWorkspace(t)
	line1, ledger1 := origins(t, w1)
	token := inviteS2(t, w1, line1, ledger1, "127.0.0.1:0")
	runGit(t, w1.root, "push", "-q", "origin", "main")

	w2 := newWorkspace(t)
	line2, ledger2 := origins(t, w2)
	tok := decodeInvite(t, token)
	tok.Line, tok.Ledger = line2, ledger2
	tok.Store = w2.path("repos", "store.git")
	tok.Repos = map[string]string{"code": w2.path("repos", "code.git")}

	dir := filepath.Join(t.TempDir(), "joined")
	r := &runner{t: t, root: dir}
	r.run("join", encodeInvite(t, tok), "--dir", dir).
		fails(t, "join another workspace", "different workspace")
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("join left a half-joined workspace at %s", dir)
	}
}

func TestJoinNeedsAnInvitation(t *testing.T) {
	w := newWorkspace(t)
	line, ledger := origins(t, w)
	tok := inviteToken{
		Workspace: strings.TrimSpace(runGit(t, w.root, "rev-list", "--max-parents=0", "HEAD")),
		Site:      "s2",
		Line:      line,
		Ledger:    ledger,
		Store:     w.path("repos", "store.git"),
		Repos:     map[string]string{"code": w.path("repos", "code.git")},
		Nonce:     "not-invited",
	}
	dir := filepath.Join(t.TempDir(), "joined")
	r := &runner{t: t, root: dir}
	r.run("join", encodeInvite(t, tok), "--dir", dir).
		fails(t, "join without an invitation", "roster does not declare")
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("join left a half-joined workspace at %s", dir)
	}
}

func TestInvitationIsOneTime(t *testing.T) {
	w1 := newWorkspace(t)
	line, ledger := origins(t, w1)
	token := inviteS2(t, w1, line, ledger, "127.0.0.1:0")
	runGit(t, w1.root, "push", "-q", "origin", "main")

	first := filepath.Join(t.TempDir(), "first")
	(&runner{t: t, root: first}).run("join", token, "--dir", first).ok(t, "the first join")

	second := filepath.Join(t.TempDir(), "second")
	r2 := &runner{t: t, root: second}
	r2.run("join", token, "--dir", second).
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
