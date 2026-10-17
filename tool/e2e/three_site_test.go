package e2e

import (
	"fmt"
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
	p1, p2, p3 string
}

// setupThreeSites creates one workspace, one origin per history, and two joins.
func setupThreeSites(t *testing.T) *threeSites {
	t.Helper()
	dir := t.TempDir()
	w1 := newWorkspace(t)
	p1 := firstSite(t, w1)
	p2 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	p3 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	token2 := strings.TrimSpace(w1.r().run("site", "invite", "s2", "--at", p2).ok(t, "invite s2").stdout)
	token3 := strings.TrimSpace(w1.r().run("site", "invite", "s3", "--at", p3).ok(t, "invite s3").stdout)
	join := func(name, token string) *workspace {
		root := filepath.Join(dir, name)
		(&runner{t: t, root: root}).run("join", token, "--dir", root).ok(t, "join "+name)
		return &workspace{t: t, root: root}
	}
	w2 := join("w2", token2)
	w3 := join("w3", token3)
	wireSites(t, []*workspace{w1, w2, w3}, []string{p1, p2, p3})
	for _, w := range []*workspace{w2, w3} {
		runGit(t, w1.root, "clone", "--bare", w1.path("repos", "code.git"), w.path("repos", "code.git"))
	}
	for round := 0; round < 2; round++ {
		for _, w := range []*workspace{w1, w2, w3} {
			w.r().run("sync").ok(t, "sync")
		}
	}
	return &threeSites{t: t, w1: w1, w2: w2, w3: w3, p1: p1, p2: p2, p3: p3}
}

// TestThreeSiteClaim settles one name claimed on all three sites, in the order the
// design says: the first claim in the derived total order keeps it.
func TestThreeSiteClaim(t *testing.T) {
	ts := setupThreeSites(t)
	ts.w1.r().run("repo", "add", "shared", "--about", "from w1").ok(t, "claim on w1")
	time.Sleep(1100 * time.Millisecond)
	ts.w2.r().run("repo", "add", "shared", "--about", "from w2").ok(t, "claim on w2")
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
	// The code is the project's own; the third site fetches the evidence commit the
	// way the project would, and thrum's verify then resolves the handoff's head.
	runGit(t, ts.w3.path("repos", "code.git"), "fetch", "-q", "origin",
		"+refs/heads/*:refs/remotes/origin/*")
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

func TestOrderIndependence(t *testing.T) {
	ts := setupThreeSites(t)
	ts.w1.r().run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	ts.w2.r().run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	ts.w3.r().run("actor", "declare", "a3", "--kind", "agent").ok(t, "a3")
	for _, w := range []*workspace{ts.w1, ts.w3, ts.w2, ts.w1, ts.w3, ts.w2} {
		w.r().run("sync").ok(t, "sync")
	}
	trees := map[string]bool{}
	for _, w := range []*workspace{ts.w1, ts.w2, ts.w3} {
		trees[strings.TrimSpace(runGit(t, w.root, "rev-parse", "HEAD^{tree}"))] = true
	}
	if len(trees) != 1 {
		t.Fatalf("the three sites ended with different trees: %v", trees)
	}
}

func TestTwoSitesClaimOneSiteName(t *testing.T) {
	ts := setupTwoSites(t)
	invite := func(w *workspace) {
		w.r().run("site", "invite", "s3", "--at", "127.0.0.1:0").ok(t, "invite s3")
	}
	invite(ts.w1)
	time.Sleep(1100 * time.Millisecond)
	invite(ts.w2)
	settled := ts.w2.r().run("sync").ok(t, "settle the site name")
	if !strings.Contains(settled.stdout, "settled") {
		t.Fatalf("the two invitations were not settled: %s", settled.stdout)
	}
	ts.w1.r().run("sync").ok(t, "w1 sync")
	a := ts.w1.file("doc/site/s3.yaml")
	b := ts.w2.file("doc/site/s3.yaml")
	if a != b {
		t.Fatalf("the roster disagrees:\n--- w1 ---\n%s\n--- w2 ---\n%s", a, b)
	}
	ts.w1.r().run("verify").ok(t, "verify w1")
	ts.w2.r().run("verify").ok(t, "verify w2")
}

func TestHistoriesOnlyGrow(t *testing.T) {
	ts := setupTwoSites(t)
	lineRoot := strings.TrimSpace(runGit(t, ts.w1.root, "rev-list", "--max-parents=0", "HEAD"))
	ledgerRoot := strings.TrimSpace(runGit(t, ts.w1.path("handoff"), "rev-list", "--max-parents=0", "HEAD"))
	ts.w1.r().run("actor", "declare", "a4", "--kind", "agent").ok(t, "a4")
	event := strings.TrimSpace(runGit(t, ts.w1.root, "rev-parse", "HEAD"))
	ts.w1.r().run("sync").ok(t, "publish")
	ts.w2.r().run("sync").ok(t, "fetch")

	for _, c := range []struct{ dir, hash, what string }{
		{ts.w1.root, lineRoot, "the line's first commit at w1"},
		{ts.w2.root, lineRoot, "the line's first commit at w2"},
		{ts.w1.path("handoff"), ledgerRoot, "the ledger's first commit at w1"},
		{ts.w2.path("handoff"), ledgerRoot, "the ledger's first commit at w2"},
		{ts.w1.root, event, "the event at w1"},
		{ts.w2.root, event, "the event at w2"},
	} {
		if out, err := gitOutput(c.dir, "merge-base", "--is-ancestor", c.hash, "HEAD"); err != nil {
			t.Fatalf("%s (%s) is not an ancestor: %v\n%s", c.what, c.hash, err, out)
		}
	}
}

func TestTwoSitesOpenOneJob(t *testing.T) {
	ts := setupTwoSites(t)
	ts.w1.r().run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "from w1").ok(t, "w1")
	ts.w2.r().run("job", "open", "j1", "--repo", "code", "--label", "two", "--goal", "from w2").ok(t, "w2")
	settled := ts.w2.r().run("sync").ok(t, "settle")
	if !strings.Contains(settled.stdout, "settled") {
		t.Fatalf("the two briefs were not settled: %s", settled.stdout)
	}
	ts.w1.r().run("sync").ok(t, "w1 sync")
	for _, w := range []*workspace{ts.w1, ts.w2} {
		show := w.r().run("job", "show", "j1").ok(t, "show").stdout
		if !strings.Contains(show, "from w1") {
			t.Fatalf("%s kept the wrong brief:\n%s", w.root, show)
		}
	}
	ts.w1.r().run("verify").ok(t, "verify w1")
	ts.w2.r().run("verify").ok(t, "verify w2")
}

func TestInvitationFromANonCreator(t *testing.T) {
	ts := setupTwoSites(t)
	p3 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	token := strings.TrimSpace(ts.w2.r().run("site", "invite", "s3", "--at", p3).ok(t, "invite").stdout)
	root := filepath.Join(t.TempDir(), "w3")
	(&runner{t: t, root: root}).run("join", token, "--dir", root).ok(t, "join")
	w3 := &workspace{t: t, root: root}

	ts.w1.r().run("site", "reach", "s3", "--at", p3).ok(t, "w1 reaches s3")
	ts.w2.r().run("site", "reach", "s3", "--at", p3).ok(t, "w2 reaches s3")
	w3.r().run("site", "reach", "s1", "--at", ts.p1).ok(t, "w3 reaches s1")
	w3.r().run("site", "reach", "s2", "--at", ts.p2).ok(t, "w3 reaches s2")
	w3.r().run("site", "reach", "s3", "--at", p3).ok(t, "w3 reaches itself")
	startDaemon(t, w3, p3)

	ts.w2.r().run("sync").ok(t, "the inviter sees the key")
	ts.w1.r().run("sync").ok(t, "the creator learns through the sync")
	w3.r().run("sync").ok(t, "the joined site syncs")
	if show := ts.w1.r().run("site", "show", "s3").ok(t, "show"); !strings.Contains(show.stdout, "ssh-ed25519") {
		t.Fatalf("site one did not learn the joined site's key: %s", show.stdout)
	}
	w3.r().run("verify").ok(t, "verify the joined site")
}

func TestForgetRefusesTheForgottenSite(t *testing.T) {
	ts := setupTwoSites(t)
	ts.w2.r().as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	ts.w2.r().run("sync").ok(t, "sync a2")
	ts.w1.r().run("sync").ok(t, "w1 sync")
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "before").ok(t, "before")

	ts.w1.r().run("site", "forget", "s2").ok(t, "forget")
	ts.w1.r().run("sync").ok(t, "publish the forget")
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "after").
		fails(t, "a forgotten site", "not declared")

	// The records it published before the forget are already in the histories; it
	// cannot reach the site that forgot it any more, because its key is not in the
	// roster.
	ts.w2.r().run("sync").fails(t, "the forgotten site", "not declared")
	ts.w1.r().run("sync").ok(t, "w1 sync")
	ts.w1.r().run("verify").ok(t, "verify the site that forgot")
}
