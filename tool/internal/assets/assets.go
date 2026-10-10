// Package assets is the durable data a workspace keeps: bytes in a bare repository,
// content-addressed, published as tags.
//
// The store is a git repository because git already provides content addressing,
// immutability, deduplication, delta compression between similar versions, integrity
// checking, transport and backup. Publishing a version is a tag, which makes it atomic:
// the blobs are written first, and the tag is what makes the dataset visible. A tag is
// never moved, and never deleted except by withdrawal.
package assets

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/commit"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Ingested is what putting bytes into the store produces.
type Ingested struct {
	// Commit is the object a later publish tags. It is unreachable until then, which
	// is exactly what "the blobs are written first" means.
	Commit string
	// Single reports whether the version's content is one blob. A single blob gets a
	// raw checksum; a directory does not, because there is no single byte string to
	// hash.
	Single bool
	SHA256 string
	Bytes  int64
}

// Ingest writes a file or a directory into the store's object database, without
// publishing it.
//
// A directory's contents land at the root of the version, so materialising it gives
// the dataset itself. A single file lands under its own name, because a file needs one.
func Ingest(l workspace.Layout, path string) (Ingested, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Ingested{}, err
	}
	if !gitx.IsRepo(l.Store()) {
		return Ingested{}, fmt.Errorf("%s is not a repository", l.Store())
	}

	index, err := os.CreateTemp("", "thrum-index-*")
	if err != nil {
		return Ingested{}, err
	}
	indexPath := index.Name()
	index.Close()
	os.Remove(indexPath) // git wants to create it itself
	defer os.Remove(indexPath)

	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := gitx.RunEnv(l.Store(), env, "read-tree", "--empty"); err != nil {
		return Ingested{}, err
	}

	out := Ingested{}
	if info.IsDir() {
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(path, p)
			if err != nil {
				return err
			}
			return addFile(l, env, p, filepath.ToSlash(rel))
		})
		if err != nil {
			return Ingested{}, err
		}
	} else {
		if err := addFile(l, env, path, filepath.Base(path)); err != nil {
			return Ingested{}, err
		}
		sum, size, err := rawSum(path)
		if err != nil {
			return Ingested{}, err
		}
		out.Single, out.SHA256, out.Bytes = true, sum, size
	}

	tree, err := gitx.RunEnv(l.Store(), env, "write-tree")
	if err != nil {
		return Ingested{}, err
	}
	tree = strings.TrimSpace(tree)

	args := gitx.IdentityArgs(l.Store(), "thrum")
	args = append(args, "commit-tree", tree, "-m", "ingest: "+filepath.Base(path))
	commit, err := gitx.Run(l.Store(), args...)
	if err != nil {
		return Ingested{}, err
	}
	out.Commit = strings.TrimSpace(commit)
	return out, nil
}

// addFile hashes one file into the store and stages it in the temporary index.
func addFile(l workspace.Layout, env []string, file, name string) error {
	blob, err := gitx.RunEnv(l.Store(), env, "hash-object", "-w", "--", file)
	if err != nil {
		return err
	}
	mode := "100644"
	if info, err := os.Stat(file); err == nil && info.Mode()&0o100 != 0 {
		mode = "100755"
	}
	spec := fmt.Sprintf("%s,%s,%s", mode, strings.TrimSpace(blob), name)
	_, err = gitx.RunEnv(l.Store(), env, "update-index", "--add", "--cacheinfo", spec)
	return err
}

// rawSum is the hash of the file itself, which is not the git object id: git hashes
// "blob <len>\0<content>". Anything crossing the workspace boundary speaks in raw
// checksums, so both are recorded.
func rawSum(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// PublishOptions is what publishing a version needs.
type PublishOptions struct {
	Asset   string
	Version string
	// Object is what to tag, as Ingest returned it. Path is the alternative: the
	// bytes are ingested first, which is the common case.
	Object string
	Path   string

	Location string
	Source   string
	License  string
	Job      string
	Commit   string
	Summary  string
}

// Publish tags a version and writes its manifest, in one asset commit.
func Publish(l workspace.Layout, o PublishOptions) (string, error) {
	if err := commit.Current(l); err != nil {
		return "", err
	}
	if !format.ValidSlug(o.Asset) {
		return "", fmt.Errorf("%q is not an asset name: an asset name is a slug", o.Asset)
	}
	if !format.ValidDay(o.Version) {
		return "", fmt.Errorf("%q is not a version: a version is a calendar date, with -N for a further version that day", o.Version)
	}
	if o.Location == "" {
		o.Location = "store"
	}
	if _, _, err := workspace.ReadBrief(l, o.Job); err != nil {
		return "", err
	}

	var ingested Ingested
	switch {
	case o.Object != "":
		if err := checkObject(l, o.Object); err != nil {
			return "", err
		}
	case o.Path != "":
		var err error
		ingested, err = Ingest(l, o.Path)
		if err != nil {
			return "", err
		}
		o.Object = ingested.Commit
	default:
		return "", fmt.Errorf("publish needs the bytes: name a path, or an object already ingested")
	}

	tag := format.AssetTag(o.Asset, o.Version)

	// Every refusal happens before anything is created, so that a refused publish
	// leaves nothing behind: a tag made and then abandoned would keep a withdrawn
	// version's bytes alive and looking published.
	if _, err := gitx.Run(l.Store(), "rev-parse", "--verify", "--quiet", "refs/tags/"+tag); err == nil {
		return "", fmt.Errorf("%s is already published, and a tag is never moved", tag)
	}
	path := l.Manifest(o.Asset, o.Version)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	// A version number is taken forever, withdrawal included: reusing it would point
	// every record that names it at different bytes.
	if _, err := os.Stat(l.ArchivedManifest(o.Asset, o.Version)); err == nil {
		return "", fmt.Errorf("%s was withdrawn, and a version number is never reused", tag)
	}

	// A tag names a commit, so the checksums belong to the content the commit holds.
	if !ingested.Single {
		if single, sum, size, err := describe(l, o.Object); err != nil {
			return "", err
		} else if single {
			ingested = Ingested{Single: true, SHA256: sum, Bytes: size}
		}
	}

	manifest := format.Manifest{
		Asset:    o.Asset,
		Version:  o.Version,
		Location: o.Location,
		Ref:      tag,
		Source:   o.Source,
		License:  o.License,
	}
	if ingested.Single {
		manifest.SHA256 = ingested.SHA256
		manifest.Bytes = ingested.Bytes
	}
	if o.Job != "" || o.Commit != "" {
		commit := o.Commit
		if commit == "" {
			commit, _ = headOfJob(l, o.Job)
		}
		manifest.ProducedBy = &format.ProducedBy{Job: o.Job, Commit: commit}
	}

	// The template is read for its body: the front-matter is written from the record
	// itself, so the template's placeholders never reach a real manifest.
	_, body, err := workspace.Template(l, "manifest.md")
	if err != nil {
		return "", err
	}
	encoded, err := format.Encode(manifest)
	if err != nil {
		return "", err
	}
	// The template's own title is a placeholder; the record's is its name and version.
	body = retitle(body, o.Asset+" "+o.Version)

	if err := os.MkdirAll(l.AssetDir(o.Asset), 0o755); err != nil {
		return "", err
	}
	if _, err := gitx.Run(l.Store(), "tag", tag, o.Object); err != nil {
		return "", fmt.Errorf("tagging %s: %w", tag, err)
	}
	if err := format.SaveDocument(path, encoded, body); err != nil {
		// The tag is what publishes a version, so a manifest that cannot be written
		// takes the tag with it rather than leaving a version with no record.
		_, _ = gitx.Run(l.Store(), "tag", "-d", tag)
		return "", err
	}

	summary := o.Summary
	if summary == "" {
		summary = "published"
	}
	return commitEvent(l, commit.KindAsset, tag, summary, path)
}

// describe reports whether a version's content is a single blob, and if so its raw
// checksum and size.
func describe(l workspace.Layout, object string) (bool, string, int64, error) {
	out, err := gitx.Run(l.Store(), "ls-tree", "-r", "-z", "--long", object)
	if err != nil {
		return false, "", 0, err
	}
	entries := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	if len(entries) != 1 {
		return false, "", 0, nil
	}
	// <mode> SP <type> SP <object> SP <size> TAB <file>
	fields := strings.Fields(strings.Replace(entries[0], "\t", " ", 1))
	if len(fields) < 4 || fields[1] != "blob" {
		return false, "", 0, nil
	}
	var size int64
	fmt.Sscanf(fields[3], "%d", &size)

	blob, wait, err := gitx.Stream(l.Store(), "cat-file", "blob", fields[2])
	if err != nil {
		return false, "", 0, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, blob); err != nil {
		blob.Close()
		_ = wait()
		return false, "", 0, err
	}
	if err := blob.Close(); err != nil {
		_ = wait()
		return false, "", 0, err
	}
	if err := wait(); err != nil {
		return false, "", 0, err
	}
	return true, hex.EncodeToString(h.Sum(nil)), size, nil
}

func checkObject(l workspace.Layout, object string) error {
	if !gitx.Resolves(l.Store(), object) {
		return fmt.Errorf("%s does not resolve in the store", object)
	}
	return nil
}

// Materialise extracts a version into a job's transient data directory, which is the
// only place an asset is ever unpacked: assets are never checked out permanently.
func Materialise(l workspace.Layout, asset, version, job string) (int, error) {
	tag := format.AssetTag(asset, version)
	if _, err := gitx.Run(l.Store(), "rev-parse", "--verify", "--quiet", "refs/tags/"+tag); err != nil {
		return 0, fmt.Errorf("%s is not published", tag)
	}
	if _, _, err := workspace.ReadBrief(l, job); err != nil {
		return 0, err
	}
	dest := l.JobSub(job, "data")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return 0, err
	}

	stream, wait, err := gitx.Stream(l.Store(), "archive", tag)
	if err != nil {
		return 0, err
	}
	count, extractErr := extract(stream, dest)
	closeErr := wait()
	if extractErr != nil {
		return count, extractErr
	}
	return count, closeErr
}

// extract unpacks a tar stream, refusing anything that would escape the destination.
func extract(r io.Reader, dest string) (int, error) {
	tr := tar.NewReader(r)
	count := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		target, err := safeJoin(dest, header.Name)
		if err != nil {
			return count, err
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			// Extended headers carry metadata for what follows. git archive emits a
			// global one, and it is not a file in the version.
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return count, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return count, err
			}
			mode := os.FileMode(0o644)
			if header.Mode&0o100 != 0 {
				mode = 0o755
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return count, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return count, err
			}
			if err := f.Close(); err != nil {
				return count, err
			}
			count++
		default:
			return count, fmt.Errorf("%s is not a file or a directory", header.Name)
		}
	}
}

func safeJoin(dest, name string) (string, error) {
	target := filepath.Join(dest, filepath.FromSlash(name))
	rel, err := filepath.Rel(dest, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s would land outside %s", name, dest)
	}
	return target, nil
}

// WithdrawOptions is what withdrawing a version needs.
type WithdrawOptions struct {
	Asset        string
	Version      string
	Date         string
	Reason       string
	SupersededBy string
	Summary      string
}

// Withdraw retires a manifest and deletes its tag, in one retire commit.
//
// Withdrawal is a retirement rather than a deletion, so a handoff that names the
// version keeps a resolvable reference and the reason is on record. The version number
// stays taken.
func Withdraw(l workspace.Layout, o WithdrawOptions) (string, error) {
	if err := commit.Current(l); err != nil {
		return "", err
	}
	if o.Date == "" {
		return "", fmt.Errorf("a withdrawal needs a date")
	}
	if !format.ValidDay(o.Date) {
		return "", fmt.Errorf("%q is not a date", o.Date)
	}
	if o.Reason == "" {
		return "", fmt.Errorf("a withdrawal needs a reason: the notice answers why the document is no longer true")
	}

	path := l.Manifest(o.Asset, o.Version)
	front, body, err := format.LoadDocument(path)
	if err != nil {
		return "", fmt.Errorf("%s is not published", format.AssetTag(o.Asset, o.Version))
	}
	var m format.Manifest
	if err := format.Decode(front, &m); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}

	body = insertNotice(body, o.Date, o.Reason, o.SupersededBy)
	if err := os.MkdirAll(filepath.Dir(l.ArchivedManifest(o.Asset, o.Version)), 0o755); err != nil {
		return "", err
	}
	if err := format.SaveDocument(l.ArchivedManifest(o.Asset, o.Version), front, body); err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	if err := removeIfEmpty(filepath.Dir(path)); err != nil {
		return "", err
	}
	if m.Ref != "" {
		if _, err := gitx.Run(l.Store(), "tag", "-d", m.Ref); err != nil {
			return "", fmt.Errorf("deleting the tag: %w", err)
		}
	}

	summary := o.Summary
	if summary == "" {
		summary = o.Reason
	}
	return commitEvent(l, commit.KindRetire, "asset/"+o.Asset+"/"+o.Version, summary,
		l.AssetDir(o.Asset), filepath.Join(l.Archive(), "asset", o.Asset))
}

// commitEvent records one coordination event, with paths made relative to the root,
// which is where the commit is made.
func commitEvent(l workspace.Layout, kind commit.Kind, subject, summary string, paths ...string) (string, error) {
	rel := make([]string, 0, len(paths))
	for _, p := range paths {
		r, err := filepath.Rel(l.Root, p)
		if err != nil {
			return "", err
		}
		rel = append(rel, r)
	}
	return commit.Event(l, kind, subject, summary, rel...)
}

// Unreachable lists what no tag reaches, which is regenerable by definition and may be
// collected. Reachability is judged without reflogs, because a tag is what publishes a
// version and nothing else in the store does.
func Unreachable(l workspace.Layout) ([]string, error) {
	// fsck reports a non-zero status when it finds something, which here is the
	// point rather than a failure, so only an empty answer is treated as an error.
	out, err := gitx.Run(l.Store(), "fsck", "--unreachable", "--no-reflogs", "--no-progress")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	// git has two words for this. An object nothing reaches at all is dangling; one
	// that only another unreachable object reaches is unreachable. Both are what the
	// design means by "no tag reaches it".
	var found []string
	for _, line := range strings.Split(out, "\n") {
		for _, word := range []string{"dangling ", "unreachable "} {
			if strings.HasPrefix(line, word) {
				found = append(found, strings.TrimPrefix(line, word))
			}
		}
	}
	sort.Strings(found)
	return found, nil
}

// Prune removes everything no tag reaches.
func Prune(l workspace.Layout) error {
	_, err := gitx.Run(l.Store(), "gc", "--prune=now", "--quiet")
	return err
}

// headOfJob is the commit a job's branch is at, used when a manifest records where a
// version came from.
func headOfJob(l workspace.Layout, job string) (string, error) {
	out, err := gitx.Run(l.CodeRepo(), "rev-parse", "job/"+job)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// retitle replaces a template's placeholder title with the record's own.
func retitle(body []byte, title string) []byte {
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			lines[i] = "# " + title
			return []byte(strings.Join(lines, "\n"))
		}
	}
	return body
}

// insertNotice puts a retirement notice immediately after a document's title, which is
// the position the design fixes so that a reader arriving from a stale link finds it
// first.
func insertNotice(body []byte, date, reason, supersededBy string) []byte {
	notice := []string{"> Retired " + date + ": " + reason}
	if supersededBy != "" {
		notice = append(notice, "> Superseded by `"+supersededBy+"`.")
	} else {
		notice = append(notice, "> No replacement.")
	}

	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			out := append([]string{}, lines[:i+1]...)
			out = append(out, "", notice[0], notice[1])
			out = append(out, lines[i+1:]...)
			return []byte(strings.Join(out, "\n"))
		}
	}
	// A document with no title cannot carry a notice in the place the design fixes,
	// so the caller is told by validation rather than by a notice in the wrong place.
	return body
}

// removeIfEmpty removes a directory left empty by a move.
func removeIfEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	if len(entries) == 0 {
		return os.Remove(dir)
	}
	return nil
}
