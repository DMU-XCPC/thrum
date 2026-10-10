package bundles_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/ws/tool/internal/bundles"
	"github.com/multiagentworkspace/ws/tool/internal/format"
	"github.com/multiagentworkspace/ws/tool/internal/gitx"
	"github.com/multiagentworkspace/ws/tool/internal/validate"
	"github.com/multiagentworkspace/ws/tool/internal/workspace"
)

// source is a workspace with a job that has been worked in and handed over, and a
// payload worth carrying.
func source(t *testing.T) (workspace.Layout, string) {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "build-01",
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := "worktree:\n  - name: uploader\n    purpose: Uploader reliability work\n    ephemeral: false\n"
	if err := os.WriteFile(l.ResourceKind("worktree"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCode(t, l)

	// The working directories belong to the code repository, not to the workspace root.
	if _, err := gitx.Run(l.CodeRepo(), "worktree", "add", "--quiet", l.Worktree("uploader"), "-b", "job/carried-work"); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: carried-work\nstatus: active\nbranch: job/carried-work\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.MkdirAll(l.TaskDir("carried-work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Brief("carried-work"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}

	// A payload: some of it travels and some of it does not.
	for sub, content := range map[string]string{
		"out":   "the result",
		"log":   "what happened",
		"data":  "an input",
		"tmp":   "scratch",
		"cache": "regenerable",
	} {
		dir := l.JobSub("carried-work", sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	handoff := "---\n" +
		"job: carried-work\n" +
		"agent: tester\n" +
		"branch: job/carried-work\n" +
		"base: " + headOf(t, l) + "\n" +
		"head: " + headOf(t, l) + "\n" +
		"status: partial\n" +
		"started: 2026-10-09T02:10:00Z\n" +
		"ended: 2026-10-09T03:00:00Z\n" +
		"next: keep going\n" +
		"---\n\n## What changed\n\n## What was verified\n\n## What remains\n\n## Open questions\n"
	if err := os.MkdirAll(l.HandoffDir("carried-work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.HandoffFile("carried-work", "2026-10-09T02-10-00Z.md"), []byte(handoff), 0o644); err != nil {
		t.Fatal(err)
	}
	return l, "carried-work"
}

func headOf(t *testing.T, l workspace.Layout) string {
	t.Helper()
	out, err := gitx.Run(l.CodeRepo(), "rev-parse", "job/carried-work")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func seedCode(t *testing.T, l workspace.Layout) {
	t.Helper()
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
}

func TestABundleCarriesThePayloadAndNotTheScratch(t *testing.T) {
	l, job := source(t)
	out := filepath.Join(t.TempDir(), "carried-work.tar.gz")

	path, err := bundles.Export(l, bundles.ExportOptions{Job: job, Out: out, Site: "build-01"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	names, header := readBundle(t, path)
	if header.Job != job || header.Handoff != "2026-10-09T02-10-00Z.md" || header.Site != "build-01" {
		t.Errorf("header: %+v", header)
	}
	if names[0] != bundles.HeaderFile {
		t.Errorf("the header has to come first, so an import can refuse before unpacking; got %q", names[0])
	}
	for _, want := range []string{"job/carried-work/out/file", "job/carried-work/log/file", "job/carried-work/data/file"} {
		if !contains(names, want) {
			t.Errorf("%s should travel", want)
		}
	}
	for _, unwanted := range []string{"job/carried-work/tmp/file", "job/carried-work/cache/file"} {
		if contains(names, unwanted) {
			t.Errorf("%s should never travel: it exists only to be thrown away", unwanted)
		}
	}
}

func TestExportRefusesDuringWorkAndInsideThePayload(t *testing.T) {
	l, _ := source(t)

	// A job with no handoff has nothing to export: a bundle belongs to one.
	brief := "---\njob: unstarted\nstatus: active\nbranch: job/unstarted\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.MkdirAll(l.TaskDir("unstarted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Brief("unstarted"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := bundles.Export(l, bundles.ExportOptions{Job: "unstarted", Out: filepath.Join(t.TempDir(), "x.tar.gz")}); err == nil {
		t.Error("exporting a job that has not been handed over should be refused")
	}

	// A bundle that lived inside the payload would contain itself.
	inside := l.JobSub("carried-work", "out") + "/self.tar.gz"
	if _, err := bundles.Export(l, bundles.ExportOptions{Job: "carried-work", Out: inside}); err == nil {
		t.Error("writing a bundle inside the payload should be refused")
	}
}

func TestImportNeedsTheRecordsFirst(t *testing.T) {
	l, job := source(t)
	out := filepath.Join(t.TempDir(), "carried-work.tar.gz")
	if _, err := bundles.Export(l, bundles.ExportOptions{Job: job, Out: out}); err != nil {
		t.Fatal(err)
	}

	// A workspace that has never heard of the job.
	other := fresh(t)
	if _, err := bundles.Import(other, bundles.ImportOptions{Path: out}); err == nil {
		t.Fatal("importing without the records should be refused")
	} else if !strings.Contains(err.Error(), "brief") {
		t.Errorf("the refusal should say what is missing: %v", err)
	}

	// With the records but without the branch, the commit does not resolve.
	withRecords(t, l, other, job)
	if _, err := bundles.Import(other, bundles.ImportOptions{Path: out}); err == nil {
		t.Fatal("importing without the branch should be refused")
	} else if !strings.Contains(err.Error(), "resolve") {
		t.Errorf("the refusal should say the branch is missing: %v", err)
	}
}

func TestImportUnpacksAndReportsWhatItReplaces(t *testing.T) {
	l, job := source(t)
	out := filepath.Join(t.TempDir(), "carried-work.tar.gz")
	if _, err := bundles.Export(l, bundles.ExportOptions{Job: job, Out: out, Site: "build-01"}); err != nil {
		t.Fatal(err)
	}

	other := fresh(t)
	withRecords(t, l, other, job)
	if _, err := gitx.Run(other.CodeRepo(), "fetch", "--quiet", l.CodeRepo(), "refs/heads/*:refs/heads/*"); err != nil {
		t.Fatal(err)
	}

	result, err := bundles.Import(other, bundles.ImportOptions{Path: out})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Files != 3 {
		t.Errorf("unpacked %d files, want 3 (out, log and data; not tmp or cache)", result.Files)
	}
	if result.Header.Site != "build-01" {
		t.Errorf("header: %+v", result.Header)
	}
	got, err := os.ReadFile(filepath.Join(other.JobSub(job, "out"), "file"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "the result" {
		t.Errorf("the payload did not arrive intact: %q", got)
	}
	if _, err := os.Stat(other.JobSub(job, "tmp")); err == nil {
		t.Error("scratch should not have been carried")
	}
	// Import commits nothing: it writes nothing versioned.
	if _, err := gitx.Run(other.Root, "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}

	// A second import refuses, because a payload is already there.
	if _, err := bundles.Import(other, bundles.ImportOptions{Path: out}); err == nil {
		t.Fatal("importing over an existing payload should be refused")
	}

	// Replacing is explicit, and reports every path it discards.
	if err := os.WriteFile(filepath.Join(other.JobSub(job, "out"), "extra"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	replaced, err := bundles.Import(other, bundles.ImportOptions{Path: out, Replace: true})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(replaced.Discarded) == 0 {
		t.Error("a replacement has to report what it discarded")
	}
	if !contains(replaced.Discarded, "job/"+job+"/out/extra") {
		t.Errorf("the discarded paths should name what went: %v", replaced.Discarded)
	}
	if _, err := os.Stat(filepath.Join(other.JobSub(job, "out"), "extra")); err == nil {
		t.Error("the replaced file should be gone")
	}

	if findings := validate.Validate(other); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestImportRefusesABundleThatWouldEscape(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nasty.tar.gz")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	header := "job: carried-work\nhandoff: 2026-10-09T02-10-00Z.md\nexported: 2026-10-09T03:00:00Z\n"
	if err := tw.WriteHeader(&tar.Header{Name: bundles.HeaderFile, Mode: 0o644, Size: int64(len(header)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(header)); err != nil {
		t.Fatal(err)
	}
	body := "x"
	if err := tw.WriteHeader(&tar.Header{Name: "job/carried-work/../../escaped.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	f.Close()

	l, job := source(t)
	other := fresh(t)
	withRecords(t, l, other, job)
	if _, err := gitx.Run(other.CodeRepo(), "fetch", "--quiet", l.CodeRepo(), "refs/heads/*:refs/heads/*"); err != nil {
		t.Fatal(err)
	}

	if _, err := bundles.Import(other, bundles.ImportOptions{Path: out}); err == nil {
		t.Fatal("a bundle that would land outside the job should be refused")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(other.Root), "escaped.txt")); err == nil {
		t.Fatal("the escape actually happened")
	}
}

func TestImportReadsTheHeaderBeforeAnythingElse(t *testing.T) {
	out := filepath.Join(t.TempDir(), "headless.tar.gz")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := "payload first"
	if err := tw.WriteHeader(&tar.Header{Name: "job/x/out/file", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	f.Close()

	if _, err := bundles.Import(fresh(t), bundles.ImportOptions{Path: out}); err == nil {
		t.Fatal("a bundle whose header is not first should be refused")
	} else if !strings.Contains(err.Error(), bundles.HeaderFile) {
		t.Errorf("the refusal should name the header: %v", err)
	}
}

// fresh is an empty workspace, which is what a receiving site starts as.
func fresh(t *testing.T) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root: filepath.Join(t.TempDir(), "workspace"),
		Site: "lab-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// withRecords copies the versioned half, which is what git would have carried.
func withRecords(t *testing.T, from, to workspace.Layout, job string) {
	t.Helper()
	for _, dir := range []string{to.TaskDir(job), to.HandoffDir(job)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(to.Brief(job), mustRead(t, from.Brief(job)), 0o644); err != nil {
		t.Fatal(err)
	}
	src := from.HandoffDir(job)
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.WriteFile(filepath.Join(to.HandoffDir(job), e.Name()), mustRead(t, filepath.Join(src, e.Name())), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The records are versioned, so a receiving site would have committed them.
	if _, err := gitx.Run(to.Root, "add", "-A", "--", "doc", "handoff"); err != nil {
		t.Fatal(err)
	}
	args := append(gitx.IdentityArgs(to.Root, "tester"), "commit", "--quiet", "-m", "handoff carried-work: arrived")
	if _, err := gitx.Run(to.Root, args...); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// readBundle lists a bundle's entries in order and decodes its header.
func readBundle(t *testing.T, path string) ([]string, bundles.Header) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)

	var names []string
	var header bundles.Header
	for {
		entry, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, entry.Name)
		if entry.Name == bundles.HeaderFile {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if err := format.DecodeLoose(data, &header); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("the bundle is empty")
	}
	return names, header
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
