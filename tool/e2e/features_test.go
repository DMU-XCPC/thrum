package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateVerifyStatus(t *testing.T) {
	w := newWorkspace(t)
	w.r().run("verify").ok(t, "verify a fresh workspace")

	for _, rel := range []string{
		"doc/site/s1.yaml", "repos/code.git", "repos/store.git", "handoff/.git",
		"AGENTS.md", ".agents/skills/workspace-actor/SKILL.md",
		"doc/template/brief.md",
	} {
		w.mustExist(rel)
	}
	if !strings.Contains(w.file(".gitignore"), "/repos/") {
		t.Fatal(".gitignore does not ignore /repos/")
	}
	if strings.TrimSpace(w.file("var/site")) != "s1" {
		t.Fatalf("var/site is %q", w.file("var/site"))
	}
	if out := w.git("status", "--short"); strings.TrimSpace(out) != "" {
		t.Fatalf("the line is not clean after create:\n%s", out)
	}

	res := w.r().run("status", "--json").ok(t, "status --json")
	var st struct {
		Site  string `json:"site"`
		Repos []struct {
			Name         string `json:"name"`
			Materialised bool   `json:"materialised"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &st); err != nil {
		t.Fatalf("status json: %v\n%s", err, res.stdout)
	}
	if st.Site != "s1" {
		t.Fatalf("status site = %q", st.Site)
	}
	found := false
	for _, r := range st.Repos {
		if r.Name == "code" && r.Materialised {
			found = true
		}
	}
	if !found {
		t.Fatalf("status does not report code as materialised: %s", res.stdout)
	}
}

func TestActors(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent", "--about", "the tester").ok(t, "declare a1")
	r.run("actor", "declare", "h1", "--kind", "human").ok(t, "declare h1")
	r.run("actor", "declare", "x", "--kind", "robot").fails(t, "bad kind", "human or agent")
	r.run("actor", "declare", "a1", "--kind", "agent").fails(t, "declare twice", "already declared")

	res := r.run("actor", "list", "--json").ok(t, "actor list")
	var actors []struct {
		Actor string `json:"actor"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &actors); err != nil {
		t.Fatalf("actor list json: %v\n%s", err, res.stdout)
	}
	if len(actors) != 2 {
		t.Fatalf("want 2 actors, got %d: %s", len(actors), res.stdout)
	}
	if show := r.run("actor", "show", "a1").ok(t, "actor show"); !strings.Contains(show.stdout, "agent") {
		t.Fatalf("actor show does not report the kind: %s", show.stdout)
	}
	r.run("actor", "hello").ok(t, "actor hello")
	if place := w.file("var/location.yaml"); !strings.Contains(place, "a1") {
		t.Fatalf("actor hello did not record a place: %s", place)
	}
	r.run("actor", "retire", "h1").ok(t, "retire h1")
	w.mustExist("doc/archive/actor/h1.yaml")
	w.mustNotExist("doc/actor/h1.yaml")
	r.run("verify").ok(t, "verify after retire")
}

func TestRepos(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("repo", "add", "web", "--about", "the web", "--from", w.src).ok(t, "repo add web")
	r.run("repo", "add", "empty", "--about", "nothing yet").ok(t, "repo add empty")
	w.mustExist("repos/empty.git")

	res := r.run("repo", "list", "--json").ok(t, "repo list")
	var repos []struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &repos); err != nil {
		t.Fatalf("repo list json: %v\n%s", err, res.stdout)
	}
	if len(repos) != 3 {
		t.Fatalf("want three repositories, got %d: %s", len(repos), res.stdout)
	}
	if show := r.run("repo", "show", "web").ok(t, "repo show"); !strings.Contains(show.stdout, "the web") {
		t.Fatalf("repo show lost the description: %s", show.stdout)
	}

	r.run("repo", "remove", "web").ok(t, "repo remove")
	w.mustExist("repos/web.git")
	w.mustExist("doc/archive/repo/web.yaml")
	r.run("repo", "remove", "web").fails(t, "remove twice", "already retired")
	r.run("verify").ok(t, "verify with a retired repository")
}

func TestMessages(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("actor", "declare", "a2", "--kind", "human").ok(t, "declare a2")
	r.as("a1").run("actor", "hello").ok(t, "a1 hello")
	r.as("a2").run("actor", "hello").ok(t, "a2 hello")

	attachment := w.path("note.txt")
	if err := os.WriteFile(attachment, []byte("attached bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := w.path("body.txt")
	if err := os.WriteFile(body, []byte("the body is here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(r.as("a1").run("message", "send", "a2",
		"--subject", "hello a2", "--body", body, "--attach", attachment).ok(t, "send").stdout)
	if id == "" {
		t.Fatal("send printed no id")
	}

	listed := r.as("a2").run("message", "list", "--json").ok(t, "list")
	msgs := jsonLines[struct {
		ID      string `json:"id"`
		From    string `json:"from"`
		Subject string `json:"subject"`
	}](t, listed.stdout)
	if len(msgs) != 1 || msgs[0].ID != id || msgs[0].From != "a1" {
		t.Fatalf("mailbox: %s", listed.stdout)
	}

	outDir := t.TempDir()
	read := r.as("a2").run("message", "read", id, "--out", outDir).ok(t, "read")
	if !strings.Contains(read.stdout, "the body is here") {
		t.Fatalf("read lost the body: %s", read.stdout)
	}
	if got, err := os.ReadFile(filepath.Join(outDir, "note.txt")); err != nil || string(got) != "attached bytes" {
		t.Fatalf("attachment: %q, %v", got, err)
	}

	replyID := strings.TrimSpace(r.as("a2").withStdin("got it\n", "message", "reply", id,
		"--subject", "re: hello a2", "--body", "-").ok(t, "reply").stdout)
	if replyID == "" {
		t.Fatal("reply printed no id")
	}
	back := r.as("a1").run("message", "list", "--json").ok(t, "a1 list")
	if msgs := jsonLines[struct {
		ID      string `json:"id"`
		ReplyTo string `json:"reply_to"`
	}](t, back.stdout); len(msgs) != 1 || msgs[0].ReplyTo != id {
		t.Fatalf("reply: %s", back.stdout)
	}
	r.as("a1").run("message", "purge", replyID).ok(t, "purge")
	r.as("a1").run("message", "list").ok(t, "list after purge")

	r.as("a1").run("message", "send", "ghost", "--subject", "x").fails(t, "send to nobody", "no actor named")
	r.as("ghost").run("message", "send", "a2", "--subject", "x").fails(t, "send from nobody", "not declared")

	events := r.as("a2").run("events", "--json").ok(t, "events")
	evs := jsonLines[struct {
		Kind    string `json:"kind"`
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}](t, events.stdout)
	if len(evs) != 1 || evs[0].Kind != "message.delivered" || evs[0].Message.ID != id {
		t.Fatalf("events: %s", events.stdout)
	}
}

func TestAssets(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")

	artifact := w.path("artifact.bin")
	if err := os.WriteFile(artifact, []byte("the bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	ing := r.run("asset", "ingest", artifact).ok(t, "ingest")
	sum := sha256.Sum256([]byte("the bytes"))
	if !strings.Contains(ing.stdout, hex.EncodeToString(sum[:])) {
		t.Fatalf("ingest hash is wrong: %s", ing.stdout)
	}
	r.run("asset", "publish", "data", "2026-10-17", "--description", "a file").ok(t, "publish")
	r.run("verify").ok(t, "verify with a manifest")
	r.run("asset", "publish", "data", "2026-10-17").fails(t, "publish twice", "already published")

	r.run("asset", "materialise", "data", "2026-10-17", "--job", "j1").ok(t, "materialise")
	if got := w.file("job/j1/data/artifact.bin"); got != "the bytes" {
		t.Fatalf("materialised %q", got)
	}
	r.run("asset", "materialise", "data", "2026-10-17", "--job", "nope").fails(t, "materialise into nothing", "no job named")

	r.run("asset", "withdraw", "data", "2026-10-17", "--why", "wrong bytes").ok(t, "withdraw")
	w.mustExist("doc/archive/asset/data/2026-10-17.md")
	r.run("verify").ok(t, "verify after withdraw")
	if tag, err := gitOutput(w.path("repos", "store.git"), "tag"); err != nil || strings.TrimSpace(tag) != "" {
		t.Fatalf("the tag survived the withdrawal: %q %v", tag, err)
	}
	store := r.run("asset", "store").ok(t, "store")
	if !strings.Contains(store.stdout, "dangling") {
		t.Fatalf("the withdrawn bytes are not reported: %s", store.stdout)
	}
	r.run("asset", "prune").ok(t, "prune")
	if after := r.run("asset", "store").ok(t, "store after prune"); strings.TrimSpace(after.stdout) != "" {
		t.Fatalf("prune left objects: %s", after.stdout)
	}
}

func TestBundles(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	r.run("job", "handoff", "j1", "--status", "partial", "--next", "finish").ok(t, "handoff")
	w.write("job/j1/log/x.log", "the log")

	bundle := filepath.Join(t.TempDir(), "j1.tar.gz")
	r.run("bundle", "export", "j1", "--out", bundle).ok(t, "export")
	if err := os.RemoveAll(w.path("job", "j1")); err != nil {
		t.Fatal(err)
	}
	r.run("bundle", "import", bundle).ok(t, "import")
	if got := w.file("job/j1/log/x.log"); got != "the log" {
		t.Fatalf("imported %q", got)
	}
	r.run("bundle", "import", bundle).fails(t, "import over a payload", "already holds a payload")
	r.run("bundle", "import", bundle, "--replace").ok(t, "import --replace")

	r.run("job", "open", "j2", "--repo", "code", "--label", "two", "--goal", "g").ok(t, "open j2")
	r.run("bundle", "export", "j2", "--out", filepath.Join(t.TempDir(), "j2.tar.gz")).
		fails(t, "export without a handoff", "no handoff")
}

func TestCommitRetire(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	w.write("doc/topic/uploader/overview.md", "# Uploader\n\nHow it works.\n")
	w.write("doc/convention/01-naming.md", "# Naming\n\nLowercase.\n")

	r.run("commit", "--summary", "both at once").fails(t, "different events", "different events")
	r.run("commit", "--summary", "added the topic", "--", "doc/topic/uploader").ok(t, "commit topic")
	r.run("commit", "--summary", "added the convention", "--", "doc/convention/01-naming.md").ok(t, "commit convention")
	r.run("verify").ok(t, "verify after commits")

	r.run("retire", "topic/uploader", "--why", "moved", "--superseded-by", "topic/uploader-retry").ok(t, "retire")
	w.mustExist("doc/archive/topic/uploader/overview.md")
	w.mustNotExist("doc/topic/uploader/overview.md")
	notice := w.file("doc/archive/topic/uploader/overview.md")
	if !strings.Contains(notice, "> Retired") || !strings.Contains(notice, "Superseded by `topic/uploader-retry`") {
		t.Fatalf("the notice is wrong:\n%s", notice)
	}
	r.run("verify").ok(t, "verify after retire")
	r.run("retire", "topic/nope", "--why", "x").fails(t, "retire nothing", "no document")
	r.run("retire", "README.md", "--why", "x").fails(t, "retire a non-document", "not a document that retires")
}

func TestCleanUpdate(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	w.write("doc/stray.txt", "stray")
	w.write("var/keep.txt", "keep")
	r.run("clean").ok(t, "clean")
	w.mustNotExist("doc/stray.txt")
	w.mustExist("var/keep.txt")

	w.write("AGENTS.md", "tampered")
	r.run("update", "--dry-run").ok(t, "update --dry-run")
	if w.file("AGENTS.md") != "tampered" {
		t.Fatal("--dry-run wrote a file")
	}
	r.run("update").ok(t, "update")
	if w.file("AGENTS.md") == "tampered" {
		t.Fatal("update did not restore the skeleton")
	}

	ignore := w.file(".gitignore")
	w.write(".gitignore", strings.Replace(ignore, "/repos/\n", "", 1))
	r.run("update").ok(t, "update the ignore rules")
	if !strings.Contains(w.file(".gitignore"), "/repos/") {
		t.Fatal("update did not restore the ignore rule")
	}

	w.write("doc/template/brief.md", "changed")
	r.run("update").ok(t, "update reports a template diff")
	if w.file("doc/template/brief.md") != "changed" {
		t.Fatal("update wrote a copy-me file without --force")
	}
	r.run("update", "--force").ok(t, "update --force")
	if w.file("doc/template/brief.md") == "changed" {
		t.Fatal("--force did not write the template")
	}
}

func TestVerifyCatches(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")

	w.write("doc/actor/wrong.yaml", "actor: someone\nkind: agent\n")
	r.run("verify").fails(t, "actor file name", "actor does not equal its file name")
	if err := os.Remove(w.path("doc/actor/wrong.yaml")); err != nil {
		t.Fatal(err)
	}

	runGit(t, w.root, "init", "--bare", "repos/ghost.git")
	r.run("verify").fails(t, "undeclared repository", "no declaration for this repository")
	if err := os.RemoveAll(w.path("repos/ghost.git")); err != nil {
		t.Fatal(err)
	}

	w.write("doc/convention/01-a.md", "# A\n\none.\n")
	w.write("doc/convention/01-b.md", "# B\n\ntwo.\n")
	r.run("verify").fails(t, "duplicate convention number", "number 01 is also")
	if err := os.Remove(w.path("doc/convention/01-b.md")); err != nil {
		t.Fatal(err)
	}

	r.run("job", "open", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open")
	r.run("job", "handoff", "j1", "--status", "partial", "--next", "finish").ok(t, "handoff")
	paths, err := filepath.Glob(w.path("handoff", "j1", "*.md"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("handoff files: %v %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], []byte(strings.Replace(string(data), "status: partial", "status: done", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	r.run("verify").fails(t, "a done handoff with a baton", "a done stretch has next: none")

	if err := os.WriteFile(paths[0], data, 0o644); err != nil {
		t.Fatal(err)
	}
	r.run("verify").ok(t, "verify after repairing the records")
}

func TestAssetVersionIsTakenAcrossSites(t *testing.T) {
	ts := setupTwoSites(t)
	one := filepath.Join(t.TempDir(), "one.bin")
	if err := os.WriteFile(one, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts.w1.r().run("asset", "ingest", one).ok(t, "ingest at w1")
	ts.w1.r().run("asset", "publish", "cross", "2026-10-18", "--description", "one").ok(t, "publish at w1")
	ts.w1.r().run("sync").ok(t, "publish the version")
	ts.w2.r().run("sync").ok(t, "site two learns the version")

	two := filepath.Join(t.TempDir(), "two.bin")
	if err := os.WriteFile(two, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts.w2.r().run("asset", "ingest", two).ok(t, "ingest at w2")
	ts.w2.r().run("asset", "publish", "cross", "2026-10-18").
		fails(t, "the version is taken", "already published")
}

func TestBundleBeforeTheCodeIsRefused(t *testing.T) {
	ts := setupTwoSites(t)
	ts.w1.r().run("work", "j1", "--repo", "code", "--label", "up", "--goal", "g").ok(t, "open at w1")
	wt := ts.w1.path("worktree", "up")
	if err := os.WriteFile(filepath.Join(wt, "README"), []byte("a change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wt, "add", "-A")
	runGit(t, wt, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "work")
	ts.w1.r().run("job", "handoff", "j1", "--status", "partial", "--next", "finish").ok(t, "handoff")
	ts.w1.r().run("sync").ok(t, "publish")

	// Site two takes the histories but not the code: the head cannot resolve.
	runGit(t, ts.w2.path("repos", "code.git"), "remote", "remove", "origin")
	ts.w2.r().run("sync").ok(t, "site two syncs the histories only")
	bundle := filepath.Join(t.TempDir(), "j1.tgz")
	ts.w1.r().run("bundle", "export", "j1", "--out", bundle).ok(t, "export")
	ts.w2.r().run("bundle", "import", bundle).fails(t, "import before the code", "fetch the code first")
}

func TestWithdrawalCrossesTheHistories(t *testing.T) {
	ts := setupTwoSites(t)
	one := filepath.Join(t.TempDir(), "one.bin")
	if err := os.WriteFile(one, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts.w1.r().run("asset", "ingest", one).ok(t, "ingest")
	ts.w1.r().run("asset", "publish", "cross", "2026-10-18").ok(t, "publish")
	ts.w1.r().run("sync").ok(t, "publish the version")
	ts.w2.r().run("sync").ok(t, "site two learns the version")
	ts.w2.r().run("verify").ok(t, "site two verifies")

	ts.w1.r().run("asset", "withdraw", "cross", "2026-10-18", "--why", "wrong bytes").ok(t, "withdraw")
	ts.w1.r().run("sync").ok(t, "publish the withdrawal")
	ts.w2.r().run("sync").ok(t, "site two learns the withdrawal")
	if tags := runGit(t, ts.w1.path("repos", "store.git"), "tag"); strings.TrimSpace(tags) != "" {
		t.Fatalf("the withdrawn tag is still at site one: %s", tags)
	}
	ts.w2.mustExist("doc/archive/asset/cross/2026-10-18.md")
	ts.w2.r().run("verify").ok(t, "verify after the withdrawal")
}

type bundleEntry struct {
	name    string
	content string
}

func makeBundle(t *testing.T, path string, entries []bundleEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBundlePathTraversalIsRefused(t *testing.T) {
	w := newWorkspace(t)
	path := filepath.Join(t.TempDir(), "evil.tgz")
	makeBundle(t, path, []bundleEntry{
		{"bundle.yaml", "job: j1\nhandoff: 2026-01-01T00-00-00Z.md\n"},
		{"job/j1/../../escape.txt", "x"},
	})
	w.r().run("bundle", "import", path).fails(t, "a path outside the payload", "outside the payload")
	if _, err := os.Stat(filepath.Join(filepath.Dir(w.root), "escape.txt")); err == nil {
		t.Fatal("the import wrote outside the workspace")
	}
	if w.exists("escape.txt") {
		t.Fatal("the import wrote outside the job")
	}
}

func TestManyMessages(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	for i := 0; i < 100; i++ {
		r.as("a1").run("message", "send", "a2", "--subject", fmt.Sprintf("m%03d", i)).ok(t, "send")
	}
	msgs := jsonLines[struct {
		ID string `json:"id"`
	}](t, r.as("a2").run("message", "list", "--json").ok(t, "list").stdout)
	if len(msgs) != 100 {
		t.Fatalf("got %d messages, want 100", len(msgs))
	}
}

func TestLargeAttachment(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	big := make([]byte, 10<<20)
	for i := range big {
		big[i] = byte(i % 251)
	}
	f := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(f, big, 0o644); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(ts.w2.r().as("a2").run("message", "send", "a1",
		"--subject", "big", "--attach", f).ok(t, "send").stdout)
	out := t.TempDir()
	ts.w1.r().as("a1").run("message", "read", id, "--out", out).ok(t, "read")
	got, err := os.ReadFile(filepath.Join(out, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, big) {
		t.Fatal("the attachment's bytes changed")
	}
}

// TestAttachmentHardening: duplicate names, src:name, the 10 MB bound, metadata,
// and --out refusing to overwrite without --force.
func TestAttachmentHardening(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.as("a1").run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")

	dir := t.TempDir()
	one := filepath.Join(dir, "one.txt")
	if err := os.WriteFile(one, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	two := filepath.Join(sub, "one.txt")
	if err := os.WriteFile(two, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r.as("a1").run("message", "send", "a2", "--subject", "dup",
		"--attach", one, "--attach", two).fails(t, "duplicate names", "given twice")

	id := strings.TrimSpace(r.as("a1").run("message", "send", "a2", "--subject", "named",
		"--attach", one+":renamed.txt").ok(t, "send").stdout)
	out := r.as("a2").run("message", "read", id).ok(t, "read").stdout
	if !strings.Contains(out, "renamed.txt") {
		t.Fatalf("the attachment was not renamed: %s", out)
	}
	if !strings.Contains(out, "text/plain") {
		t.Fatalf("the MIME type is missing: %s", out)
	}
	list := r.as("a2").run("message", "list", "--json").ok(t, "list").stdout
	for _, want := range []string{`"sha256"`, `"mime"`} {
		if !strings.Contains(list, want) {
			t.Fatalf("the metadata %s is missing: %s", want, list)
		}
	}

	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	r.as("a2").run("message", "read", id, "--out", outDir).ok(t, "first out")
	r.as("a2").run("message", "read", id, "--out", outDir).fails(t, "existing out", "exists")
	r.as("a2").run("message", "read", id, "--out", outDir, "--force").ok(t, "forced out")
	if got, err := os.ReadFile(filepath.Join(outDir, "renamed.txt")); err != nil || string(got) != "one\n" {
		t.Fatalf("the attachment bytes: %q, %v", got, err)
	}

	big := filepath.Join(dir, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64((10 << 20) + 1)); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	r.as("a1").run("message", "send", "a2", "--subject", "big",
		"--attach", big).fails(t, "over the bound", "at most")
}
