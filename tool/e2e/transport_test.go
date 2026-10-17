package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func proxyURL(t *testing.T, w *workspace, peer, repo, service string) string {
	t.Helper()
	return "ext::" + thrumBin + " --root " + w.root + " git-proxy --peer " + peer +
		" --repo " + repo + " --service " + service
}

func TestDaemonServesGit(t *testing.T) {
	ts := setupTwoSites(t)
	p1 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ts.w2.r().run("site", "reach", "s1", "--at", p1).ok(t, "reach")
	startDaemon(t, ts.w1, p1)

	url := proxyURL(t, ts.w2, "s1", "line", "upload-pack")
	out, err := exec.Command("git", "-c", "protocol.ext.allow=always", "-C", ts.w2.root,
		"ls-remote", url).CombinedOutput()
	if err != nil {
		t.Fatalf("ls-remote: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "refs/heads/main") {
		t.Fatalf("the line's refs: %s", out)
	}

	dest := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "-c", "protocol.ext.allow=always", "clone", "-q", url, dest).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dest, "doc", "actor", "a1.yaml")); err != nil {
		t.Fatalf("the cloned line is missing the actor: %v", err)
	}
}

// TestSyncThroughTheDaemon: after site transport, the two sites exchange both
// histories through each other's daemons; the origin directories are gone, so an
// accidental external remote would fail.
func TestSyncThroughTheDaemon(t *testing.T) {
	ts := setupTwoSites(t)
	p1 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	p2 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ts.w1.r().run("site", "reach", "s2", "--at", p2).ok(t, "w1 reach")
	ts.w2.r().run("site", "reach", "s1", "--at", p1).ok(t, "w2 reach")
	startDaemon(t, ts.w1, p1)
	startDaemon(t, ts.w2, p2)

	ts.w1.r().run("site", "transport", "s2").ok(t, "w1 transport")
	ts.w2.r().run("site", "transport", "s1").ok(t, "w2 transport")
	if err := os.RemoveAll(ts.origin); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(ts.ledger); err != nil {
		t.Fatal(err)
	}

	ts.w1.r().run("actor", "declare", "a4", "--kind", "agent").ok(t, "a4")
	ts.w1.r().run("sync").ok(t, "w1 sync")
	// The push updated site two's checked-out line as it landed.
	ts.w2.mustExist("doc/actor/a4.yaml")

	ts.w2.r().run("actor", "declare", "a5", "--kind", "agent").ok(t, "a5")
	ts.w2.r().run("sync").ok(t, "w2 sync")
	ts.w1.r().run("sync").ok(t, "w1 settles")
	ts.w2.r().run("sync").ok(t, "w2 settles")
	for _, w := range []*workspace{ts.w1, ts.w2} {
		list := w.r().run("actor", "list").ok(t, "list").stdout
		if !strings.Contains(list, "a4") || !strings.Contains(list, "a5") {
			t.Fatalf("%s does not see both actors:\n%s", w.root, list)
		}
	}
	ts.w1.r().run("verify").ok(t, "verify w1")
	ts.w2.r().run("verify").ok(t, "verify w2")
}
