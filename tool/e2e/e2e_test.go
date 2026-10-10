// Package e2e drives the workspace tool the way an actor does: through the command
// line, against a real workspace, over a whole job's life.
//
// Every other test in this repository checks one package's rules. This one checks that
// the rules still hold when they meet: that a workspace created by the tooling passes
// the tooling's own validation after every step, and that the records one operation
// writes are the records the next one reads.
package e2e_test

import (
	"crypto/sha256"
	"encoding/hex"
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
	if out := run(t, "--root", root, "actor", "ask", "--job", "add-retry"); !strings.Contains(out, "fixture-actor") {
		t.Errorf("opening with an owner should claim the job: %s", out)
	}

	// A label is held by its job, and a second job may not take it.
	refuses(t, "holds", "--root", root, "worktree", "add", "--label", "uploader", "--job", "another-job")

	// The claim, and the lease: the two things that say who is working, and what the
	// work holds.
	run(t, "--root", root, "occupancy", "occupy", "--job", "add-retry")
	refuses(t, "occupied", "--root", root, "occupancy", "occupy", "--job", "add-retry", "--actor", "somebody-else")
	run(t, "--root", root, "lease", "take", "--resource", "gpu", "--job", "add-retry", "--ttl", "1h")
	refuses(t, "held by", "--root", root, "lease", "take", "--resource", "gpu", "--job", "add-retry", "--ttl", "1h",
		"--actor", "somebody-else")

	// A stretch of work, recorded where the next actor will look for it.
	run(t, "--root", root, "handoff", "--job", "add-retry", "--status", "partial",
		"--next", "compare against the baseline", "--started", "2026-10-09T02:10:00Z",
		"--worktrees", "uploader", "--summary", "the flaky timeout remains")
	// The same start twice is a collision, not an edit.
	refuses(t, "already exists", "--root", root, "handoff", "--job", "add-retry", "--status", "partial",
		"--started", "2026-10-09T02:10:00Z")

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
	if out := run(t, "--root", root, "actor", "ask", "--job", "add-retry"); !strings.Contains(out, "nobody") && !strings.Contains(out, "claim") {
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

	// And the job can be closed, which is the end of it.
	run(t, "--root", root, "close", "--job", "second-job")
	if out := run(t, "--root", root, "verify"); strings.TrimSpace(out) != "ok" {
		t.Errorf("the workspace should validate with everything in it, and said:\n%s", out)
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
	run(t, "--root", root, "occupancy", "occupy", "--job", "first-job", "--actor", "claude-sonnet")

	// The recipient exists because it has acted, which is the whole of what an actor
	// does to exist.
	out := run(t, "--root", root, "send", "--to", "claude-sonnet", "--subject", "the timeout is explained",
		"--body", "It was the retry budget.", "--job", "first-job")
	if !strings.Contains(out, "delivered to claude-sonnet at build-01") {
		t.Errorf("sending said: %s", out)
	}

	// A name nobody has seen is refused rather than written into a mailbox nobody
	// reads: the roster is exactly the knowledge that makes the check possible.
	refuses(t, "no actor named", "--root", root, "send", "--to", "claud-sonnet", "--subject", "typo")

	// The recipient reads it, and the list shows an id and a subject as separate things.
	inbox := run(t, "--root", root, "inbox", "--actor", "claude-sonnet")
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
	run(t, "--root", root, "send", "--to", "claude-sonnet", "--subject", "the sample", "--file", sample)
	second := ""
	for _, line := range strings.Split(run(t, "--root", root, "inbox", "--actor", "claude-sonnet", "--json"), "\n") {
		_ = line
	}
	list := run(t, "--root", root, "inbox", "--actor", "claude-sonnet")
	for _, line := range strings.Split(list, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[len(fields)-1] == "sample" {
			second = fields[0]
		}
	}
	if second == "" {
		t.Fatalf("the second message is not in the inbox:\n%s", list)
	}
	outDir := t.TempDir()
	read := run(t, "--root", root, "read", second, "--actor", "claude-sonnet", "--out", outDir)
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
	run(t, "--root", root, "reply", id, "--actor", "claude-sonnet", "--body", "Thanks.")
	replies := run(t, "--root", root, "inbox")
	if !strings.Contains(replies, "re: the timeout is explained") {
		t.Errorf("the reply should arrive with the subject marked:\n%s", replies)
	}
	if !strings.Contains(run(t, "--root", root, "inbox", "--json"), "\"reply_to\"") {
		t.Errorf("a reply carries the id it answers")
	}

	// A message cannot be retracted, so removing one is a deliberate local act.
	run(t, "--root", root, "purge", id, "--actor", "claude-sonnet")
	if out := run(t, "--root", root, "inbox", "--actor", "claude-sonnet"); strings.Contains(out, id) {
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
