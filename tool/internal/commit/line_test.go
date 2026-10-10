package commit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/commit"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/resources"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Two sites of one workspace, each with its own clone of the shared record line.
func twoSites(t *testing.T) (workspace.Layout, workspace.Layout) {
	t.Helper()
	first := newSite(t, "build-01")
	shared := filepath.Join(t.TempDir(), "records.git")
	git(t, first.Root, "init", "--bare", "--quiet", shared)
	git(t, first.Root, "remote", "add", "origin", shared)

	// The roster is what the other sites read, so both sites are declared before either
	// joins.
	if err := os.WriteFile(first.ResourceKind("site"),
		[]byte("site:\n  - name: build-01\n    purpose: build\n  - name: lab-02\n    purpose: lab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(first.Root, append(gitx.IdentityArgs(first.Root, "tester"),
		"add", "-A", "--", "doc")...); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(first.Root, append(gitx.IdentityArgs(first.Root, "tester"),
		"commit", "--quiet", "-m", "resource: declared the sites")...); err != nil {
		t.Fatal(err)
	}
	git(t, first.Root, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	second, err := workspace.Join(workspace.JoinOptions{
		Root:   filepath.Join(t.TempDir(), "lab"),
		Remote: shared,
		Site:   "lab-02",
	})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	return first, second
}

// Two sites recording different things end up in one line, in one order, with no merge:
// whoever publishes second is put on top of what was published first.
func TestTwoSitesRecordingDifferentThingsKeepOneLine(t *testing.T) {
	a, b := twoSites(t)

	// B becomes current, and then the line moves before B records anything: this is the
	// race the protocol exists for.
	if err := commit.Current(b); err != nil {
		t.Fatal(err)
	}
	declare(t, a, "gpu", "gpu", "the only GPU")

	// B records a different file, so its event can go on top of A's.
	if err := os.WriteFile(b.ResourceKind("worktree"), []byte("worktree:\n  - name: uploader\n    purpose: work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := commit.Event(b, commit.KindResource, "worktree", "declared uploader", "doc/resource/worktree.yaml"); err != nil {
		t.Fatalf("an event on a file nobody else moved should be published: %v", err)
	}

	if !strings.Contains(log(t, b.Root), "declared gpu") || !strings.Contains(log(t, b.Root), "declared uploader") {
		t.Errorf("both events should be recorded:\n%s", log(t, b.Root))
	}
	if merges := gitOut(t, b.Root, "rev-list", "--merges", "--all"); strings.TrimSpace(merges) != "" {
		t.Errorf("the record line must have no merge commits:\n%s", merges)
	}
	// And both sites see the same line, in the same order. A has to look before it can
	// see what B published.
	git(t, a.Root, "fetch", "--quiet", "origin")
	if line(t, a.Root) != line(t, b.Root) {
		t.Errorf("the two sites disagree about the line:\nA:\n%s\nB:\n%s", line(t, a.Root), line(t, b.Root))
	}
	if !strings.Contains(line(t, a.Root), "declared gpu") {
		t.Errorf("A's event should be in the line:\n%s", line(t, a.Root))
	}
}

// Two sites recording the same file cannot both be in the line, and the one that loses is
// taken back out: nothing is published, nothing is merged, and what it wrote survives.
func TestTwoSitesRecordingTheSameFileRefuseRatherThanDiverge(t *testing.T) {
	a, b := twoSites(t)

	if err := commit.Current(b); err != nil {
		t.Fatal(err)
	}
	// A declares the GPU first.
	declare(t, a, "gpu", "gpu", "the GPU, as A describes it")

	// B declares the same resource, having read the registry before A published.
	mine := []byte("gpu:\n  - name: gpu\n    purpose: the GPU, as B describes it\n")
	if err := os.WriteFile(b.ResourceKind("gpu"), mine, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := commit.Event(b, commit.KindResource, "gpu", "declared gpu", "doc/resource/gpu.yaml")
	if err == nil {
		t.Fatal("an event on a file another site moved should be refused")
	}
	if !strings.Contains(err.Error(), "doc/resource/gpu.yaml") {
		t.Errorf("the refusal should name the file: %v", err)
	}

	// The line has A's event and not B's.
	subjects := log(t, b.Root)
	if strings.Count(subjects, "declared gpu") != 1 {
		t.Errorf("exactly one of the two events should be in the line:\n%s", subjects)
	}
	if merges := gitOut(t, b.Root, "rev-list", "--merges", "--all"); strings.TrimSpace(merges) != "" {
		t.Errorf("the record line must have no merge commits:\n%s", merges)
	}
	// And B's own words are still where B wrote them, uncommitted.
	kept, readErr := os.ReadFile(b.ResourceKind("gpu"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(kept) != string(mine) {
		t.Errorf("what the refused event wrote should survive:\n%s", kept)
	}
}

// A site that is alone publishes later, and its unpublished records go on top of what was
// published meanwhile: they were never in the line, so nothing anybody has seen is
// rewritten.
func TestARecordMadeWhileAloneIsPublishedOnTopLater(t *testing.T) {
	a, b := twoSites(t)

	// B is cut off, and records something anyway.
	real := gitOut(t, b.Root, "remote", "get-url", "origin")
	git(t, b.Root, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "not-there"))
	declare(t, b, "gpu", "gpu", "B's GPU")
	git(t, b.Root, "remote", "set-url", "origin", strings.TrimSpace(real))

	// Meanwhile the line moves, on a different file.
	declare(t, a, "worktree", "uploader", "work")

	// B records again, which brings its own record up to date first, and publishes both.
	declare(t, b, "dataset", "corpus", "the corpus")

	git(t, a.Root, "fetch", "--quiet", "origin")
	git(t, b.Root, "fetch", "--quiet", "origin")
	subjects := line(t, a.Root)
	for _, want := range []string{"declared gpu", "declared uploader", "declared corpus"} {
		if !strings.Contains(subjects, want) {
			t.Errorf("the line should hold everything recorded, and does not hold %q:\n%s", want, subjects)
		}
	}
	if merges := gitOut(t, a.Root, "rev-list", "--merges", "--all"); strings.TrimSpace(merges) != "" {
		t.Errorf("the record line must have no merge commits:\n%s", merges)
	}
	if line(t, b.Root) != subjects {
		t.Errorf("the sites disagree about the line after publishing:\n%s\n---\n%s", line(t, b.Root), subjects)
	}
}

// A workspace nobody else has cannot diverge from anybody, and nothing is pushed.
func TestAWorkspaceWithNoRemoteJustRecords(t *testing.T) {
	l := newSite(t, "alone")
	if err := commit.Current(l); err != nil {
		t.Fatal(err)
	}
	declare(t, l, "gpu", "gpu", "the GPU")
	if !strings.Contains(log(t, l.Root), "declared gpu") {
		t.Errorf("the record should be in the line:\n%s", log(t, l.Root))
	}
}

// declare is the two steps a declaration is: the registry is edited, and the event is
// recorded. The command does both, and so must anything that means to declare something.
func declare(t *testing.T, l workspace.Layout, kind, name, purpose string) {
	t.Helper()
	if err := resources.Declare(l, kind, resources.Entry{Name: name, Purpose: purpose}); err != nil {
		t.Fatal(err)
	}
	if _, err := commit.Event(l, commit.KindResource, kind, "declared "+name,
		filepath.Join("doc", "resource", kind+".yaml")); err != nil {
		t.Fatal(err)
	}
}

func newSite(t *testing.T, site string) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  site,
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(dir, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitx.Run(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// line is the shared line as this site sees it, in the order it is in: what every ref
// happens to point at, sorted by date, is not the same question.
func line(t *testing.T, root string) string {
	t.Helper()
	out, err := gitx.Run(root, "log", "--pretty=%s", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func log(t *testing.T, root string) string {
	t.Helper()
	out, err := gitx.Run(root, "log", "--pretty=%s", "--all")
	if err != nil {
		t.Fatal(err)
	}
	return out
}
