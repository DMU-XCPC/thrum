// Package assets owns the store: durable bytes, content-addressed, named by an
// asset and a version, never checked out.
package assets

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// ingestRef is where an ingest parks its commit until a publish names it.
const ingestRef = "refs/heads/ingest"

// Ingest writes bytes into the store and reports what it wrote.
func Ingest(l layout.Layout, path string) (records.Artifact, string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return records.Artifact{}, "", err
	}
	src := path
	a := records.Artifact{Path: path}
	if !st.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return records.Artifact{}, "", err
		}
		sum := sha256.Sum256(data)
		a.SHA256 = hex.EncodeToString(sum[:])
		a.Bytes = int64(len(data))
		tmp, err := os.MkdirTemp("", "thrum-ingest-*")
		if err != nil {
			return records.Artifact{}, "", err
		}
		defer os.RemoveAll(tmp)
		if err := os.WriteFile(filepath.Join(tmp, filepath.Base(path)), data, 0o644); err != nil {
			return records.Artifact{}, "", err
		}
		src = tmp
	}
	commit, err := importDir(l.Store(), src)
	if err != nil {
		return records.Artifact{}, "", err
	}
	if _, err := gitx.Run("", "--git-dir", l.Store(), "update-ref", ingestRef, commit); err != nil {
		return records.Artifact{}, "", err
	}
	return a, commit, nil
}

func importDir(store, src string) (string, error) {
	idx, err := os.CreateTemp("", "thrum-index-*")
	if err != nil {
		return "", err
	}
	name := idx.Name()
	_ = idx.Close()
	_ = os.Remove(name) // git wants to create the index itself; an empty file is corrupt
	defer os.Remove(name)
	env := []string{"GIT_INDEX_FILE=" + name}
	add := []string{"-c", "core.bare=false", "--git-dir", store, "--work-tree", src, "add", "-A"}
	if _, err := gitx.RunEnv("", env, add...); err != nil {
		return "", err
	}
	tree, err := gitx.RunEnv("", env, "--git-dir", store, "write-tree")
	if err != nil {
		return "", err
	}
	identity := gitx.IdentityArgs("", "thrum")
	args := append(append([]string{}, identity...), "--git-dir", store, "commit-tree", strings.TrimSpace(tree), "-m", "asset: ingested")
	out, err := gitx.RunEnv("", nil, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Publish writes the tag and the manifest, as one event.
func Publish(l layout.Layout, name, version, from, description string, now time.Time) error {
	if !format.ValidSlug(name) {
		return fmt.Errorf("%q is not an asset name: an asset name is a slug", name)
	}
	if !format.ValidDay(version) {
		return fmt.Errorf("%q is not a version: use YYYY-MM-DD, then -N", version)
	}
	if _, err := os.Stat(l.Manifest(name, version)); err == nil {
		return fmt.Errorf("version %s of %s is already published", version, name)
	}
	if _, err := os.Stat(l.ArchivedManifest(name, version)); err == nil {
		return fmt.Errorf("version %s of %s has been used", version, name)
	}
	tag := format.AssetTag(name, version)
	if _, err := gitx.Run("", "--git-dir", l.Store(), "rev-parse", "--verify", "--quiet", "refs/tags/"+tag); err == nil {
		return fmt.Errorf("tag %s already exists", tag)
	}
	rev := from
	if rev == "" {
		rev = ingestRef
	}
	commit, err := gitx.Run("", "--git-dir", l.Store(), "rev-parse", "--verify", rev)
	if err != nil {
		return fmt.Errorf("nothing has been ingested: %w", err)
	}
	commit = strings.TrimSpace(commit)
	if _, err := gitx.Run("", "--git-dir", l.Store(), "tag", tag, commit); err != nil {
		return err
	}
	if err := markPublished(l, tag); err != nil {
		return err
	}

	m := records.Manifest{Asset: name, Version: version, Location: "store", Ref: tag}
	if sha, size, ok, err := singleBlob(l.Store(), commit); err != nil {
		return err
	} else if ok {
		m.SHA256, m.Bytes = sha, size
	}

	body := []byte("# " + name + " " + version + "\n\n## Description\n\n" + none(description) + "\n")
	front, err := format.MarshalYAML(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.Manifest(name, version)), 0o755); err != nil {
		return err
	}
	if err := format.SaveDocument(l.Manifest(name, version), front, body); err != nil {
		return err
	}
	if err := line.Commit(l.Root, []string{l.Manifest(name, version)},
		fmt.Sprintf("asset %s: published", tag)); err != nil {
		return err
	}
	_, _ = gitx.Run("", "--git-dir", l.Store(), "update-ref", "-d", ingestRef)
	return nil
}

// Materialise extracts one version into a job's transient data directory.
func Materialise(l layout.Layout, name, version, job string) error {
	m, ok, err := Find(l, name, version)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no asset %s at version %s", name, version)
	}
	if m.Location != "store" {
		return fmt.Errorf("version %s of %s is not held in the store", version, name)
	}
	if _, err := os.Stat(l.Brief(job)); err != nil {
		return fmt.Errorf("no job named %q", job)
	}
	dest := l.PayloadSub(job, "data")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	out, err := gitx.Run("", "--git-dir", l.Store(), "archive", "--format=tar", m.Ref)
	if err != nil {
		return err
	}
	return extractTar([]byte(out), dest)
}

// Withdraw retires the manifest and deletes the tag, which makes the bytes
// collectible. The version number stays taken.
func Withdraw(l layout.Layout, name, version, why string, now time.Time) error {
	m, ok, err := Find(l, name, version)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no asset %s at version %s", name, version)
	}
	live := l.Manifest(name, version)
	archived := l.ArchivedManifest(name, version)
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		return err
	}
	front, body, err := format.LoadDocument(live)
	if err != nil {
		return err
	}
	notice := fmt.Sprintf("> Retired %s: %s.\n> Withdrawn from the store; the version number stays taken.",
		now.UTC().Format(format.DayForm), strings.TrimSuffix(why, "."))
	body = afterTitle(body, notice)
	if err := format.SaveDocument(archived, front, body); err != nil {
		return err
	}
	if err := os.Remove(live); err != nil {
		return err
	}
	if m.Ref != "" {
		_, _ = gitx.Run("", "--git-dir", l.Store(), "tag", "-d", m.Ref)
		unpublish(l, m.Ref)
	}
	return line.Commit(l.Root, []string{live, archived},
		fmt.Sprintf("retire asset/%s/%s: withdrawn", name, version))
}

// Find reads one manifest.
func Find(l layout.Layout, name, version string) (records.Manifest, bool, error) {
	front, _, err := format.LoadDocument(l.Manifest(name, version))
	if os.IsNotExist(err) {
		return records.Manifest{}, false, nil
	}
	if err != nil {
		return records.Manifest{}, false, err
	}
	var m records.Manifest
	if err := format.Decode(front, &m); err != nil {
		return records.Manifest{}, false, err
	}
	return m, true, nil
}

// Unreachable lists the objects no tag reaches.
func Unreachable(l layout.Layout) ([]string, error) {
	out, err := gitx.Run("", "--git-dir", l.Store(), "fsck", "--unreachable", "--no-reflogs")
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			lines = append(lines, ln)
		}
	}
	return lines, nil
}

// Prune collects the unreachable objects. They are regenerable by definition.
func Prune(l layout.Layout) error {
	return gitx.OK("", "--git-dir", l.Store(), "gc", "--prune=now")
}

func singleBlob(store, commit string) (sha string, size int64, ok bool, err error) {
	out, err := gitx.Run("", "--git-dir", store, "ls-tree", commit)
	if err != nil {
		return "", 0, false, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 || strings.TrimSpace(lines[0]) == "" {
		return "", 0, false, nil
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 3 || fields[1] != "blob" {
		return "", 0, false, nil
	}
	blob, err := gitx.Run("", "--git-dir", store, "cat-file", "blob", fields[2])
	if err != nil {
		return "", 0, false, err
	}
	sum := sha256.Sum256([]byte(blob))
	return hex.EncodeToString(sum[:]), int64(len(blob)), true, nil
}

func extractTar(data []byte, dest string) error {
	tr := tar.NewReader(strings.NewReader(string(data)))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("the archive names %q, which is outside the destination", hdr.Name)
		}
		target := filepath.Join(dest, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

func afterTitle(body []byte, notice string) []byte {
	var out []string
	inserted := false
	for _, ln := range strings.Split(string(body), "\n") {
		out = append(out, ln)
		if !inserted && strings.HasPrefix(ln, "# ") {
			out = append(out, "", notice)
			inserted = true
		}
	}
	return []byte(strings.Join(out, "\n"))
}

func none(s string) string {
	if strings.TrimSpace(s) == "" {
		return "None."
	}
	return s
}

// Published is the tags this site published. Sync pushes these and only these, so
// a tag fetched from a peer is never pushed back, and a withdrawal is not undone
// by a peer that still has the old tag.
func Published(l layout.Layout) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(l.StateDir(), "tags"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ln := range strings.Split(string(b), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
		}
	}
	return out, nil
}

func writePublished(l layout.Layout, tags []string) error {
	if err := os.MkdirAll(l.StateDir(), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for _, t := range tags {
		b.WriteString(t + "\n")
	}
	return format.WriteFileAtomic(filepath.Join(l.StateDir(), "tags"), []byte(b.String()))
}

func markPublished(l layout.Layout, tag string) error {
	tags, err := Published(l)
	if err != nil {
		return err
	}
	for _, t := range tags {
		if t == tag {
			return nil
		}
	}
	return writePublished(l, append(tags, tag))
}

func unpublish(l layout.Layout, tag string) {
	tags, err := Published(l)
	if err != nil {
		return
	}
	var out []string
	for _, t := range tags {
		if t != tag {
			out = append(out, t)
		}
	}
	_ = writePublished(l, out)
	remotes, err := gitx.Run(l.Store(), "remote")
	if err != nil {
		return
	}
	for _, remote := range strings.Fields(remotes) {
		_ = gitx.OK(l.Store(), "push", remote, ":refs/tags/"+tag)
	}
}
