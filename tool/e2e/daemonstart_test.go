package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

// TestDaemonStartsOnDemand: the daemon is an implementation detail. No command
// starts it by hand; sync, hello, a message and a join do. The help does not list
// the daemon group, though the internal command still exists.
func TestDaemonStartsOnDemand(t *testing.T) {
	a := newWorkspace(t)
	pa := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	a.r().run("site", "reach", "s1", "--at", pa).ok(t, "a address")

	pb := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	token := strings.TrimSpace(a.r().run("site", "invite", "s2", "--at", pb).ok(t, "invite").stdout)
	bRoot := filepath.Join(t.TempDir(), "b")
	(&runner{t: t, root: bRoot}).run("join", token, "--dir", bRoot).ok(t, "join")
	b := &workspace{t: t, root: bRoot}
	t.Cleanup(func() { _ = daemon.Stop(layout.New(bRoot)) })
	b.r().run("site", "reach", "s2", "--at", pb).ok(t, "b address")

	// No daemon was started by hand: the history exchange starts both.
	b.r().run("sync").ok(t, "b sync")
	a.r().run("sync").ok(t, "a sync")

	a.r().as("a1").run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	b.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	a.r().run("sync").ok(t, "a sync a1")
	b.r().run("sync").ok(t, "b sync a2")
	a.r().as("a1").run("actor", "hello").ok(t, "a1 hello")
	b.r().as("a2").run("actor", "hello").ok(t, "a2 hello")
	b.r().as("a2").run("message", "send", "a1", "--subject", "no daemon commands").ok(t, "message")
	if out := a.r().as("a1").run("message", "list").ok(t, "a1 list").stdout; !strings.Contains(out, "no daemon commands") {
		t.Fatalf("the message did not arrive:\n%s", out)
	}

	// The user-facing help does not mention the daemon; the internal command still
	// works for an operator.
	if help := a.r().run("--help").ok(t, "help").stdout; strings.Contains(help, "daemon") {
		t.Fatalf("the daemon group is still in the help:\n%s", help)
	}
	if out := a.r().run("daemon", "status").ok(t, "status").stdout; !strings.Contains(out, "site") {
		t.Fatalf("the internal status command is not reachable: %s", out)
	}

	// The discovery toggle is a local setting; the daemon reads it on its next start.
	a.r().run("site", "discover", "on").ok(t, "discover on")
	if out := a.r().run("site", "show", "s1").ok(t, "show on").stdout; !strings.Contains(out, "LAN    on") {
		t.Fatalf("discovery on was not recorded: %s", out)
	}
	a.r().run("site", "discover", "off").ok(t, "discover off")
	if out := a.r().run("site", "show", "s1").ok(t, "show off").stdout; !strings.Contains(out, "LAN    off") {
		t.Fatalf("discovery off was not recorded: %s", out)
	}
}

// TestDaemonIdlesWhenAlone: with no reachable neighbour the daemon stops itself
// after a few route intervals; the next command starts it again.
func TestDaemonIdlesWhenAlone(t *testing.T) {
	t.Setenv("THRUM_ROUTE_INTERVAL", "100ms")
	a := newWorkspace(t)
	pa := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	a.r().run("site", "reach", "s1", "--at", pa).ok(t, "address")
	a.r().run("sync").ok(t, "sync")
	l := layout.New(a.root)
	if _, err := daemon.Status(l); err != nil {
		t.Fatalf("the daemon did not start on demand: %v", err)
	}
	gone := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := daemon.Status(l); err != nil {
			gone = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !gone {
		t.Fatalf("the daemon did not stop when it had no neighbour")
	}
	a.r().run("sync").ok(t, "sync again")
	if _, err := daemon.Status(l); err != nil {
		t.Fatalf("the daemon did not come back: %v", err)
	}
}
