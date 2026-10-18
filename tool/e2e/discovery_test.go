package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDiscoveryJoinsAndFinds: no address is declared anywhere. The joiner finds the
// inviter by asking the LAN, and each side learns the other's address from its
// announcements.
func TestDiscoveryJoinsAndFinds(t *testing.T) {
	w1 := newWorkspace(t)
	pa := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	startDaemonDiscover(t, w1, pa)

	token := strings.TrimSpace(w1.r().run("site", "invite", "s2").ok(t, "invite").stdout)
	w2Root := filepath.Join(t.TempDir(), "w2")
	(&runner{t: t, root: w2Root}).run("join", token, "--dir", w2Root).ok(t, "join")
	w2 := &workspace{t: t, root: w2Root}
	pb := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	startDaemonDiscover(t, w2, pb)

	waitDiscovered(t, w1, "s2", pb)
	waitDiscovered(t, w2, "s1", pa)

	if v := readDiscovered(w2, "s1"); v != pa {
		t.Fatalf("the join did not use the invited address: %q", v)
	}

	w1.r().run("sync").ok(t, "w1 sync")
	w2.r().run("sync").ok(t, "w2 sync")
	w1.r().as("a1").run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	w1.r().run("sync").ok(t, "w1 sync a1")
	w2.r().run("sync").ok(t, "w2 sync a2")
	w2.r().as("a2").run("message", "send", "a1", "--subject", "over the LAN").ok(t, "message")
	if out := w1.r().as("a1").run("message", "list").ok(t, "a1 list").stdout; !strings.Contains(out, "over the LAN") {
		t.Fatalf("the message did not arrive over the discovered address:\n%s", out)
	}

	// This machine's own view wins over a fresh hint.
	w1.r().run("site", "reach", "s2", "--at", "127.0.0.1:1").ok(t, "local override")
	if out := w1.r().run("sync").ok(t, "w1 sync").stdout; !strings.Contains(out, "unreachable: s2") {
		t.Fatalf("the local view did not win over the hint:\n%s", out)
	}
	_ = os.Remove(filepath.Join(w1.root, "var", "config", "s2.yaml"))
	w1.r().run("sync").ok(t, "w1 sync via the hint")

	// The peer goes away: the fresh hint is tried and reported; once the hint is old
	// it is skipped instead of dialled forever.
	w2.r().run("daemon", "stop").ok(t, "stop w2")
	if out := w1.r().run("sync").ok(t, "w1 sync down").stdout; !strings.Contains(out, "unreachable: s2") {
		t.Fatalf("a fresh hint was not tried:\n%s", out)
	}
	stale := fmt.Sprintf("reach: %s\nseen: 2020-01-01T00:00:00Z\n", pb)
	if err := os.WriteFile(filepath.Join(w1.root, "var", "discovered", "s2.yaml"), []byte(stale), 0o600); err != nil {
		t.Fatalf("writing the stale hint: %v", err)
	}
	if out := w1.r().run("sync").ok(t, "w1 sync stale").stdout; strings.Contains(out, "unreachable: s2") {
		t.Fatalf("a stale hint was still dialled:\n%s", out)
	}
}

func readDiscovered(w *workspace, site string) string {
	data, err := os.ReadFile(filepath.Join(w.root, "var", "discovered", site+".yaml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "reach: "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func waitDiscovered(t *testing.T, w *workspace, site, reach string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out := w.r().run("site", "show", site, "--json").ok(t, "site show").stdout
		var v struct {
			Discovered string `json:"discovered"`
		}
		if json.Unmarshal([]byte(out), &v) == nil && v.Discovered == reach {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no discovered address for %s at %s", site, w.root)
}
