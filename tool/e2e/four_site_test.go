package e2e

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type fourSites struct {
	t              *testing.T
	w1, w2, w3, w4 *workspace
	line, ledger   string
}

func setupFourSites(t *testing.T) *fourSites {
	t.Helper()
	dir := t.TempDir()
	w1 := newWorkspace(t)
	line := filepath.Join(dir, "line.git")
	ledger := filepath.Join(dir, "ledger.git")
	runGit(t, w1.root, "init", "--bare", line)
	runGit(t, w1.root, "remote", "add", "origin", line)
	runGit(t, w1.root, "push", "-q", "-u", "origin", "main")
	runGit(t, w1.root, "init", "--bare", ledger)
	runGit(t, w1.path("handoff"), "remote", "add", "origin", ledger)
	runGit(t, w1.path("handoff"), "push", "-q", "-u", "origin", "main")

	invite := func(name string) string {
		return strings.TrimSpace(w1.r().run("site", "invite", name, "--at", "127.0.0.1:0",
			"--line", line, "--ledger", ledger,
			"--store", w1.path("repos", "store.git"),
			"--code", "code="+w1.path("repos", "code.git")).ok(t, "invite "+name).stdout)
	}
	tokens := []string{invite("s2"), invite("s3"), invite("s4")}
	w1.r().run("sync").ok(t, "sync the invitations")

	join := func(name, token string) *workspace {
		root := filepath.Join(dir, name)
		(&runner{t: t, root: root}).run("join", token, "--dir", root).ok(t, "join "+name)
		return &workspace{t: t, root: root}
	}
	w2 := join("w2", tokens[0])
	w3 := join("w3", tokens[1])
	w4 := join("w4", tokens[2])
	w1.r().run("sync").ok(t, "w1 sync")
	for _, w := range []*workspace{w2, w3, w4} {
		w.r().run("sync").ok(t, "sync")
	}
	return &fourSites{t: t, w1: w1, w2: w2, w3: w3, w4: w4, line: line, ledger: ledger}
}

// TestFourSitesScale: four sites write 200 repository declarations, then the mesh
// syncs twice; every site has the same board.
func TestFourSitesScale(t *testing.T) {
	ts := setupFourSites(t)
	sites := []*workspace{ts.w1, ts.w2, ts.w3, ts.w4}
	for i, w := range sites {
		w.r().run("actor", "declare", fmt.Sprintf("a%d", i+1), "--kind", "agent").ok(t, "actor")
		for j := 0; j < 50; j++ {
			w.r().run("repo", "add", fmt.Sprintf("r%d-%02d", i, j), "--about", "scale").ok(t, "repo")
		}
	}
	for round := 0; round < 2; round++ {
		for _, w := range sites {
			w.r().run("sync").ok(t, "sync")
		}
	}
	for _, w := range sites {
		res := w.r().run("repo", "list", "--json").ok(t, "repo list")
		var repos []struct {
			Repo string `json:"repo"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &repos); err != nil {
			t.Fatalf("repo list at %s: %v\n%s", w.root, err, res.stdout)
		}
		if len(repos) != 201 {
			t.Fatalf("%s has %d repositories, want 201", w.root, len(repos))
		}
	}
	ts.w1.r().run("verify").ok(t, "verify the four-site line")
}

// TestThousandEvents: a thousand events at one site, then sync and verify.
func TestThousandEvents(t *testing.T) {
	ts := setupTwoSites(t)
	for i := 0; i < 1000; i++ {
		ts.w1.r().run("actor", "declare", fmt.Sprintf("x%d", i), "--kind", "agent").ok(t, "declare")
	}
	ts.w1.r().run("sync").ok(t, "publish")
	ts.w2.r().run("sync").ok(t, "fetch")

	res := ts.w2.r().run("actor", "list", "--json").ok(t, "list")
	var actors []struct {
		Actor string `json:"actor"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &actors); err != nil {
		t.Fatalf("actor list: %v\n%s", err, res.stdout)
	}
	if len(actors) < 1001 {
		t.Fatalf("site two has %d actors, want at least 1001", len(actors))
	}
	ts.w2.r().run("verify").ok(t, "verify a thousand events")
}
