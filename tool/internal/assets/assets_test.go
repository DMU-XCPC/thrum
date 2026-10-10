package assets_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/assets"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/validate"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

func fixture(t *testing.T) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "demo",
		Actor: "tester",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	openJob(t, l, "sample-job")
	return l
}

// openJob writes a brief straight into doc/task/, which is what every publish needs:
// a version records where it came from.
func openJob(t *testing.T, l workspace.Layout, job string) {
	t.Helper()
	if err := os.MkdirAll(l.TaskDir(job), 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: " + job + "\nstatus: active\nbranch: job/" + job + "\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(l.Brief(job), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestASingleFileKeepsItsRawChecksum(t *testing.T) {
	l := fixture(t)
	src := filepath.Join(t.TempDir(), "sample.csv")
	if err := os.WriteFile(src, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := assets.Ingest(l, src)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !got.Single || got.Bytes != 8 || len(got.SHA256) != 64 {
		t.Fatalf("ingest of a file: %+v", got)
	}
	// Nothing is published yet, so nothing is reachable from a tag.
	unreachable, err := assets.Unreachable(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreachable) == 0 {
		t.Error("ingested bytes should be unreachable until a tag publishes them")
	}

	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "sample", Version: "2026-10-09", Object: got.Commit, Job: "sample-job",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	manifest := readManifest(t, l, "sample", "2026-10-09")
	if manifest.SHA256 != got.SHA256 || manifest.Bytes != got.Bytes {
		t.Errorf("manifest checksums: %+v", manifest)
	}
	if manifest.Ref != "sample@2026-10-09" {
		t.Errorf("ref: %q", manifest.Ref)
	}
	if manifest.ProducedBy == nil || manifest.ProducedBy.Job != "sample-job" {
		t.Errorf("produced_by: %+v", manifest.ProducedBy)
	}

	// The body's title is the record's own, not the template's placeholder.
	_, body, err := format.LoadDocument(l.Manifest("sample", "2026-10-09"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# sample 2026-10-09") {
		t.Errorf("the manifest's title should name the version:\n%s", body)
	}

	count, err := assets.Materialise(l, "sample", "2026-10-09", "sample-job")
	if err != nil {
		t.Fatalf("materialise: %v", err)
	}
	if count != 1 {
		t.Errorf("unpacked %d files", count)
	}
	back, err := os.ReadFile(filepath.Join(l.JobSub("sample-job", "data"), "sample.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != "a,b\n1,2\n" {
		t.Errorf("round trip changed the bytes: %q", back)
	}

	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestADirectoryLandsAtTheRootAndHasNoRawChecksum(t *testing.T) {
	l := fixture(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "two.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "dataset", Version: "2026-10-09", Path: dir, Job: "sample-job",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	manifest := readManifest(t, l, "dataset", "2026-10-09")
	if manifest.SHA256 != "" || manifest.Bytes != 0 {
		t.Errorf("a directory has no single byte string to hash, so neither field belongs: %+v", manifest)
	}

	if _, err := assets.Materialise(l, "dataset", "2026-10-09", "sample-job"); err != nil {
		t.Fatal(err)
	}
	// The dataset itself lands in data/, not a directory named after the source.
	data := l.JobSub("sample-job", "data")
	for _, want := range []string{"one.txt", "nested/two.txt"} {
		if _, err := os.Stat(filepath.Join(data, filepath.FromSlash(want))); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}

	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestAPublishedVersionIsNeverMovedOrReused(t *testing.T) {
	l := fixture(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(file, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "sample", Version: "2026-10-09", Path: file, Job: "sample-job",
	}); err != nil {
		t.Fatal(err)
	}

	// The same version again.
	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "sample", Version: "2026-10-09", Path: file, Job: "sample-job",
	}); err == nil {
		t.Error("publishing a version twice should be refused")
	}

	// A further version the same day takes -2, which the tool does not choose for
	// you: the name has to be written down.
	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "sample", Version: "2026-10-09-2", Path: file, Job: "sample-job",
	}); err != nil {
		t.Fatalf("a further version that day should be allowed: %v", err)
	}

	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "sample", Version: "not-a-date", Path: file, Job: "sample-job",
	}); err == nil {
		t.Error("a version that is not a date should be refused")
	}
	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "sample", Version: "2026-10-10", Path: file, Job: "no-such-job",
	}); err == nil {
		t.Error("a version from a job the workspace does not know should be refused")
	}
}

func TestWithdrawalRetiresTheManifestAndDeletesTheTag(t *testing.T) {
	l := fixture(t)
	file := filepath.Join(t.TempDir(), "bad.csv")
	if err := os.WriteFile(file, []byte("wrong"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "sample", Version: "2026-10-09", Path: file, Job: "sample-job",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := assets.Withdraw(l, assets.WithdrawOptions{
		Asset: "sample", Version: "2026-10-09", Date: "2026-10-10",
		Reason: "the sample was taken from the wrong environment",
	}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	// Gone from the live plane, present in the archive, with a notice after its title.
	if _, err := os.Stat(l.Manifest("sample", "2026-10-09")); err == nil {
		t.Error("the manifest should have left doc/asset/")
	}
	_, body, err := format.LoadDocument(l.ArchivedManifest("sample", "2026-10-09"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "> Retired 2026-10-10: the sample was taken from the wrong environment") {
		t.Errorf("the notice is missing or misplaced:\n%s", text)
	}
	if !strings.Contains(text, "> No replacement.") {
		t.Errorf("with nothing replacing it, the notice should say so:\n%s", text)
	}

	// The tag is gone, so the bytes are collectible, and a materialise refuses.
	if _, err := gitx.Run(l.Store(), "rev-parse", "--verify", "--quiet", "refs/tags/sample@2026-10-09"); err == nil {
		t.Error("the tag should have been deleted")
	}
	if _, err := assets.Materialise(l, "sample", "2026-10-09", "sample-job"); err == nil {
		t.Error("a withdrawn version should not materialise")
	}

	// The version number stays taken, and the refusal must leave nothing behind: a tag
	// made and then abandoned would keep the withdrawn bytes looking published.
	if _, err := assets.Publish(l, assets.PublishOptions{
		Asset: "sample", Version: "2026-10-09", Path: file, Job: "sample-job",
	}); err == nil {
		t.Error("a withdrawn version number should never be reused")
	}
	if _, err := gitx.Run(l.Store(), "rev-parse", "--verify", "--quiet", "refs/tags/sample@2026-10-09"); err == nil {
		t.Error("a refused publish left its tag behind")
	}

	// With the tag gone, the bytes are unreachable and may be collected.
	unreachable, err := assets.Unreachable(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreachable) == 0 {
		t.Error("with the tag deleted, nothing should be reachable")
	}

	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func TestMaterialiseRefusesWhatIsNotPublished(t *testing.T) {
	l := fixture(t)
	if _, err := assets.Materialise(l, "nothing", "2026-10-09", "sample-job"); err == nil {
		t.Error("materialising a version that was never published should be refused")
	}
}

func TestCollectingWhatNoTagReaches(t *testing.T) {
	l := fixture(t)
	file := filepath.Join(t.TempDir(), "x.bin")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.Ingest(l, file); err != nil {
		t.Fatal(err)
	}
	before, err := assets.Unreachable(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("ingested bytes should be unreachable")
	}
	if err := assets.Prune(l); err != nil {
		t.Fatal(err)
	}
	after, err := assets.Unreachable(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("pruning should leave nothing unreachable, got %v", after)
	}
}

// A manifest written by hand, in the style the design documents it, must validate: an
// unquoted date is the documented form.
func TestAManifestWrittenByHandValidates(t *testing.T) {
	l := fixture(t)
	dir := l.AssetDir("handwritten")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nasset: handwritten\nversion: 2026-01-01\nlocation: store\n" +
		"ref: handwritten@2026-01-01\n---\n\n# handwritten 2026-01-01\n\n## Description\n"
	if err := os.WriteFile(filepath.Join(dir, "2026-01-01.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if findings := validate.Validate(l); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}

func readManifest(t *testing.T, l workspace.Layout, asset, version string) format.Manifest {
	t.Helper()
	front, _, err := format.LoadDocument(l.Manifest(asset, version))
	if err != nil {
		t.Fatal(err)
	}
	var m format.Manifest
	if err := format.Decode(front, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
