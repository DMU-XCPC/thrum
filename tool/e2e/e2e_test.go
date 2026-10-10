// Package e2e drives the workspace tool the way an actor does: through the command
// line, against a real workspace, over a whole job's life.
//
// Every other test in this repository checks one package's rules. This one checks that
// the rules still hold when they meet: that a workspace created by the tooling passes
// the tooling's own validation after every step, and that the records one operation
// writes are the records the next one reads.
package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ws is the built command, shared by every test in this package.
var ws string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ws-e2e-")
	if err != nil {
		panic(err)
	}
	ws = filepath.Join(dir, "ws")
	build := exec.Command("go", "build", "-o", ws, "./cmd/ws")
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
		t.Fatalf("ws %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// try runs the command and returns what it said, and whether it refused.
func try(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(ws, args...)
	cmd.Env = append(os.Environ(), "WS_ACTOR=fixture-actor")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// refuses checks that an operation is refused, and that the refusal says why.
func refuses(t *testing.T, want string, args ...string) {
	t.Helper()
	out, err := try(t, args...)
	if err == nil {
		t.Fatalf("ws %s should have been refused, and it said:\n%s", strings.Join(args, " "), out)
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
	run(t, "--root", root, "resource", "declare", "--name", "uploader",
		"--purpose", "Uploader reliability work", "--field", "owned-by=the platform team")
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
		"--owner", "fixture-actor", "--owned-paths", "src/upload/**")

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

	// And with no daemon, the running world is honestly reported as unknown.
	if out := run(t, "--root", root, "actor", "ask", "--job", "add-retry"); !strings.Contains(out, "nobody") {
		t.Errorf("with no daemon there is no live answer: %s", out)
	}
	// Starting an actor is what the daemon is for, and saying so beats doing half of it.
	refuses(t, "daemon", "--root", root, "daemon", "start", "--job", "add-retry", "--", "/bin/true")
}

// A job that moves carries its records through git and its payload through a bundle.
func TestAJobMovesToAnotherSite(t *testing.T) {
	from := filepath.Join(t.TempDir(), "from")
	to := filepath.Join(t.TempDir(), "to")

	run(t, "create", from, "--site", "build-01")
	run(t, "--root", from, "resource", "declare", "--name", "uploader", "--purpose", "Uploader reliability work")
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
