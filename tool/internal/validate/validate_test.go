package validate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/validate"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// The validation list is one list in the design and one function here, so this file
// goes through it rule by rule: each test breaks exactly one thing and asks for the
// finding that names it. A test that only checked that a good workspace validates would
// pass just as well with half the rules missing.

func fixture(t *testing.T) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "build-01",
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The template ships the conventions a workspace starts with, and these tests are
	// about the checks rather than about that set, so they begin from an empty one.
	entries, err := os.ReadDir(l.Convention())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(l.Convention(), e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

// seeded is a fixture whose code repository has a commit, so that a handoff's base and
// head can name something that resolves.
func seeded(t *testing.T) (workspace.Layout, string) {
	t.Helper()
	l := fixture(t)

	clone := filepath.Join(t.TempDir(), "clone")
	if _, err := gitx.Run("", "clone", "--quiet", l.CodeRepo(), clone); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "--quiet", "-m", "project: first commit"},
		{"push", "--quiet", "origin", "HEAD:refs/heads/main"},
	} {
		all := append(gitx.IdentityArgs(clone, "tester"), args...)
		if _, err := gitx.Run(clone, all...); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	head, err := gitx.Run(l.CodeRepo(), "rev-parse", "main")
	if err != nil {
		t.Fatal(err)
	}
	return l, strings.TrimSpace(head)
}

func write(t *testing.T, l workspace.Layout, path, content string) {
	t.Helper()
	full := filepath.Join(l.Root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findings(l workspace.Layout) []string {
	raw := validate.Validate(l)
	out := make([]string, 0, len(raw))
	for _, f := range raw {
		out = append(out, f.String())
	}
	return out
}

// wants asserts that some finding mentions what the rule is about, and that a rule
// actually fired.
func wants(t *testing.T, l workspace.Layout, parts ...string) {
	t.Helper()
	got := findings(l)
	if len(got) == 0 {
		t.Fatalf("nothing was reported, and these were expected: %v", parts)
	}
	for _, part := range parts {
		found := false
		for _, f := range got {
			if strings.Contains(f, part) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no finding mentions %q, and these were reported:\n  %s",
				part, strings.Join(got, "\n  "))
		}
	}
}

func clean(t *testing.T, l workspace.Layout) {
	t.Helper()
	if got := findings(l); len(got) != 0 {
		t.Errorf("a workspace that is right should report nothing:\n  %s", strings.Join(got, "\n  "))
	}
}

func goodHandoff(job, sha string) string {
	return "---\njob: " + job + "\nagent: tester\nbranch: job/" + job + "\n" +
		"base: " + sha + "\nhead: " + sha + "\nstatus: partial\n" +
		"started: 2026-10-09T02:10:00Z\nended: 2026-10-09T03:00:00Z\nnext: keep going\n" +
		"---\n\n## What changed\n\n## What was verified\n\n## What remains\n\n## Open questions\n"
}

func TestAGoodWorkspaceReportsNothing(t *testing.T) {
	l, sha := seeded(t)
	write(t, l, "doc/resource/worktree.yaml", "worktree:\n  - name: uploader\n    purpose: Uploader reliability work\n    ephemeral: false\n")
	if err := os.MkdirAll(l.Worktree("uploader"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, l, "doc/task/add-retry/brief.md", briefFor("add-retry", "active"))
	write(t, l, "handoff/add-retry/2026-10-09T02-10-00Z.md", goodHandoff("add-retry", sha))
	write(t, l, "doc/convention/01-python.md", "# Python\n\nHow we write it.\n")
	write(t, l, "doc/topic/uploader/overview.md", "# The uploader\n\nHow it works.\n")
	write(t, l, "doc/topic/uploader/retry/backoff.md", "# Backoff\n\nHow it backs off.\n")
	write(t, l, "doc/asset/sample/2026-10-09.md",
		"---\nasset: sample\nversion: \"2026-10-09\"\nlocation: store\nref: sample@2026-10-09\nsha256: "+strings.Repeat("a", 64)+"\nbytes: 8\n---\n\n# sample 2026-10-09\n\n## Description\n")
	write(t, l, "doc/resource/gpu.yaml", "gpu:\n  - name: gpu\n    purpose: The only GPU\n    capacity: 1\n")
	write(t, l, "var/lease/gpu.1.yaml", "resource: gpu\nunit: \"1\"\noccupant: tester\njob: add-retry\nsince: 2026-10-09T02:00:00Z\nexpires: 2026-10-09T04:00:00Z\n")
	write(t, l, "var/occupy/add-retry.yaml", "occupant: tester\nsince: 2026-10-09T02:00:00Z\n")

	clean(t, l)
}

func briefFor(job, status string) string {
	return "---\njob: " + job + "\nstatus: " + status + "\nbranch: job/" + job + "\nowned_paths: []\n" +
		"---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
}

func TestTheSiteCanNameItself(t *testing.T) {
	l := fixture(t)
	write(t, l, "var/site", "Not A Slug\n")
	wants(t, l, "var/site")
}

func TestARegistryIsOneKindAndItsNamesAreUnique(t *testing.T) {
	l := fixture(t)
	// The key and the file name disagree, and the same name is declared twice across
	// two kinds.
	write(t, l, "doc/resource/worktree.yaml", "worktrees:\n  - name: shared\n    purpose: One\n")
	write(t, l, "doc/resource/gpu.yaml", "gpu:\n  - name: shared\n    purpose: Two\n")
	write(t, l, "doc/resource/host.yaml", "host:\n  - name: shared\n    purpose: Three\n")

	got := findings(l)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "worktree.yaml") {
		t.Errorf("a kind file whose key is not its name should be reported:\n%s", joined)
	}
	if !strings.Contains(joined, "unique") {
		t.Errorf("a name used twice should be reported:\n%s", joined)
	}
}

func TestABriefIsCheckedAgainstItsOwnPath(t *testing.T) {
	l, _ := seeded(t)
	// The job id in the file is not the directory it lives in.
	write(t, l, "doc/task/add-retry/brief.md", briefFor("something-else", "active"))
	wants(t, l, "equals its directory name")

	// An invented status.
	l2, _ := seeded(t)
	write(t, l2, "doc/task/add-retry/brief.md", briefFor("add-retry", "nearly"))
	wants(t, l2, "status")

	// A closed job that is still active.
	l3, _ := seeded(t)
	write(t, l3, "doc/archive/task/add-retry/brief.md", briefFor("add-retry", "active"))
	wants(t, l3, "stays closed")

	// An owner that is not an actor name.
	l4, _ := seeded(t)
	write(t, l4, "doc/task/add-retry/brief.md",
		"---\njob: add-retry\nstatus: active\nbranch: job/add-retry\nowned_paths: []\nowner: Not An Actor\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n")
	wants(t, l4, "owner")

	// Sections out of order.
	l5, _ := seeded(t)
	write(t, l5, "doc/task/add-retry/brief.md",
		"---\njob: add-retry\nstatus: active\nbranch: job/add-retry\nowned_paths: []\n---\n\n## Goal\n\n## Acceptance criteria\n\n## Scope\n")
	wants(t, l5, "section")
}

func TestAHandoffIsCheckedAgainstItsOwnNameAndItsBranch(t *testing.T) {
	l, sha := seeded(t)

	// `started` must equal the file name converted.
	write(t, l, "handoff/add-retry/2026-10-09T02-10-00Z.md",
		strings.Replace(goodHandoff("add-retry", sha), "started: 2026-10-09T02:10:00Z", "started: 2026-10-09T05:00:00Z", 1))
	wants(t, l, "file name converted")

	// The branch is job/ plus the directory.
	l2, sha2 := seeded(t)
	write(t, l2, "handoff/add-retry/2026-10-09T02-10-00Z.md",
		strings.Replace(goodHandoff("add-retry", sha2), "branch: job/add-retry", "branch: main", 1))
	wants(t, l2, "branch")

	// An agent that is not an actor name.
	l3, sha3 := seeded(t)
	write(t, l3, "handoff/add-retry/2026-10-09T02-10-00Z.md",
		strings.Replace(goodHandoff("add-retry", sha3), "agent: tester", "agent: Not An Actor", 1))
	wants(t, l3, "agent")

	// `next` is none exactly when the status is done.
	l4, sha4 := seeded(t)
	write(t, l4, "handoff/add-retry/2026-10-09T02-10-00Z.md",
		strings.Replace(goodHandoff("add-retry", sha4), "status: partial", "status: done", 1))
	wants(t, l4, "next")

	// blocked_by belongs to a blocked handoff and to no other.
	l5, sha5 := seeded(t)
	write(t, l5, "handoff/add-retry/2026-10-09T02-10-00Z.md",
		strings.Replace(goodHandoff("add-retry", sha5), "next: keep going", "next: keep going\nblocked_by:\n  - other-job", 1))
	wants(t, l5, "blocked_by")

	// base and head have to resolve in the code repository.
	l6, _ := seeded(t)
	write(t, l6, "handoff/add-retry/2026-10-09T02-10-00Z.md", goodHandoff("add-retry", strings.Repeat("b", 40)))
	wants(t, l6, "resolve")

	// An artifact path lies under job/<job-id>/, and its hash is 64 hexadecimal digits.
	l7, sha7 := seeded(t)
	artifact := strings.Replace(goodHandoff("add-retry", sha7), "next: keep going",
		"next: keep going\nartifacts:\n  - path: results/report.txt\n    sha256: nothex\n", 1)
	write(t, l7, "handoff/add-retry/2026-10-09T02-10-00Z.md", artifact)
	wants(t, l7, "artifact")

	// supersedes names a sibling that exists.
	l8, sha8 := seeded(t)
	write(t, l8, "handoff/add-retry/2026-10-09T02-10-00Z.md",
		strings.Replace(goodHandoff("add-retry", sha8), "next: keep going", "next: keep going\nsupersedes: 2026-10-08T02-10-00Z.md", 1))
	wants(t, l8, "supersedes")
}

func TestASupersedesChainIsRefusedACycle(t *testing.T) {
	l, sha := seeded(t)
	first := strings.Replace(goodHandoff("add-retry", sha), "next: keep going",
		"next: keep going\nsupersedes: 2026-10-09T04-00-00Z.md", 1)
	second := strings.Replace(goodHandoff("add-retry", sha), "next: keep going",
		"next: keep going\nsupersedes: 2026-10-09T02-10-00Z.md", 1)
	write(t, l, "handoff/add-retry/2026-10-09T02-10-00Z.md", first)
	write(t, l, "handoff/add-retry/2026-10-09T04-00-00Z.md", second)
	wants(t, l, "cycle")
}

func TestAManifestIsCheckedAgainstItsOwnPath(t *testing.T) {
	body := "\n---\n\n# %s\n\n## Description\n"

	// The asset and the version are the path they live at.
	l := fixture(t)
	write(t, l, "doc/asset/sample/2026-10-09.md",
		"---\nasset: other\nversion: \"2026-01-01\"\nlocation: store\nref: sample@2026-10-09\n---\n"+strings.Replace(body, "%s", "x", 1))
	wants(t, l, "equals its directory name", "equals its file name")

	// The tag is asset@version, and the hash is 64 hexadecimal digits.
	l2 := fixture(t)
	write(t, l2, "doc/asset/sample/2026-10-09.md",
		"---\nasset: sample\nversion: \"2026-10-09\"\nlocation: store\nref: sample@2026-01-01\nsha256: nope\n---\n\n# sample 2026-10-09\n\n## Description\n")
	wants(t, l2, "asset tag", "sha256")

	// A live manifest needs its Description; a retired one needs its title.
	l3 := fixture(t)
	write(t, l3, "doc/asset/sample/2026-10-09.md",
		"---\nasset: sample\nversion: \"2026-10-09\"\nlocation: store\nref: sample@2026-10-09\n---\n\n# sample 2026-10-09\n")
	wants(t, l3, "section")

	l4 := fixture(t)
	write(t, l4, "doc/archive/asset/sample/2026-10-09.md",
		"---\nasset: sample\nversion: \"2026-10-09\"\nlocation: store\nref: sample@2026-10-09\n---\n\nno title here\n")
	wants(t, l4, "title")
}

func TestALeaseIsCheckedAgainstItsResource(t *testing.T) {
	l := fixture(t)
	write(t, l, "doc/resource/gpu.yaml", "gpu:\n  - name: gpu\n    purpose: The only GPU\n    capacity: 2\n")

	// The file name and the fields agree, the resource exists, the unit is inside the
	// capacity, and the occupant is an actor name.
	write(t, l, "var/lease/gpu.3.yaml", "resource: gpu\nunit: \"3\"\noccupant: tester\njob: x\n")
	wants(t, l, "capacity")

	write(t, l, "var/lease/nothing.1.yaml", "resource: nothing\nunit: \"1\"\noccupant: tester\njob: x\n")
	wants(t, l, "declared")

	write(t, l, "var/lease/gpu.1.yaml", "resource: something-else\nunit: \"1\"\noccupant: tester\njob: x\n")
	wants(t, l, "file name")

	write(t, l, "var/lease/gpu.2.yaml", "resource: gpu\nunit: \"2\"\noccupant: Not An Actor\njob: x\n")
	wants(t, l, "occupant")
}

func TestOccupancyIsAnchoredByItsJob(t *testing.T) {
	l, _ := seeded(t)
	write(t, l, "doc/task/add-retry/brief.md", briefFor("add-retry", "active"))

	write(t, l, "var/occupy/add-retry.yaml", "occupant: tester\nsince: 2026-10-09T02:00:00Z\n")
	clean(t, l)

	write(t, l, "var/occupy/no-such-job.yaml", "occupant: tester\nsince: 2026-10-09T02:00:00Z\n")
	wants(t, l, "anchored")

	write(t, l, "var/occupy/add-retry.yaml", "occupant: Not An Actor\nsince: 2026-10-09T02:00:00Z\n")
	wants(t, l, "occupant")
}

func TestConventionsNumberThemselves(t *testing.T) {
	l := fixture(t)
	write(t, l, "doc/convention/01-python.md", "# Python\n")
	clean(t, l)

	// Two files sharing a number.
	write(t, l, "doc/convention/01-go.md", "# Go\n")
	wants(t, l, "number")

	// A file that is not named the way a convention is.
	write(t, l, "doc/convention/python.md", "# Python\n")
	wants(t, l, "two digits")

	// Every retirable document has exactly one H1.
	l2 := fixture(t)
	write(t, l2, "doc/convention/01-python.md", "no title\n")
	wants(t, l2, "title")

	l3 := fixture(t)
	write(t, l3, "doc/convention/01-python.md", "# One\n\n# Two\n")
	wants(t, l3, "title")
}

func TestTopicsHaveOneTitle(t *testing.T) {
	l := fixture(t)
	write(t, l, "doc/topic/uploader/overview.md", "# The uploader\n\nHow it works.\n")
	write(t, l, "doc/topic/uploader/retry/backoff.md", "# Backoff\n\nHow it backs off.\n")
	clean(t, l)

	write(t, l, "doc/topic/broken/overview.md", "no title\n")
	wants(t, l, "title")
}

func TestADirectoryWithNoDeclarationIsTheErrorDirection(t *testing.T) {
	l := fixture(t)
	write(t, l, "doc/resource/worktree.yaml", "worktree:\n  - name: uploader\n    purpose: Declared\n    ephemeral: false\n")

	// Declared but not materialised is normal; materialised but not declared is the
	// error, and only that direction is reported.
	if err := os.MkdirAll(l.Worktree("uploader"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l.Worktree("stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	wants(t, l, "stray")
	if got := strings.Join(findings(l), "\n"); strings.Contains(got, "uploader") {
		t.Errorf("a declared directory is not a finding:\n%s", got)
	}
}

// The formats and the checks are one list in the design, so a value that cannot survive
// the round trip is a finding rather than a surprise later.
func TestAUnitIsQuotedSoThatYamlDoesNotReadItAsOctal(t *testing.T) {
	encoded, err := format.Encode(format.Lease{Resource: "gpu", Unit: "02", Occupant: "tester", Job: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `unit: "02"`) {
		t.Errorf("a unit that looks like a number must be quoted:\n%s", encoded)
	}
}
