// Package e2e drives the workspace tool the way an actor does: through the command
// line, against a real workspace, over a whole job's life.
//
// Every other test in this repository checks one package's rules. This one checks that
// the rules still hold when they meet: that a workspace created by the tooling passes
// the tooling's own validation after every step, and that the records one operation
// writes are the records the next one reads.
package e2e_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// thrum is the built command, shared by every test in this package.
var thrum string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "thrum-e2e-")
	if err != nil {
		panic(err)
	}
	thrum = filepath.Join(dir, "thrum")
	build := exec.Command("go", "build", "-o", thrum, "./cmd/thrum")
	build.Dir = ".."
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// run runs the command and returns what it said, failing the test on a refusal.
func run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := try(t, args...)
	if err != nil {
		t.Fatalf("thrum %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// try runs the command and returns what it said, and whether it refused.
func try(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(thrum, args...)
	cmd.Env = append(os.Environ(), "THRUM_ACTOR=fixture-actor")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tryAs runs the command as another actor, for the rules that are about who is asking.
func tryAs(t *testing.T, actorName string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(thrum, args...)
	cmd.Env = append(os.Environ(), "THRUM_ACTOR="+actorName)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// refuses checks that an operation is refused, and that the refusal says why.
func refuses(t *testing.T, want string, args ...string) {
	t.Helper()
	out, err := try(t, args...)
	if err == nil {
		t.Fatalf("thrum %s should have been refused, and it said:\n%s", strings.Join(args, " "), out)
	}
	if want != "" && !strings.Contains(out, want) {
		t.Errorf("the refusal should mention %q, and it said:\n%s", want, out)
	}
}

// The whole life of a workspace and one job in it, in the order an actor meets it.
func TestAWholeJobFromCreationToCollection(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")

	// Create, and pass the tooling's own checks from the first moment.
	if out := run(t, "create", root, "--site", "build-01"); !strings.Contains(out, "build-01") {
		t.Errorf("create said: %s", out)
	}
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("a fresh workspace should validate, and said: %s", out)
	}

	// A registry is written by the tooling, one change per commit, and a project's own
	// fields survive the tooling touching the file.
	run(t, "--root", root, "resource", "declare", "--kind", "worktree", "--name", "uploader",
		"--purpose", "Uploader reliability work", "--field", "owned-by=the platform team")
	// A kind has no default: declaring a GPU must never write a worktree entry.
	refuses(t, "kind", "--root", root, "resource", "declare", "--name", "guesswork", "--purpose", "Which kind?")
	// A name is unique across every kind, and the tooling is the only writer of the
	// registries: writing one its own verify calls broken is not a thing it should do.
	refuses(t, "unique across every kind", "--root", root, "resource", "declare",
		"--kind", "cpu", "--name", "uploader", "--purpose", "The same name, another kind")
	run(t, "--root", root, "resource", "declare", "--name", "gpu",
		"--kind", "gpu", "--purpose", "The only GPU", "--capacity", "1")
	registry, err := os.ReadFile(filepath.Join(root, "doc", "resource", "worktree.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(registry), "owned-by: the platform team") {
		t.Errorf("the tooling threw away a field it does not read:\n%s", registry)
	}

	// The project itself: a code repository with something in it.
	seedCode(t, root)

	// A job that does not exist yet is a broken reference, so a lease for it is refused
	// before it is written.
	refuses(t, "job", "--root", root, "lease", "take", "--resource", "gpu", "--job", "no-such-job", "--ttl", "1h")

	// Open: the brief, the branch, and a labelled working directory, in one commit.
	run(t, "--root", root, "open", "--job", "add-retry", "--label", "uploader",
		"--owner", "fixture-actor", "--owned-paths", "src/upload/**",
		"--goal", "Retry the uploader instead of failing it",
		"--scope", "The uploader only",
		"--criteria", "A flaky upload succeeds on the second attempt")
	brief, err := os.ReadFile(filepath.Join(root, "doc", "task", "add-retry", "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Retry the uploader instead of failing it", "The uploader only", "succeeds on the second attempt"} {
		if !strings.Contains(string(brief), want) {
			t.Errorf("a brief should say what the job is for, and it does not say %q:\n%s", want, brief)
		}
	}
	// The opener is on it, because it said who it was.
	if out := run(t, "--root", root, "job", "show", "add-retry"); !strings.Contains(out, "fixture-actor") {
		t.Errorf("opening with an owner should claim the job: %s", out)
	}

	// A label is held by its job, and a second job may not take it.
	refuses(t, "holds", "--root", root, "worktree", "add", "--label", "uploader", "--job", "another-job")

	// The claim, and the lease: who is working, and what the work holds.
	run(t, "--root", root, "job", "occupy", "add-retry")
	refuses(t, "occupied", "--root", root, "job", "occupy", "add-retry", "--actor", "somebody-else")
	run(t, "--root", root, "lease", "take", "--resource", "gpu", "--job", "add-retry", "--ttl", "1h")
	// A lease is held by the job, so a unit already held is refused to everyone — and
	// which actor is asking does not enter into it.
	refuses(t, "held for", "--root", root, "lease", "take", "--resource", "gpu", "--job", "add-retry", "--ttl", "1h",
		"--actor", "somebody-else")
	refuses(t, "held for", "--root", root, "lease", "take", "--resource", "gpu", "--job", "add-retry", "--ttl", "1h")

	// Releasing a unit nobody holds is refused rather than reported as done: a release is
	// by the job that holds the lease, and a job holding nothing has nothing to give back.
	refuses(t, "is not held", "--root", root, "lease", "release", "--resource", "gpu", "--unit", "2", "--job", "add-retry")
	refuses(t, "is not held", "--root", root, "lease", "renew", "--resource", "gpu", "--unit", "2", "--job", "add-retry", "--ttl", "1h")

	// A stretch of work, recorded where the next actor will look for it.
	run(t, "--root", root, "handoff", "--job", "add-retry", "--status", "partial",
		"--next", "compare against the baseline", "--started", "2026-10-09T02:10:00Z",
		"--worktrees", "uploader", "--summary", "the flaky timeout remains")
	// The same start twice is a collision, not an edit. Everything else about this record is
	// valid, so that the collision is what the refusal is about.
	refuses(t, "already exists", "--root", root, "handoff", "--job", "add-retry", "--status", "partial",
		"--next", "compare against the baseline", "--started", "2026-10-09T02:10:00Z")

	// An asset: published, and unpacked into the payload rather than checked out
	// anywhere permanent.
	sample := filepath.Join(t.TempDir(), "sample.csv")
	if err := os.WriteFile(sample, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "--root", root, "asset", "publish", "--asset", "sample", "--version", "2026-10-09",
		"--path", sample, "--job", "add-retry", "--license", "internal")
	// A tag is never moved.
	refuses(t, "already published", "--root", root, "asset", "publish", "--asset", "sample",
		"--version", "2026-10-09", "--path", sample, "--job", "add-retry")
	run(t, "--root", root, "asset", "materialise", "--asset", "sample", "--version", "2026-10-09",
		"--job", "add-retry")
	if _, err := os.Stat(filepath.Join(root, "job", "add-retry", "data", "sample.csv")); err != nil {
		t.Errorf("the asset did not land in the payload: %v", err)
	}

	// A payload worth carrying, and one thing that never travels.
	if err := os.WriteFile(filepath.Join(root, "job", "add-retry", "out", "report.txt"),
		[]byte("the result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "job", "add-retry", "tmp", "scratch"),
		[]byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle := filepath.Join(t.TempDir(), "add-retry.tar.gz")
	run(t, "--root", root, "bundle", "export", "--job", "add-retry", "--out", bundle, "--site", "build-01")
	// A bundle belongs to a handoff and is written outside the workspace.
	refuses(t, "inside the payload", "--root", root, "bundle", "export", "--job", "add-retry",
		"--out", filepath.Join(root, "job", "add-retry", "out", "self.tar.gz"))

	// Status reads and never maintains.
	status := run(t, "--root", root, "status")
	for _, want := range []string{"add-retry", "uploader", "gpu.1", "fixture-actor"} {
		if !strings.Contains(status, want) {
			t.Errorf("status should mention %s:\n%s", want, status)
		}
	}

	// Every step so far has to have left a workspace that still validates.
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should validate after all that, and said:\n%s", out)
	}

	// Close: the final status, the brief archived, the label released, the lease gone.
	run(t, "--root", root, "close", "--job", "add-retry", "--summary", "acceptance criteria hold")
	if _, err := os.Stat(filepath.Join(root, "doc", "archive", "task", "add-retry", "brief.md")); err != nil {
		t.Errorf("a closed brief belongs in the archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "doc", "task", "add-retry")); err == nil {
		t.Error("the brief should have left doc/task/")
	}
	if _, err := os.Stat(filepath.Join(root, "var", "lease", "gpu.1.yaml")); err == nil {
		t.Error("closing a job releases the lease it still held")
	}
	idle := run(t, "--root", root, "worktree", "list")
	if !strings.Contains(idle, "detached") {
		t.Errorf("closing a job leaves its label clean and detached:\n%s", idle)
	}
	// An id is taken forever.
	refuses(t, "used before", "--root", root, "open", "--job", "add-retry", "--label", "uploader")

	// Collection: the cheap half at once, the rest after the window.
	if out := run(t, "--root", root, "collect"); !strings.Contains(out, "add-retry") {
		t.Errorf("collect said: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "job", "add-retry", "tmp")); err == nil {
		t.Error("tmp is collected on close")
	}
	if _, err := os.Stat(filepath.Join(root, "job", "add-retry", "out", "report.txt")); err != nil {
		t.Error("the larger half waits for the window")
	}
	run(t, "--root", root, "collect", "--window=0s")
	if _, err := os.Stat(filepath.Join(root, "job", "add-retry", "out")); err == nil {
		t.Error("after the window, the rest goes")
	}

	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should still validate at the end, and said:\n%s", out)
	}

	// A site with nowhere to push says so rather than doing nothing.
	refuses(t, "no remote", "--root", root, "sync")

	// With a remote, the records go, and at a job move the branch goes too.
	records := filepath.Join(t.TempDir(), "records.git")
	git(t, root, "init", "--bare", "--quiet", records)
	git(t, root, "remote", "add", "origin", records)
	branches := filepath.Join(t.TempDir(), "branches.git")
	git(t, root, "init", "--bare", "--quiet", branches)
	git(t, filepath.Join(root, ".bare"), "remote", "add", "origin", branches)
	run(t, "--root", root, "sync", "--job", "add-retry")
	// The records arrived: the coordination repository's history is what another site
	// needs in order to do anything at all.
	if out := gitOut(t, records, "log", "--oneline", "main"); !strings.Contains(out, "add-retry") {
		t.Errorf("the records did not arrive:\n%s", out)
	}
	// And so did the branch, which is what the receiving site fetches.
	if out := gitOut(t, branches, "branch", "--list"); !strings.Contains(out, "job/add-retry") {
		t.Errorf("the branch did not arrive:\n%s", out)
	}

	// The claim is still the answer, and the tooling does not pretend it is more.
	if out := run(t, "--root", root, "job", "show", "add-retry"); !strings.Contains(out, "nobody") && !strings.Contains(out, "claim") {
		t.Errorf("with nothing running there is no live answer: %s", out)
	}

	// An actor exists by being named, and the roster says where it was heard from.
	if out := run(t, "--root", root, "actor", "list"); !strings.Contains(out, "fixture-actor") {
		t.Errorf("the roster should know the names that have acted:\n%s", out)
	}

	// A document written by hand still gets committed, and the event is read from where
	// it lives.
	run(t, "--root", root, "resource", "declare", "--kind", "worktree", "--name", "second-area", "--purpose", "A second area")
	run(t, "--root", root, "open", "--job", "second-job", "--label", "second-area")
	topicDir := filepath.Join(root, "doc", "topic", "uploader")
	if err := os.MkdirAll(topicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(topicDir, "overview.md"), []byte("# The uploader\n\nIt retries.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// What an actor runs stays inside the workspace, because the tooling can say where
	// the workspace is.
	env := run(t, "--root", root, "env", "--job", "second-job")
	for _, want := range []string{"THRUM_ROOT=" + root, "THRUM_ACTOR=fixture-actor", "TMPDIR=" + filepath.Join(root, "job", "second-job", "tmp")} {
		if !strings.Contains(env, want) {
			t.Errorf("env should say %s:\n%s", want, env)
		}
	}

	if out := run(t, "--root", root, "commit", "--summary", "added the retry contract",
		"--", filepath.Join("doc", "topic", "uploader", "overview.md")); !strings.Contains(out, "topic uploader") {
		t.Errorf("the event should be read from the path: %s", out)
	}
	if out := gitOut(t, root, "log", "-1", "--pretty=%s"); !strings.Contains(out, "topic uploader: added the retry contract") {
		t.Errorf("the commit message should name the event: %s", out)
	}
	// Recording a file that is already what the line has is an answer rather than a
	// fault, because it is what happens when somebody names the wrong file.
	if out := run(t, "--root", root, "commit", "--summary", "nothing new",
		"--", filepath.Join("doc", "topic", "uploader", "overview.md")); !strings.Contains(out, "nothing to record") {
		t.Errorf("committing an unchanged file should say so: %s", out)
	}

	// One commit is one event, so two documents of different events are refused.
	refuses(t, "different event", "--root", root, "commit", "--summary", "mixed",
		"--", filepath.Join("doc", "topic", "uploader", "overview.md"), filepath.Join("doc", "resource", "worktree.yaml"))

	// What a stretch produced is recorded with the hash of the bytes that were there,
	// because that is what makes a collected artifact a detectable absence.
	artifact := filepath.Join(root, "job", "second-job", "out", "report.txt")
	if err := os.WriteFile(artifact, []byte("the result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "--root", root, "handoff", "--job", "second-job", "--status", "done",
		"--started", "2026-10-10T02:10:00Z",
		"--artifact", filepath.Join("job", "second-job", "out", "report.txt"))
	handoff, err := os.ReadFile(filepath.Join(root, "handoff", "second-job", "2026-10-10T02-10-00Z.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("the result\n"))
	if !strings.Contains(string(handoff), hex.EncodeToString(sum[:])) {
		t.Errorf("the handoff should carry the raw hash of what it names, and says:\n%s", handoff)
	}
	if !strings.Contains(string(handoff), "job/second-job/out/report.txt") {
		t.Errorf("and the path it is relative to the workspace root:\n%s", handoff)
	}

	// A unit above the resource's capacity is not a unit, a path outside the payload is not
	// an artifact, and an owner is not something another actor takes: each of these is a
	// record the tooling used to write and its own verify then rejected.
	refuses(t, "not one of them", "--root", root, "lease", "take", "--resource", "gpu",
		"--job", "second-job", "--unit", "2", "--ttl", "1h")
	refuses(t, "lies under", "--root", root, "handoff", "--job", "second-job", "--status", "partial",
		"--next", "carry on", "--started", "2026-10-09T05:00:00Z", "--artifact", "/etc/hostname")
	run(t, "--root", root, "job", "claim", "second-job") // nobody owns it, so anybody may
	refuses(t, "only its owner", "--root", root, "job", "claim", "second-job",
		"--owner", "somebody-else", "--actor", "somebody-else")

	// The baton and the status are one rule with two directions, and the tooling refuses
	// both rather than writing a record its own verify calls broken.
	refuses(t, "exactly when the status is `done`", "--root", root, "handoff", "--job", "second-job",
		"--status", "partial", "--next", "none", "--started", "2026-10-11T01:00:00Z")
	refuses(t, "leaves a baton", "--root", root, "handoff", "--job", "second-job",
		"--status", "aborted", "--started", "2026-10-11T02:00:00Z")

	// And the job can be closed, which is the end of it.
	run(t, "--root", root, "close", "--job", "second-job")
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should validate with everything in it, and said:\n%s", out)
	}

	// A job closes once, and a job that has closed is not worked. Both are refused before
	// anything is written, because a refusal that has already changed a record has changed
	// a record — so the brief and the working tree are what this checks.
	archived := filepath.Join(root, "doc", "archive", "task", "second-job", "brief.md")
	before, err := os.ReadFile(archived)
	if err != nil {
		t.Fatal(err)
	}
	refuses(t, "closed once", "--root", root, "close", "--job", "second-job", "--abandoned")
	refuses(t, "still meant to happen is worked", "--root", root, "handoff", "--job", "second-job",
		"--status", "partial", "--next", "still poking", "--started", "2026-10-11T00:00:00Z")
	after, err := os.ReadFile(archived)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("a refused close changed the archived brief:\n%s", after)
	}
	if out, err := exec.Command("git", "-C", root, "status", "--porcelain").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Errorf("a refused operation should leave nothing behind, and left:\n%s%v", out, err)
	}
}

// A workspace arrives ready to hand to an actor: the entry point and the skill it names
// are materialised, they are the tooling's rather than the project's, and a build state
// clean leaves them where they are.
func TestTheAgentSkeletonArrivesAndIsRestored(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")

	entry := filepath.Join(root, "AGENTS.md")
	skill := filepath.Join(root, ".agents", "skills", "workspace-actor", "SKILL.md")
	for _, path := range []string{entry, skill, filepath.Join(root, ".agents", "skills", "workspace-actor", "records.md")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("a fresh workspace should carry %s: %v", path, err)
		}
	}

	// It is unversioned skeleton, so the coordination repository tracks neither it nor
	// anything else outside doc/ and handoff/.
	tracked, err := exec.Command("git", "-C", root, "ls-files").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(tracked), "\n") {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "doc/") && !strings.HasPrefix(line, "handoff/") {
			t.Errorf("the coordination repository tracks %s", line)
		}
	}

	// It is ignored, which is the only thing keeping `clean` off it.
	if out := run(t, "--root", root, "clean"); strings.Contains(out, "AGENTS.md") {
		t.Errorf("clean would remove the entry point: %s", out)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("clean removed the entry point: %v", err)
	}

	// Losing it is a finding rather than a silent absence, and update puts it back.
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if out, err := try(t, "--root", root, "verify"); err == nil {
		t.Errorf("verify should report a missing entry point, and said:\n%s", out)
	} else if !strings.Contains(out, "AGENTS.md") {
		t.Errorf("the finding should name the file, and said:\n%s", out)
	}
	if out := run(t, "--root", root, "update"); !strings.Contains(out, "AGENTS.md") {
		t.Errorf("update should say what it restored, and said:\n%s", out)
	}
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should validate once the skeleton is back, and said:\n%s", out)
	}
}

// Actors reach each other by message, and a mailbox is a directory: on one machine this
// needs nothing running at all.
func TestActorsReachEachOther(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")
	run(t, "--root", root, "resource", "declare", "--kind", "worktree", "--name", "area", "--purpose", "An area")
	seedCode(t, root)
	run(t, "--root", root, "open", "--job", "first-job", "--label", "area", "--owner", "claude-sonnet")
	// A name exists by acting: the owner is written into the brief, but it is this that
	// makes it a name the tooling has heard from.
	run(t, "--root", root, "job", "occupy", "first-job", "--actor", "claude-sonnet")

	// The recipient exists because it has acted, which is the whole of what an actor
	// does to exist.
	out := run(t, "--root", root, "message", "send", "--to", "claude-sonnet", "--subject", "the timeout is explained",
		"--body", "It was the retry budget.", "--job", "first-job")
	if !strings.Contains(out, "delivered to claude-sonnet at build-01") {
		t.Errorf("sending said: %s", out)
	}

	// A name nobody has seen is refused rather than written into a mailbox nobody
	// reads: the roster is exactly the knowledge that makes the check possible.
	refuses(t, "no site has seen an actor named", "--root", root, "message", "send", "--to", "claud-sonnet", "--subject", "typo")

	// The recipient reads it, and the list shows an id and a subject as separate things.
	inbox := run(t, "--root", root, "message", "list", "--actor", "claude-sonnet")
	if !strings.Contains(inbox, "the timeout is explained") || !strings.Contains(inbox, "m-") {
		t.Errorf("the inbox should show an id and a subject:\n%s", inbox)
	}
	id := ""
	for _, line := range strings.Split(inbox, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && strings.HasPrefix(fields[0], "m-") {
			id = fields[0]
			break
		}
	}
	if id == "" {
		t.Fatalf("no message id in the inbox:\n%s", inbox)
	}

	// A message carries bytes, and they arrive with it.
	sample := filepath.Join(t.TempDir(), "sample.csv")
	if err := os.WriteFile(sample, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "--root", root, "message", "send", "--to", "claude-sonnet", "--subject", "the sample", "--file", sample)
	second := ""
	for _, line := range strings.Split(run(t, "--root", root, "message", "list", "--actor", "claude-sonnet", "--json"), "\n") {
		_ = line
	}
	list := run(t, "--root", root, "message", "list", "--actor", "claude-sonnet")
	for _, line := range strings.Split(list, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[len(fields)-1] == "sample" {
			second = fields[0]
		}
	}
	if second == "" {
		t.Fatalf("the second message is not in the inbox:\n%s", list)
	}
	outDir := t.TempDir()
	read := run(t, "--root", root, "message", "read", second, "--actor", "claude-sonnet", "--out", outDir)
	if !strings.Contains(read, "attachment sample.csv") {
		t.Errorf("reading should mention its attachment:\n%s", read)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "sample.csv"))
	if err != nil {
		t.Fatalf("the attachment was not saved: %v", err)
	}
	if string(got) != "a,b\n1,2\n" {
		t.Errorf("the attachment changed: %q", got)
	}

	// A reply is written by whoever received the message, and read by whoever sent it.
	run(t, "--root", root, "message", "reply", id, "--actor", "claude-sonnet", "--body", "Thanks.")
	replies := run(t, "--root", root, "message", "list")
	if !strings.Contains(replies, "re: the timeout is explained") {
		t.Errorf("the reply should arrive with the subject marked:\n%s", replies)
	}
	if !strings.Contains(run(t, "--root", root, "message", "list", "--json"), "\"reply_to\"") {
		t.Errorf("a reply carries the id it answers")
	}

	// A message cannot be retracted, so removing one is a deliberate local act.
	run(t, "--root", root, "message", "purge", id, "--actor", "claude-sonnet")
	if out := run(t, "--root", root, "message", "list", "--actor", "claude-sonnet"); strings.Contains(out, id) {
		t.Errorf("the purged message is still listed:\n%s", out)
	}
}

// A job that moves carries its records through git and its payload through a bundle.
func TestAJobMovesToAnotherSite(t *testing.T) {
	from := filepath.Join(t.TempDir(), "from")
	to := filepath.Join(t.TempDir(), "to")

	run(t, "create", from, "--site", "build-01")
	run(t, "--root", from, "resource", "declare", "--kind", "worktree", "--name", "uploader", "--purpose", "Uploader reliability work")
	seedCode(t, from)
	run(t, "--root", from, "open", "--job", "carried-work", "--label", "uploader")
	if err := os.WriteFile(filepath.Join(from, "job", "carried-work", "out", "result.txt"),
		[]byte("the result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "--root", from, "handoff", "--job", "carried-work", "--status", "partial",
		"--next", "keep going", "--started", "2026-10-09T02:10:00Z")

	bundle := filepath.Join(t.TempDir(), "carried-work.tar.gz")
	run(t, "--root", from, "bundle", "export", "--job", "carried-work", "--out", bundle, "--site", "build-01")

	// The receiving site, which has nothing yet.
	run(t, "create", to, "--site", "lab-02")

	// The payload alone is not enough: the records have to arrive first, and the
	// refusal says which one is missing.
	refuses(t, "brief", "--root", to, "bundle", "import", bundle)

	// The records travel by git, so they are copied and committed here.
	copyTree(t, filepath.Join(from, "doc", "task"), filepath.Join(to, "doc", "task"))
	copyTree(t, filepath.Join(from, "handoff"), filepath.Join(to, "handoff"))
	git(t, to, "add", "-A", "--", "doc", "handoff")
	git(t, to, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "handoff carried-work: arrived")

	// With the records but without the branch, the commit the handoff names does not
	// resolve, and that is refused too rather than imported blind.
	refuses(t, "does not resolve", "--root", to, "bundle", "import", bundle)

	// The branch travels too, and then the payload lands.
	git(t, to+"/.bare", "fetch", "--quiet", from+"/.bare", "refs/heads/*:refs/heads/*")
	if out := run(t, "--root", to, "bundle", "import", bundle); !strings.Contains(out, "build-01") {
		t.Errorf("an import should say where the payload came from: %s", out)
	}
	got, err := os.ReadFile(filepath.Join(to, "job", "carried-work", "out", "result.txt"))
	if err != nil {
		t.Fatalf("the payload did not arrive: %v", err)
	}
	if string(got) != "the result\n" {
		t.Errorf("the payload changed on the way: %q", got)
	}
	// The scratch never travels.
	if _, err := os.Stat(filepath.Join(to, "job", "carried-work", "tmp")); err == nil {
		t.Error("tmp never travels")
	}
	// A payload is disposable, so replacing it is allowed and every discarded path is
	// reported.
	refuses(t, "already here", "--root", to, "bundle", "import", bundle)
	if out := run(t, "--root", to, "bundle", "import", bundle, "--replace"); !strings.Contains(out, "discarded") {
		t.Errorf("a replacement reports every path it discards:\n%s", out)
	}

	// Import commits nothing, and both sites still validate.
	if out := run(t, "--root", to, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the receiving site should validate, and said:\n%s", out)
	}
	if out := run(t, "--root", from, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the sending site should validate, and said:\n%s", out)
	}
}

// An event whose paths are in the way refuses before it writes anything. A refusal that
// comes after the write leaves half an event behind — a file on disk and in the index that
// never reaches the line, which validation does not see and sync does not carry — and it
// then blocks the very remedy it recommends.
func TestAnEventRefusesBeforeItWritesAnything(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")

	hand := filepath.Join(root, "doc", "convention", "01-hand.md")
	if err := os.WriteFile(hand, []byte("# Hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "doc/convention/01-hand.md")

	out, err := try(t, "--root", root, "resource", "declare", "--kind", "host", "--name", "gpu", "--purpose", "p")
	if err == nil {
		t.Fatalf("the declaration should have been refused, and said:\n%s", out)
	}
	if !strings.Contains(out, "doc/convention/01-hand.md") {
		t.Errorf("the refusal should name what is in the way:\n%s", out)
	}
	if strings.Contains(out, "already declares") {
		t.Errorf("it should have refused before writing, not after:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "doc", "resource", "host.yaml")); err == nil {
		t.Error("a refused event must not have written anything")
	}
	if staged := strings.TrimSpace(gitOut(t, root, "diff", "--cached", "--name-only")); staged != "doc/convention/01-hand.md" {
		t.Errorf("the index should be as it was found, and holds:\n%s", staged)
	}

	// The remedy the refusal recommends has to be one the actor can actually take.
	run(t, "--root", root, "commit", "--summary", "hand", "--", "doc/convention/01-hand.md")
	run(t, "--root", root, "resource", "declare", "--kind", "host", "--name", "gpu", "--purpose", "p")
	if _, err := os.Stat(filepath.Join(root, "doc", "resource", "host.yaml")); err != nil {
		t.Errorf("the declaration should go through once the way is clear: %v", err)
	}
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should validate, and said:\n%s", out)
	}
}

// Opening writes a brief and materialises a working directory; closing moves a brief into
// the archive and lets the runtime go. Both refuse before any of that: a job that exists on
// one machine and nowhere in the line is a job nobody else can see, and for a close there is
// no second attempt, because a job closes once.
func TestOpeningAndClosingRefuseBeforeTheyWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")
	run(t, "--root", root, "resource", "declare", "--kind", "worktree", "--name", "area", "--purpose", "an area")
	run(t, "--root", root, "resource", "declare", "--kind", "worktree", "--name", "area2", "--purpose", "another area")
	seedCode(t, root)

	hand := filepath.Join(root, "doc", "convention", "01-hand.md")
	if err := os.WriteFile(hand, []byte("# Hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "doc/convention/01-hand.md")

	refuses(t, "doc/convention/01-hand.md", "--root", root, "open", "--job", "j2", "--label", "area2")
	for _, path := range []string{"doc/task/j2", "job/j2", "worktree/area2"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err == nil {
			t.Errorf("a refused open should not have created %s", path)
		}
	}

	// The remedy works, and so does the operation after it.
	run(t, "--root", root, "commit", "--summary", "hand", "--", "doc/convention/01-hand.md")
	run(t, "--root", root, "open", "--job", "j2", "--label", "area2", "--owner", "fixture-actor")

	// Closing: nothing is written before the refusal is known, and no retry is possible
	// afterwards, so this is the refusal that matters most.
	if err := os.WriteFile(hand, []byte("# Hand\n\nMore.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "doc/convention/01-hand.md")
	refuses(t, "doc/convention/01-hand.md", "--root", root, "close", "--job", "j2")
	if _, err := os.Stat(filepath.Join(root, "doc", "task", "j2", "brief.md")); err != nil {
		t.Errorf("a refused close should leave the brief where it was: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "doc", "archive", "task", "j2")); err == nil {
		t.Error("a refused close should not have archived the brief")
	}
	brief, err := os.ReadFile(filepath.Join(root, "doc", "task", "j2", "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(brief), "status: active") {
		t.Errorf("a refused close should leave the status alone:\n%s", brief)
	}
	out := run(t, "--root", root, "status")
	if !strings.Contains(out, "j2") {
		t.Errorf("the job should still be on the board:\n%s", out)
	}

	// Clear the way, and the close happens — and only then does the working directory let go.
	run(t, "--root", root, "commit", "--summary", "hand again", "--", "doc/convention/01-hand.md")
	run(t, "--root", root, "close", "--job", "j2")
	if _, err := os.Stat(filepath.Join(root, "doc", "archive", "task", "j2", "brief.md")); err != nil {
		t.Errorf("the close should have archived the brief: %v", err)
	}
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should validate, and said:\n%s", out)
	}
}

// Records that cannot go into one line are refused with the file named and the way out in
// the message, and the way out keeps what the record wrote.
func TestRecordsThatDoNotGoTogetherSayTheWayOut(t *testing.T) {
	first := filepath.Join(t.TempDir(), "build-01")
	second := filepath.Join(t.TempDir(), "lab-02")
	records := filepath.Join(t.TempDir(), "records.git")

	run(t, "create", first, "--site", "build-01")
	git(t, first, "init", "--bare", "-b", "main", records)
	git(t, first, "remote", "add", "origin", records)
	run(t, "--root", first, "resource", "declare", "--kind", "site", "--name", "build-01", "--purpose", "build")
	run(t, "--root", first, "resource", "declare", "--kind", "site", "--name", "lab-02", "--purpose", "lab")
	run(t, "--root", first, "sync")
	run(t, "join", second, "--remote", records, "--site", "lab-02")

	// Both sites declare into the same registry while they are apart.
	git(t, second, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "not-there"))
	run(t, "--root", second, "resource", "declare", "--kind", "gpu", "--name", "gpu", "--purpose", "the GPU, as lab-02 sees it")
	git(t, second, "remote", "set-url", "origin", records)
	run(t, "--root", first, "resource", "declare", "--kind", "gpu", "--name", "other", "--purpose", "the GPU, as build-01 sees it")
	run(t, "--root", first, "sync")

	out, err := try(t, "--root", second, "sync")
	if err == nil {
		t.Fatalf("records that cannot go together should be refused, and it said:\n%s", out)
	}
	// The refusal describes the way out; the command that takes it is in tool/README.md,
	// where a command belongs. What it must not do is leave the reader without one.
	for _, want := range []string{"doc/resource/gpu.yaml", "taken back out", "working tree", "reset --hard"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal should mention %q, and says:\n%s", want, out)
		}
	}
	// The record is still local, and still a record.
	if unpublished := strings.TrimSpace(gitOut(t, second, "rev-list", "--count", "origin/main..HEAD")); unpublished != "1" {
		t.Errorf("the unpublished record should still be here, and there are %s", unpublished)
	}

	// The way out: out of the line, and what it wrote still in the tree. It also says what
	// the line has and the working tree does not, which is the content that recording the
	// file as it stands would drop.
	out = run(t, "--root", second, "sync", "--withdraw")
	if !strings.Contains(out, "took 1 record") {
		t.Errorf("withdrawing should say what it took back:\n%s", out)
	}
	if !strings.Contains(out, "the line has this in doc/resource/gpu.yaml") || !strings.Contains(out, "name: other") {
		t.Errorf("withdrawing should show what the line has and the tree does not:\n%s", out)
	}
	if unpublished := strings.TrimSpace(gitOut(t, second, "rev-list", "--count", "origin/main..HEAD")); unpublished != "0" {
		t.Errorf("nothing should be left unpublished, and there are %s", unpublished)
	}
	registry := filepath.Join(second, "doc", "resource", "gpu.yaml")
	kept, err := os.ReadFile(registry)
	if err != nil {
		t.Fatalf("the record's file should still be there: %v", err)
	}
	if !strings.Contains(string(kept), "as lab-02 sees it") {
		t.Errorf("and it should still say what the site recorded:\n%s", kept)
	}

	// Put the file right, record the event again, and it goes in.
	if err := os.WriteFile(registry, []byte("gpu:\n  - name: gpu\n    purpose: the GPU, as lab-02 sees it\n  - name: other\n    purpose: the GPU, as build-01 sees it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "--root", second, "commit", "--summary", "both declarations", "--", "doc/resource/gpu.yaml")
	run(t, "--root", second, "sync")
	line := gitOut(t, second, "log", "--pretty=%s", "origin/main")
	if !strings.Contains(line, "both declarations") {
		t.Errorf("the corrected record should be in the line:\n%s", line)
	}
	if merges := gitOut(t, second, "rev-list", "--merges", "--all"); strings.TrimSpace(merges) != "" {
		t.Errorf("and nothing was merged:\n%s", merges)
	}
	if out := run(t, "--root", second, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the site should validate, and said:\n%s", out)
	}
}

// An event that cannot go into the line is taken back out of it rather than left in it: the
// design says published or withdrawn, never kept, and a site left holding a commit that will
// never be in the line is a record whose files say one thing and whose line says another.
// A backlog of its own is how that is reached — the push is rejected, and the event cannot
// simply be replayed on top, because the site has more than one record above the line.
func TestAnEventThatCannotBePublishedIsTakenBackOut(t *testing.T) {
	first := filepath.Join(t.TempDir(), "build-01")
	second := filepath.Join(t.TempDir(), "lab-02")
	records := filepath.Join(t.TempDir(), "records.git")

	run(t, "create", first, "--site", "build-01")
	git(t, first, "init", "--bare", "-b", "main", records)
	git(t, first, "remote", "add", "origin", records)
	run(t, "--root", first, "resource", "declare", "--kind", "site", "--name", "build-01", "--purpose", "build")
	run(t, "--root", first, "resource", "declare", "--kind", "site", "--name", "lab-02", "--purpose", "lab")
	run(t, "--root", first, "sync")
	run(t, "join", second, "--remote", records, "--site", "lab-02")

	// Alone, so this record is a backlog the line does not have.
	git(t, second, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "not-there"))
	run(t, "--root", second, "resource", "declare", "--kind", "gpu", "--name", "gpu", "--purpose", "the GPU")
	git(t, second, "remote", "set-url", "origin", records)

	// The line moves while the backlog is still here, and then it moves *again* between the
	// next event being written and its push — which is what makes that event impossible to
	// publish: it is not one record above the line, it is the backlog plus this one. The
	// second move comes from a one-shot pre-push hook, because the window is a push wide.
	run(t, "--root", first, "resource", "declare", "--kind", "dataset", "--name", "corpus", "--purpose", "the corpus")
	run(t, "--root", first, "sync")

	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	git(t, filepath.Dir(elsewhere), "clone", "--quiet", records, elsewhere)
	if err := os.WriteFile(filepath.Join(elsewhere, "note.md"), []byte("somebody else's record\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, elsewhere, "add", "note.md")
	git(t, elsewhere, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "somebody else's record")
	hook := filepath.Join(second, ".git", "hooks", "pre-push")
	script := "#!/bin/sh\n" +
		"git -C " + elsewhere + " push --quiet " + records + " HEAD:refs/heads/main\n" +
		"rm -f " + hook + "\n" +
		"exit 1\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	refuses(t, "taken back out", "--root", second, "resource", "declare", "--kind", "host", "--name", "h9", "--purpose", "the machine")

	// One commit above the line, and it is the backlog; the refused event is not in the
	// line, and what it wrote is left in the working tree.
	if above := strings.TrimSpace(gitOut(t, second, "rev-list", "--count", "origin/main..HEAD")); above != "1" {
		t.Errorf("only the backlog should be above the line, and %s commits are", above)
	}
	if subjects := gitOut(t, second, "log", "--pretty=%s", "origin/main..HEAD"); !strings.Contains(subjects, "declared gpu") {
		t.Errorf("the backlog should still be here:\n%s", subjects)
	}
	if dirty := gitOut(t, second, "status", "--porcelain", "--", "doc/resource/host.yaml"); strings.TrimSpace(dirty) == "" {
		t.Errorf("what the refused event wrote should be left in the working tree")
	}

	// And the backlog is still publishable, which is the point of leaving it alone.
	if out := run(t, "--root", second, "sync"); !strings.Contains(out, "published") {
		t.Errorf("the backlog should publish, and sync said:\n%s", out)
	}
	if out := run(t, "--root", second, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the site should validate afterwards, and said:\n%s", out)
	}
}

// A site that recorded while it was alone has records the line does not, and sync is the
// command that publishes them: it brings the site up to date first and puts the backlog on
// top, rather than refusing because the line moved meanwhile.
func TestASiteThatRecordedAlonePublishesItsBacklog(t *testing.T) {
	first := filepath.Join(t.TempDir(), "build-01")
	second := filepath.Join(t.TempDir(), "lab-02")
	records := filepath.Join(t.TempDir(), "records.git")

	run(t, "create", first, "--site", "build-01")
	git(t, first, "init", "--bare", "-b", "main", records)
	git(t, first, "remote", "add", "origin", records)
	run(t, "--root", first, "resource", "declare", "--kind", "site", "--name", "build-01", "--purpose", "build")
	run(t, "--root", first, "resource", "declare", "--kind", "site", "--name", "lab-02", "--purpose", "lab")
	run(t, "--root", first, "sync")
	run(t, "join", second, "--remote", records, "--site", "lab-02")

	// The second site is alone for a while: the remote is not there, and it records
	// anyway. That is what a backlog is.
	git(t, second, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "not-there"))
	run(t, "--root", second, "resource", "declare", "--kind", "gpu", "--name", "gpu", "--purpose", "the GPU")
	git(t, second, "remote", "set-url", "origin", records)

	// Meanwhile the line moves, so publishing means going on top of what moved.
	run(t, "--root", first, "resource", "declare", "--kind", "dataset", "--name", "corpus", "--purpose", "the corpus")
	run(t, "--root", first, "sync")

	out := run(t, "--root", second, "sync")
	if !strings.Contains(out, "published") {
		t.Errorf("sync should publish the backlog, and said:\n%s", out)
	}
	line := gitOut(t, second, "log", "--pretty=%s", "origin/main")
	for _, want := range []string{"declared gpu", "declared corpus"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line should hold %q:\n%s", want, line)
		}
	}
	if merges := gitOut(t, second, "rev-list", "--merges", "--all"); strings.TrimSpace(merges) != "" {
		t.Errorf("publishing a backlog must not merge:\n%s", merges)
	}
	if out := run(t, "--root", second, "sync"); !strings.Contains(out, "nothing to publish") {
		t.Errorf("a second sync has nothing to do, and said:\n%s", out)
	}
	// Withdrawing always does what it says, and says when it was not needed: a record that
	// would have gone in cleanly comes out anyway, and has to be recorded again.
	git(t, second, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "not-there"))
	run(t, "--root", second, "resource", "declare", "--kind", "host", "--name", "h9", "--purpose", "alone once more")
	git(t, second, "remote", "set-url", "origin", records)
	out = run(t, "--root", second, "sync", "--withdraw")
	if !strings.Contains(out, "would have published them as they are") {
		t.Errorf("withdrawing records that would have gone in cleanly should say so:\n%s", out)
	}
	if unpublished := strings.TrimSpace(gitOut(t, second, "rev-list", "--count", "origin/main..HEAD")); unpublished != "0" {
		t.Errorf("and it should still have taken them out, and %s are left unpublished", unpublished)
	}

	if out := run(t, "--root", second, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the site should still validate, and said:\n%s", out)
	}
}

// Two actors recording at once on one machine is a race the tooling settles rather than
// reports: both events land, each holds exactly its own file, and the refusal an actor
// sees is the tooling's rather than git's index lock.
func TestTwoActorsRecordingAtOnceBothLand(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")
	topic := filepath.Join(root, "doc", "topic", "race")
	if err := os.MkdirAll(topic, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"overview.md": "# Race\n\nTwo writers, one topic.\n",
		"one.md":      "# One\n\nstart\n",
		"two.md":      "# Two\n\nstart\n",
	} {
		if err := os.WriteFile(filepath.Join(topic, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, root, "add", "-A", "--", "doc/topic/race")
	git(t, root, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "topic race: start")

	// Each writer changes its own file, so neither event has anything to do with the
	// other except the moment they are recorded.
	writers := []struct {
		args []string
		body string
	}{
		{[]string{"--root", root, "commit", "--summary", "one", "--", "doc/topic/race/one.md"}, "# One\n\nwritten\n"},
		{[]string{"--root", root, "commit", "--summary", "two", "--", "doc/topic/race/two.md"}, "# Two\n\nwritten\n"},
	}
	for _, w := range writers {
		path := filepath.Join(root, filepath.FromSlash(w.args[len(w.args)-1]))
		if err := os.WriteFile(path, []byte(w.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	outputs := make([]string, len(writers))
	errs := make([]error, len(writers))
	done := make(chan int)
	for i, w := range writers {
		go func(i int, args []string) {
			cmd := exec.Command(thrum, args...)
			cmd.Env = append(os.Environ(), "THRUM_ACTOR=fixture-actor")
			out, err := cmd.CombinedOutput()
			outputs[i], errs[i] = string(out), err
			done <- i
		}(i, w.args)
	}
	for range writers {
		<-done
	}

	for i := range writers {
		if errs[i] != nil {
			t.Errorf("both writers should have recorded, and one said:\n%s", outputs[i])
		}
		if strings.Contains(outputs[i], "index.lock") {
			t.Errorf("a refusal should be the tooling's, not git's:\n%s", outputs[i])
		}
	}
	// Each event is one commit holding exactly its own file, and nothing is left over.
	for _, name := range []string{"one", "two"} {
		id := strings.TrimSpace(gitOut(t, root, "log", "--pretty=%H", "--grep", "^topic race: "+name+"$", "-1"))
		if id == "" {
			t.Errorf("the event %q never reached the line", name)
			continue
		}
		files := strings.Fields(gitOut(t, root, "show", "--pretty=format:", "--name-only", id))
		want := "doc/topic/race/" + name + ".md"
		if len(files) != 1 || files[0] != want {
			t.Errorf("event %q should hold exactly %s, and holds %v", name, want, files)
		}
	}
	if left := strings.TrimSpace(gitOut(t, root, "status", "--porcelain")); left != "" {
		t.Errorf("nothing should be left uncommitted:\n%s", left)
	}
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should still validate, and said:\n%s", out)
	}
}

// The convenience layer composes commands rather than events: one call gets an actor into a
// working directory, and what it recorded is what the operations recorded, each as its own
// commit.
func TestTakingUpAJobInOneStep(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project.git")
	root := filepath.Join(t.TempDir(), "workspace")
	clone := filepath.Join(t.TempDir(), "clone")

	// A project with a history, which is what a site joins rather than inventing.
	git(t, t.TempDir(), "init", "--bare", "-b", "main", project)
	git(t, t.TempDir(), "clone", "--quiet", project, clone)
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "add", "README.md")
	git(t, clone, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "project: first commit")
	git(t, clone, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	// A workspace that starts from that project, so nothing has to be pushed by hand.
	run(t, "create", root, "--site", "build-01", "--code", project)
	if out := gitOut(t, filepath.Join(root, ".bare"), "log", "--oneline", "main"); !strings.Contains(out, "project: first commit") {
		t.Errorf("the code repository should have come from the project:\n%s", out)
	}

	// One command: the label declared, the job opened, claimed, occupied, and the directory
	// printed.
	out := run(t, "--root", root, "work", "--job", "j1", "--label", "area",
		"--purpose", "an area", "--goal", "do the thing", "--scope", "only the thing",
		"--criteria", "the thing works")
	if !strings.Contains(out, "work in "+filepath.Join(root, "worktree", "area")) {
		t.Errorf("work should end by naming the directory:\n%s", out)
	}
	if !strings.Contains(out, `eval "$(thrum env --job j1)"`) {
		t.Errorf("work should hand over the environment line:\n%s", out)
	}

	// Each step is its own event, and nothing else was recorded.
	subjects := gitOut(t, root, "log", "--pretty=%s")
	for _, want := range []string{"resource:", "brief j1:"} {
		if !strings.Contains(subjects, want) {
			t.Errorf("the records should hold %q:\n%s", want, subjects)
		}
	}

	// Asking again finds everything where it already is, and records nothing new.
	before := gitOut(t, root, "rev-list", "--count", "HEAD")
	if out := run(t, "--root", root, "work", "--job", "j1"); !strings.Contains(out, "work in ") {
		t.Errorf("asking a second time should land in the same place:\n%s", out)
	}
	if after := gitOut(t, root, "rev-list", "--count", "HEAD"); after != before {
		t.Errorf("asking a second time should record nothing, and commits went from %s to %s", before, after)
	}

	// The job's picture, for a person and for whatever asked.
	if out := run(t, "--root", root, "job", "show", "j1"); !strings.Contains(out, "do the thing") || !strings.Contains(out, "work in") {
		t.Errorf("the job view should say what it is for and where it is:\n%s", out)
	}
	if out := run(t, "--root", root, "job", "show", "j1", "--json"); !strings.Contains(out, `"goal": "do the thing"`) {
		t.Errorf("the JSON view should carry the same answer:\n%s", out)
	}

	// Where, for a script: the directory and nothing else.
	where := strings.TrimSpace(run(t, "--root", root, "where", "--job", "j1"))
	if where != filepath.Join(root, "worktree", "area") {
		t.Errorf("where should print the directory alone, and printed %q", where)
	}

	// The same actor asking again is fine, and a different one is refused before anything
	// is written: one actor at a time is what the occupancy note is for.
	out, err := tryAs(t, "somebody-else", "--root", root, "work", "--job", "j1", "--label", "area")
	if err == nil {
		t.Fatalf("another actor should have been refused, and it said:\n%s", out)
	}
	if !strings.Contains(out, "occupied by fixture-actor") {
		t.Errorf("the refusal should say who is on it:\n%s", out)
	}

	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should validate, and said:\n%s", out)
	}
}

// The site's own half is written by the tooling, and none of it is committed.
func TestTheSitesOwnHalfIsWrittenRatherThanTyped(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")
	// A site that accepts peers runs a daemon, and a test that leaves one behind leaves a
	// process holding a temporary directory that is about to be removed.
	stopDaemon := func() { _, _ = try(t, "--root", root, "daemon", "stop") }
	t.Cleanup(stopDaemon)

	if out := run(t, "--root", root, "site", "show"); !strings.Contains(out, "accepts   nobody") {
		t.Errorf("a fresh site should accept nobody:\n%s", out)
	}
	run(t, "--root", root, "site", "listen", "--at", "unix:"+filepath.Join(root, "var", "peers.sock"))
	// The roster is what says which sites exist, so a reach to one nobody declares is
	// stale by design and reported as such.
	run(t, "--root", root, "resource", "declare", "--kind", "site", "--name", "lab-02", "--purpose", "the lab")
	run(t, "--root", root, "site", "reach", "lab-02", "lab-02.internal:22", "--ssh-user", "root")
	out := run(t, "--root", root, "site", "show")
	for _, want := range []string{"accepts   peers at unix:", "reaches   lab-02 at lab-02.internal:22"} {
		if !strings.Contains(out, want) {
			t.Errorf("the site view should say %q:\n%s", want, out)
		}
	}
	// The key is made once and printed as the line a peer authorises.
	first := strings.TrimSpace(run(t, "--root", root, "site", "key"))
	if !strings.HasPrefix(first, "ssh-ed25519 ") {
		t.Errorf("the key should be an authorized_keys line:\n%s", first)
	}
	if again := strings.TrimSpace(run(t, "--root", root, "site", "key")); again != first {
		t.Errorf("the key should be made once, and it changed:\n%s\n%s", first, again)
	}
	// Nothing under var/ is committed, and the workspace still validates.
	if out := gitOut(t, root, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Errorf("the site's own half must never be committed:\n%s", out)
	}
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should validate, and said:\n%s", out)
	}
}

// The command reference is documentation, and documentation drifts. This is the guard: every
// command the binary has is in the reference, and every command the reference names exists.
func TestTheCommandReferenceMatchesTheBinary(t *testing.T) {
	// The reference lives two directories up from this package.
	reference, err := os.ReadFile(filepath.Join("..", "..", "doc", "tooling.md"))
	if err != nil {
		t.Fatal(err)
	}
	section := string(reference)
	if i := strings.Index(section, "## Commands"); i >= 0 {
		section = section[i:]
	} else {
		t.Fatal("doc/tooling.md has no ## Commands section")
	}

	documented := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		name := strings.TrimPrefix(line, "| `")
		if j := strings.Index(name, "`"); j > 0 {
			documented[name[:j]] = true
		}
	}

	out := run(t, "--help")
	actual := map[string]bool{}
	inList := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "Available Commands:"):
			inList = true
			continue
		case inList && strings.TrimSpace(line) == "":
			inList = false
			continue
		case !inList:
			continue
		}
		name := strings.Fields(line)[0]
		switch name {
		case "completion", "help", "daemon":
			continue // completion and help are cobra's; daemon is documented as hidden
		}
		actual[name] = true
	}

	for name := range actual {
		if !documented[name] {
			t.Errorf("the binary has a command the reference does not: %s", name)
		}
	}
	for name := range documented {
		if !actual[name] {
			t.Errorf("the reference names a command the binary does not have: %s", name)
		}
	}
}

// A name a silent site might have is not a name nobody has. The first is a send failure that
// names the site that did not answer; only the second is about the name — and a name this site
// knows is delivered to without asking anyone, because the gate is the site that has it.
func TestASilentSiteIsNotNoSuchName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")
	run(t, "--root", root, "resource", "declare", "--kind", "site", "--name", "lab-02", "--purpose", "the lab")
	// This site's key, so that asking a peer fails because the peer is not there rather than
	// because this machine has no identity to dial with.
	run(t, "--root", root, "site", "key")

	// A peer with an address nothing is listening on: the dial fails at once, and locally.
	silent := "unix:" + filepath.Join(t.TempDir(), "nothing.sock")
	run(t, "--root", root, "site", "reach", "lab-02", silent, "--ssh-user", "root")

	out, err := tryAs(t, "sender", "--root", root, "message", "send", "--to", "nobody", "--subject", "hello", "--body", "hi")
	if err == nil {
		t.Fatalf("a send that cannot establish where the recipient is should fail, and it said:\n%s", out)
	}
	if !strings.Contains(out, "lab-02 did not answer") || !strings.Contains(out, "cannot be established") {
		t.Errorf("the send should name the site that did not answer:\n%s", out)
	}
	if !strings.Contains(out, "the send failed") {
		t.Errorf("it should say that the send failed:\n%s", out)
	}
	if strings.Contains(out, "no site has seen") {
		t.Errorf("a silent site must not be reported as nobody having the name:\n%s", out)
	}

	if out, err := tryAs(t, "sender", "--root", root, "actor", "show", "nobody"); err == nil || !strings.Contains(out, "cannot be established") {
		t.Errorf("asking about a name a silent site might have is not the answer that it does not exist:\n%s", out)
	}

	// Forgetting the address does not make the site stop existing: a site the roster declares
	// and this machine cannot ask is still a site that did not answer.
	run(t, "--root", root, "site", "reach", "lab-02", "--forget")
	if out, err := tryAs(t, "sender", "--root", root, "message", "send", "--to", "nobody", "--subject", "hello", "--body", "hi"); err == nil || !strings.Contains(out, "lab-02 did not answer") {
		t.Errorf("a declared site this machine has no address for cannot be asked, so the answer is not established:\n%s", out)
	}

	// With no other site in the world at all, the same name is simply unknown.
	run(t, "--root", root, "resource", "remove", "--kind", "site", "--name", "lab-02")
	refuses(t, "no site has seen", "--root", root, "message", "send", "--to", "nobody", "--subject", "hello", "--body", "hi")

	t.Cleanup(func() { _, _ = try(t, "--root", root, "daemon", "stop") })

	// And a name this site has seen goes into its own mailbox, whatever a peer is doing: the
	// site that has the name is the gate, not the sites that do not.
	run(t, "--root", root, "site", "reach", "lab-02", silent, "--ssh-user", "root")
	tryAs(t, "sender", "--root", root, "status")
	out = run(t, "--root", root, "message", "send", "--to", "sender", "--subject", "hello", "--body", "hi")
	if !strings.Contains(out, "sender") {
		t.Errorf("a name at this site should be delivered to here:\n%s", out)
	}
	if out := run(t, "--root", root, "message", "list", "--actor", "sender"); !strings.Contains(out, "hello") {
		t.Errorf("the message should be in the mailbox it was sent to:\n%s", out)
	}
}

// The whole picture is readable by a machine as well as by a person, names and reachability
// included: a harness should not have to parse the text.
func TestStatusIsReadableByAMachine(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")
	run(t, "--root", root, "resource", "declare", "--kind", "worktree", "--name", "area", "--purpose", "An area")
	seedCode(t, root)
	run(t, "--root", root, "open", "--job", "first-job", "--label", "area", "--owner", "fixture-actor")

	out := run(t, "--root", root, "status", "--json")
	var got struct {
		Report struct {
			Jobs []struct {
				ID    string `json:"id"`
				Owner string `json:"owner"`
			} `json:"jobs"`
			Worktrees []struct {
				Label string `json:"label"`
			} `json:"worktrees"`
		} `json:"report"`
		Actors []struct {
			Name string `json:"name"`
		} `json:"actors"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("status --json should be JSON: %v\n%s", err, out)
	}
	if len(got.Report.Jobs) != 1 || got.Report.Jobs[0].ID != "first-job" || got.Report.Jobs[0].Owner != "fixture-actor" {
		t.Errorf("the board should be in it: %+v", got.Report.Jobs)
	}
	if len(got.Report.Worktrees) != 1 || got.Report.Worktrees[0].Label != "area" {
		t.Errorf("the working directories should be in it: %+v", got.Report.Worktrees)
	}
	if len(got.Actors) == 0 {
		t.Error("the names should be in it, which is a question about the present and not a file")
	}

	// The keys are the shape a machine reads, so they are pinned here: the report, and the
	// two questions about the present that no report answers.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"report", "actors", "sites"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("status --json should carry %q, and carries %v", want, keys)
		}
	}
	if len(keys) != 3 {
		t.Errorf("and nothing else, because a stable shape is the point: %v", keys)
	}
}

// seedCode gives the code repository one commit, which is what a project has before any
// job exists. It is bare, so the commit is made in a clone and pushed.
func seedCode(t *testing.T, root string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", "--quiet", filepath.Join(root, ".bare"), clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "add", "README.md")
	git(t, clone, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "project: first commit")
	git(t, clone, "push", "--quiet", "origin", "HEAD:refs/heads/main")
}

// waitForFile waits for an actor's own output to appear, which is the only proof that
// something really ran.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
}

// gitOut runs git and returns what it said.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// copyTree copies a directory, which is how the versioned half would arrive by git.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		src := filepath.Join(from, entry.Name())
		dst := filepath.Join(to, entry.Name())
		if entry.IsDir() {
			copyTree(t, src, dst)
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A harness that has to be woken rather than to ask reads a mailbox as a stream. The
// watch prints what is already there — the only chance to see it, because nothing is
// re-delivered — and then one line per message as it arrives, each line one JSON object.
func TestAWatchingInboxReportsTheMailboxAndThenWhatArrives(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")

	// A name has to have been seen before anybody can be sent anything.
	run(t, "--root", root, "--actor", "alice", "message", "list")
	run(t, "--root", root, "--actor", "bob", "message", "list")
	run(t, "--root", root, "--actor", "bob", "message", "send", "--to", "alice",
		"--subject", "already in the mailbox", "--body", "…")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := exec.CommandContext(ctx, thrum, "--root", root, "message", "list", "--watch", "--json")
	watch.Env = append(os.Environ(), "THRUM_ACTOR=alice")
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = watch.Wait()
	}()

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	first := nextLine(t, lines, 5*time.Second)
	if !strings.Contains(first, "already in the mailbox") {
		t.Errorf("a watch should print the mailbox it finds, and printed: %s", first)
	}

	run(t, "--root", root, "--actor", "bob", "message", "send", "--to", "alice",
		"--subject", "sent while watching", "--body", "…")

	second := nextLine(t, lines, 10*time.Second)
	var got struct{ ID, From, Subject string }
	if err := json.Unmarshal([]byte(second), &got); err != nil {
		t.Fatalf("each line should be one JSON object: %v in %q", err, second)
	}
	if got.Subject != "sent while watching" || got.From != "bob" || got.ID == "" {
		t.Errorf("the stream should carry the message that arrived, and carried %+v", got)
	}
}

// The mailbox is created by the first message delivered into it, so a watch that starts
// before anybody has ever written to this actor has nothing to watch. It must still report
// that first message: it watches the way down to the mailbox and moves as it appears.
func TestAWatchingInboxReportsTheFirstMessageEverDelivered(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	run(t, "create", root, "--site", "build-01")
	run(t, "--root", root, "--actor", "alice", "message", "list")
	run(t, "--root", root, "--actor", "bob", "message", "list")
	if _, err := os.Stat(filepath.Join(root, "var", "msg")); err == nil {
		t.Fatal("nothing should have created a mailbox yet")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := exec.CommandContext(ctx, thrum, "--root", root, "message", "list", "--watch", "--json")
	watch.Env = append(os.Environ(), "THRUM_ACTOR=alice")
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = watch.Wait()
	}()

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	// Nobody has written anything, so the stream should be silent until somebody does.
	select {
	case line := <-lines:
		t.Fatalf("a mailbox nobody has written to should print nothing, and printed: %s", line)
	case <-time.After(time.Second):
	}

	run(t, "--root", root, "--actor", "bob", "message", "send", "--to", "alice",
		"--subject", "the first message ever", "--body", "…")

	line := nextLine(t, lines, 10*time.Second)
	var got struct{ ID, Subject string }
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("each line should be one JSON object: %v in %q", err, line)
	}
	if got.Subject != "the first message ever" || got.ID == "" {
		t.Errorf("the stream should carry the first message, and carried %+v", got)
	}
}

// nextLine reads one line from a watcher, or fails the test rather than hanging.
func nextLine(t *testing.T, lines <-chan string, within time.Duration) string {
	t.Helper()
	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatal("the watching inbox stopped")
		}
		return line
	case <-time.After(within):
		t.Fatal("the watching inbox said nothing in time")
		return ""
	}
}

// A claim left behind by an actor who stopped is stale when this site no longer has a
// directory for the job — and it must not block the job for everybody. Releasing the working
// directory does not clear the note, so this is met at the last step of taking a job up.
func TestAStaleClaimDoesNotBlockTakingAJobUp(t *testing.T) {
	root := filepath.Join(t.TempDir(), "build-01")
	run(t, "create", root, "--site", "build-01")

	clone := filepath.Join(t.TempDir(), "seed")
	git(t, filepath.Dir(clone), "clone", "--quiet", filepath.Join(root, ".bare"), clone)
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "add", ".")
	git(t, clone, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "project: first")
	git(t, clone, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	run(t, "--root", root, "--actor", "alice", "resource", "declare", "--kind", "worktree", "--name", "uploader", "--purpose", "the area")
	run(t, "--root", root, "--actor", "alice", "work", "--job", "j1", "--label", "uploader",
		"--goal", "g", "--scope", "s", "--criteria", "c", "--owned-paths", "src/**")
	// Alice stops, and the directory goes back to being idle: the note stays behind.
	run(t, "--root", root, "--actor", "alice", "handoff", "--job", "j1", "--status", "partial",
		"--next", "carry on", "--started", "2026-10-10T02:00:00Z")
	run(t, "--root", root, "--actor", "alice", "worktree", "release", "--label", "uploader")

	// Somebody else takes the job up, and the stale note does not stand in the way.
	out := run(t, "--root", root, "--actor", "bob", "work", "--job", "j1", "--label", "uploader")
	if !strings.Contains(out, "stale") || !strings.Contains(out, "work in") {
		t.Errorf("a stale claim should be taken over and said so, and it said:\n%s", out)
	}
	show := run(t, "--root", root, "status")
	if !strings.Contains(show, "bob") {
		t.Errorf("the claim should now be bob's, and occupancy says:\n%s", show)
	}
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should still validate, and said:\n%s", out)
	}
}

// A handoff's body is the actor's own: those four sections are what the next actor reads, and a
// template skeleton is not that. It is passed in the way a brief's body is, and checked before
// anything is recorded.
func TestAHandoffBodyIsTheActorsOwn(t *testing.T) {
	root := filepath.Join(t.TempDir(), "build-01")
	run(t, "create", root, "--site", "build-01")

	clone := filepath.Join(t.TempDir(), "seed")
	git(t, filepath.Dir(clone), "clone", "--quiet", filepath.Join(root, ".bare"), clone)
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "add", ".")
	git(t, clone, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "project: first")
	git(t, clone, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	run(t, "--root", root, "--actor", "alice", "resource", "declare", "--kind", "worktree", "--name", "area", "--purpose", "the area")
	run(t, "--root", root, "--actor", "alice", "work", "--job", "j1", "--label", "area",
		"--goal", "g", "--scope", "s", "--criteria", "c")

	work := filepath.Join(root, "worktree", "area")
	if err := os.WriteFile(filepath.Join(work, "changed.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "the change")

	// A body without the sections is refused before anything is recorded.
	if out, err := tryAs(t, "alice", "--root", root, "handoff", "--job", "j1", "--status", "partial",
		"--next", "carry on", "--body", "-"); err == nil {
		t.Errorf("a body with no sections should be refused, and it said:\n%s", out)
	}

	path := filepath.Join(t.TempDir(), "body.md")
	text := "## What changed\n\nThe uploader retries once.\n\n## What was verified\n\nThe test passes.\n\n" +
		"## What remains\n\nClose it.\n\n## Open questions\n\nNone.\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "--root", root, "--actor", "alice", "handoff", "--job", "j1", "--status", "partial",
		"--next", "carry on", "--body", path)

	dir := filepath.Join(root, "handoff", "j1")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("one stretch is one handoff, and there are %d", len(entries))
	}
	record, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "The uploader retries once.") {
		t.Errorf("the body should be in the record, and it is:\n%s", record)
	}
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the record should validate, and verify said:\n%s", out)
	}
}

// A job opened for somebody else is taken up by whoever takes it up: `--owner` is who the work is
// promised to, and the claim names the actor at the machine that holds the job.
func TestTakingUpAJobOpenedForSomebodyElse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "build-01")
	run(t, "create", root, "--site", "build-01")

	clone := filepath.Join(t.TempDir(), "seed")
	git(t, filepath.Dir(clone), "clone", "--quiet", filepath.Join(root, ".bare"), clone)
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "add", ".")
	git(t, clone, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "project: first")
	git(t, clone, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	run(t, "--root", root, "--actor", "alice", "resource", "declare", "--kind", "worktree",
		"--name", "area", "--purpose", "the area")
	out := run(t, "--root", root, "--actor", "alice", "work", "--job", "j1", "--label", "area",
		"--goal", "g", "--scope", "s", "--criteria", "c", "--owner", "carol")
	if !strings.Contains(out, "work in") {
		t.Errorf("taking up a job opened for somebody else should work, and it said:\n%s", out)
	}
	if show := run(t, "--root", root, "status"); !strings.Contains(show, "alice") {
		t.Errorf("the actor at the machine is on the job, and occupancy says:\n%s", show)
	}
	if j := run(t, "--root", root, "job", "show", "j1"); !strings.Contains(j, "carol") {
		t.Errorf("the owner stays who the work is promised to, and the job says:\n%s", j)
	}
}

// A site that cannot name itself cannot find its own half of a declaration, so a name that is not
// a slug is refused where it is given rather than reported by verify afterwards.
func TestASiteNameIsASlugLikeEveryOtherName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "build-01")
	if out, err := try(t, "create", root, "--site", "ArchX"); err == nil {
		t.Errorf("a site name that is not a slug should be refused, and it said:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "var", "site")); err == nil {
		t.Error("a refused create should not have written a site file")
	}
	run(t, "create", root, "--site", "build-01")
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("a workspace should be born coherent, and verify said:\n%s", out)
	}
}
