package e2e

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// checkMailboxWhole: every message a reader can see names only attachments that
// are already there. A killed writer may leave an orphan directory, never a
// half-message.
func checkMailboxWhole(t *testing.T, w *workspace, actor string) {
	t.Helper()
	dir := w.path("var", "msg", actor)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		front, _, err := format.LoadDocument(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var msg records.Message
		if err := format.Decode(front, &msg); err != nil {
			t.Fatal(err)
		}
		for _, a := range msg.Attachments {
			if _, err := os.Stat(filepath.Join(dir, id, a.Name)); err != nil {
				t.Fatalf("%s names the attachment %s, which is missing", e.Name(), a.Name)
			}
		}
	}
}

func TestSlowPeerDoesNotBlock(t *testing.T) {
	ts := setupTwoSites(t)
	addr := ts.p1

	slow, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	_, _ = slow.Write([]byte(`{"verb":`))

	ts.w1.r().run("daemon", "status").ok(t, "the local socket while a peer stalls")
	c, err := daemon.Dial(layout.New(ts.w2.root), "s1")
	if err != nil {
		t.Fatalf("a second peer while one stalls: %v", err)
	}
	_ = c.Close()
}

func TestKilledDaemonLeavesNoPartialMessage(t *testing.T) {
	ts := setupTwoSites(t)
	p1 := ts.p1
	ts.w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	ts.w2.r().run("sync").ok(t, "sync a2")
	ts.w1.r().run("sync").ok(t, "w1 sync")
	ts.w1.r().run("daemon", "stop").ok(t, "stop site one")
	d := startDaemon(t, ts.w1, p1)

	// Ten megabytes is the message bound; the kill has to land mid-transfer, and
	// the resend has to be accepted.
	big := make([]byte, 10<<20)
	for i := range big {
		big[i] = byte(i % 251)
	}
	f := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(f, big, 0o644); err != nil {
		t.Fatal(err)
	}
	const id = "kill-test"
	done := make(chan result, 1)
	go func() {
		done <- ts.w2.r().as("a2").run("message", "send", "a1",
			"--id", id, "--subject", "kill", "--attach", f)
	}()
	time.Sleep(150 * time.Millisecond)
	if err := d.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = d.cmd.Wait()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the send did not return after the daemon was killed")
	}
	checkMailboxWhole(t, ts.w1, "a1")

	startDaemon(t, ts.w1, p1)
	ts.w2.r().as("a2").run("message", "send", "a1",
		"--id", id, "--subject", "kill", "--attach", f).ok(t, "the resend")
	checkMailboxWhole(t, ts.w1, "a1")
	if mail := ts.w1.r().as("a1").run("message", "list").ok(t, "list"); !strings.Contains(mail.stdout, "kill") {
		t.Fatalf("the resent message is missing: %s", mail.stdout)
	}
	out := t.TempDir()
	ts.w1.r().as("a1").run("message", "read", id, "--out", out).ok(t, "read")
	got, err := os.ReadFile(filepath.Join(out, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(big) {
		t.Fatalf("the attachment is %d bytes, want %d", len(got), len(big))
	}
}

func startDelayProxy(t *testing.T, target string, delay time.Duration) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				time.Sleep(delay)
				d, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer d.Close()
				go func() { _, _ = io.Copy(d, c) }()
				_, _ = io.Copy(c, d)
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestSlowLink(t *testing.T) {
	ts := setupTwoSites(t)
	p2 := ts.p2
	proxy := startDelayProxy(t, p2, 300*time.Millisecond)
	ts.w1.r().run("site", "reach", "s2", "--at", proxy).ok(t, "reach through the proxy")
	ts.w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	ts.w2.r().run("sync").ok(t, "sync a2")
	ts.w1.r().run("sync").ok(t, "w1 sync")

	ts.w1.r().as("a1").run("message", "send", "a2", "--subject", "slow link").ok(t, "send")
	if mail := ts.w2.r().as("a2").run("message", "list").ok(t, "mail"); !strings.Contains(mail.stdout, "slow link") {
		t.Fatalf("the message did not cross the slow link: %s", mail.stdout)
	}
}

func TestShortSoak(t *testing.T) {
	ts := setupTwoSites(t)
	ts.w1.r().as("a1").run("actor", "hello").ok(t, "a1 hello")
	ts.w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	ts.w2.r().run("sync").ok(t, "sync a2")
	ts.w1.r().run("sync").ok(t, "w1 sync")
	ts.w2.r().as("a2").run("actor", "hello").ok(t, "a2 hello")

	deadline := time.Now().Add(6 * time.Second)
	for i := 0; time.Now().Before(deadline); i++ {
		if i%2 == 0 {
			ts.w1.r().as("a1").run("message", "send", "a2", "--subject", "soak").ok(t, "send")
		} else {
			ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "soak").ok(t, "send")
		}
		ts.w1.r().run("sync").ok(t, "sync")
		ts.w2.r().run("sync").ok(t, "sync")
		time.Sleep(250 * time.Millisecond)
	}
	ts.w1.r().run("verify").ok(t, "verify w1")
	ts.w2.r().run("verify").ok(t, "verify w2")
	if mail := ts.w1.r().as("a1").run("message", "list").ok(t, "mail"); !strings.Contains(mail.stdout, "soak") {
		t.Fatalf("site one received no soak message: %s", mail.stdout)
	}
	if mail := ts.w2.r().as("a2").run("message", "list").ok(t, "mail"); !strings.Contains(mail.stdout, "soak") {
		t.Fatalf("site two received no soak message: %s", mail.stdout)
	}
}
