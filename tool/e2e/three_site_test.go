package e2e

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type threeSites struct {
	t          *testing.T
	w1, w2, w3 *workspace
	line       string
	ledger     string
}

// setupThreeSites creates one workspace, one origin per history, and two joins.
func setupThreeSites(t *testing.T) *threeSites {
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
	token2 := invite("s2")
	token3 := invite("s3")
	w1.r().run("sync").ok(t, "publish the invitations")

	join := func(name, token string) *workspace {
		root := filepath.Join(dir, name)
		(&runner{t: t, root: root}).run("join", token, "--dir", root).ok(t, "join "+name)
		return &workspace{t: t, root: root}
	}
	w2 := join("w2", token2)
	w3 := join("w3", token3)
	w1.r().run("sync").ok(t, "site one sync")
	w2.r().run("sync").ok(t, "site two sync")
	w3.r().run("sync").ok(t, "site three sync")
	return &threeSites{t: t, w1: w1, w2: w2, w3: w3, line: line, ledger: ledger}
}

// TestThreeSiteClaim settles one name claimed on all three sites, in the order the
// design says: the first claim in the derived total order keeps it.
func TestThreeSiteClaim(t *testing.T) {
	ts := setupThreeSites(t)
	ts.w1.r().run("repo", "add", "shared", "--about", "from w1").ok(t, "claim on w1")
	ts.w1.r().run("sync").ok(t, "publish w1's claim")
	time.Sleep(1100 * time.Millisecond)
	ts.w2.r().run("repo", "add", "shared", "--about", "from w2").ok(t, "claim on w2")
	ts.w2.r().run("sync").ok(t, "settle on w2")
	time.Sleep(1100 * time.Millisecond)
	ts.w3.r().run("repo", "add", "shared", "--about", "from w3").ok(t, "claim on w3")
	settled := ts.w3.r().run("sync").ok(t, "settle on w3")
	if !strings.Contains(settled.stdout, "settled") {
		t.Fatalf("the third claim was not settled: %s", settled.stdout)
	}
	ts.w1.r().run("sync").ok(t, "w1 sync")
	ts.w2.r().run("sync").ok(t, "w2 sync")
	ts.w3.r().run("sync").ok(t, "w3 sync")

	a := ts.w1.file("doc/repo/shared.yaml")
	b := ts.w2.file("doc/repo/shared.yaml")
	c := ts.w3.file("doc/repo/shared.yaml")
	if a != b || b != c {
		t.Fatalf("the sites disagree:\n--- w1 ---\n%s\n--- w2 ---\n%s\n--- w3 ---\n%s", a, b, c)
	}
	if !strings.Contains(a, "from w1") {
		t.Fatalf("the first claim did not keep the name:\n%s", a)
	}
	ts.w1.r().run("verify").ok(t, "verify w1")
	ts.w2.r().run("verify").ok(t, "verify w2")
	ts.w3.r().run("verify").ok(t, "verify w3")
}

// TestConcurrentLedgerHandoffs records two stretches for one job while the sites are
// apart: the ledger is a union, so both files survive and nothing is rewritten.
func TestConcurrentLedgerHandoffs(t *testing.T) {
	ts := setupThreeSites(t)
	ts.w1.r().run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	ts.w1.r().run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	ts.w1.r().run("sync").ok(t, "publish the job")
	ts.w2.r().run("sync").ok(t, "site two sees the job")

	// Site two works the branch too, and publishes the commit its handoff will name
	// under an evidence ref, because the job branch itself is checked out at site one.
	// A second is enough to keep the two stretches' start stamps apart.
	time.Sleep(1100 * time.Millisecond)
	gitdir := ts.w2.path("repos", "code.git")
	runGit(t, gitdir, "fetch", "origin", "+refs/heads/job/j1:refs/heads/job/j1")
	tree := strings.TrimSpace(runGit(t, gitdir, "rev-parse", "refs/heads/job/j1^{tree}"))
	h2 := strings.TrimSpace(runGit(t, gitdir, "-c", "user.name=t", "-c", "user.email=t@t",
		"commit-tree", tree, "-p", "refs/heads/job/j1", "-m", "work at site two"))
	runGit(t, gitdir, "update-ref", "refs/heads/job/j1", h2)
	runGit(t, gitdir, "push", "origin", h2+":refs/heads/evidence-j1")

	ts.w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "declare a2")

	// Neither site sees the other's handoff: both record their own stretch.
	ts.w1.r().as("a1").run("job", "handoff", "j1", "--status", "partial", "--next", "from site one").ok(t, "w1 handoff")
	ts.w2.r().as("a2").run("job", "handoff", "j1", "--status", "partial", "--next", "from site two").ok(t, "w2 handoff")

	ts.w1.r().run("sync").ok(t, "w1 sync")
	ts.w2.r().run("sync").ok(t, "w2 sync")
	ts.w1.r().run("sync").ok(t, "w1 sync again")
	ts.w3.r().run("sync").ok(t, "w3 sync")

	for _, w := range []*workspace{ts.w1, ts.w2, ts.w3} {
		files, err := filepath.Glob(w.path("handoff", "j1", "*.md"))
		if err != nil || len(files) != 2 {
			t.Fatalf("%s has %d handoffs, want two: %v", w.root, len(files), err)
		}
	}
	ts.w3.r().run("verify").ok(t, "verify the joined evidence")
}

// TestEventClockDecidesTheClaim: the derived order is the event's clock, not the
// commit time. Site one's clock is far ahead and its commit is earlier; site two's
// commit is later but its clock is smaller, and site two keeps the name.
func TestEventClockDecidesTheClaim(t *testing.T) {
	ts := setupTwoSites(t)
	ts.w1.write("var/state/clock", "9000000000000000000\n")
	early := []string{"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z"}
	late := []string{"GIT_AUTHOR_DATE=2026-06-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-06-01T00:00:00Z"}
	ts.w1.r().with(early...).run("repo", "add", "shared", "--about", "from w1").ok(t, "claim on w1")
	ts.w1.r().run("sync").ok(t, "publish w1's claim")
	ts.w2.r().with(late...).run("repo", "add", "shared", "--about", "from w2").ok(t, "claim on w2")
	settled := ts.w2.r().run("sync").ok(t, "settle")
	if !strings.Contains(settled.stdout, "settled") {
		t.Fatalf("the claim was not settled: %s", settled.stdout)
	}
	ts.w1.r().run("sync").ok(t, "w1 sync")

	a := ts.w1.file("doc/repo/shared.yaml")
	b := ts.w2.file("doc/repo/shared.yaml")
	if a != b {
		t.Fatalf("the sites disagree:\n--- w1 ---\n%s\n--- w2 ---\n%s", a, b)
	}
	if !strings.Contains(a, "from w2") {
		t.Fatalf("the smaller event clock did not keep the name:\n%s", a)
	}
}

func TestProseConflictStopsAndNamesTheFile(t *testing.T) {
	ts := setupTwoSites(t)
	ts.w1.write("doc/topic/shared/overview.md", "# Shared\n\nfrom w1\n")
	ts.w1.r().run("commit", "--summary", "added the topic", "--", "doc/topic/shared").ok(t, "w1 adds the topic")
	ts.w1.r().run("sync").ok(t, "publish the topic")
	ts.w2.r().run("sync").ok(t, "site two has the base")

	ts.w2.write("doc/topic/shared/overview.md", "# Shared\n\nfrom w2\n")
	ts.w2.r().run("commit", "--summary", "edited at w2", "--", "doc/topic/shared").ok(t, "w2 edits")
	ts.w1.write("doc/topic/shared/overview.md", "# Shared\n\nfrom w1 again\n")
	ts.w1.r().run("commit", "--summary", "edited at w1", "--", "doc/topic/shared").ok(t, "w1 edits")
	ts.w1.r().run("sync").ok(t, "publish w1's edit")

	res := ts.w2.r().run("sync")
	res.fails(t, "a prose conflict", "do not go together")
	if !strings.Contains(res.output(), "overview.md") {
		t.Fatalf("the conflict does not name the file: %s", res.output())
	}
	if out := ts.w2.git("status", "--short"); strings.TrimSpace(out) != "" {
		t.Fatalf("the merge was not aborted cleanly:\n%s", out)
	}
}

func TestWorkingAnotherSitesJobIsRefused(t *testing.T) {
	ts := setupTwoSites(t)
	ts.w1.r().run("work", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open at w1")
	ts.w1.r().run("sync").ok(t, "publish the job")
	ts.w2.r().run("sync").ok(t, "site two sees the job")

	ts.w2.r().as("a2").run("work", "j1", "--label", "up2").
		fails(t, "working another site's job", "is owned by")
}
