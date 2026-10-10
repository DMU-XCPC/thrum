// Package bundles carries one job's payload between sites.
//
// A bundle is a transport artifact rather than a workspace file: it is written outside
// the workspace, nothing in the workspace refers to it, and it is expected to be
// deleted once unpacked. Its name is a convenience and not an identity — a file that
// has been through a chat client comes back renamed — so what a receiver trusts is the
// header inside it. The header comes first, which is what lets an import refuse before
// it has unpacked anything.
package bundles

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// HeaderFile is the name of the header inside every bundle.
const HeaderFile = "bundle.yaml"

// NotCarried are the payload subdirectories that never travel: the two that exist only
// to be thrown away are the two that need never leave.
var NotCarried = map[string]bool{"tmp": true, "cache": true}

// Header is what a bundle says about itself.
type Header struct {
	Job      string `yaml:"job"`
	Handoff  string `yaml:"handoff"`
	Exported string `yaml:"exported"`
	Site     string `yaml:"site,omitempty"`
}

// ExportOptions is what exporting needs.
type ExportOptions struct {
	Job string
	// Handoff is the record the payload belongs to. Empty means the most recent one,
	// because export runs after a handoff and never during work.
	Handoff string
	// Out is where to write the bundle. Empty means a name in the working directory.
	Out  string
	Site string
	Now  time.Time
}

// Export writes one job's payload as a bundle and returns the path it wrote.
func Export(l workspace.Layout, o ExportOptions) (string, error) {
	if _, _, err := workspace.ReadBrief(l, o.Job); err != nil {
		return "", err
	}

	handoff := o.Handoff
	if handoff == "" {
		// The most recent one: a payload is only as current as the record that
		// describes it, and exporting mid-stretch would name a record that does not
		// exist yet.
		paths, _ := filepath.Glob(filepath.Join(l.HandoffDir(o.Job), "*.md"))
		if len(paths) == 0 {
			return "", fmt.Errorf("%s has no handoff yet, and a bundle belongs to one", o.Job)
		}
		sort.Strings(paths)
		handoff = filepath.Base(paths[len(paths)-1])
	}
	if _, err := os.Stat(l.HandoffFile(o.Job, handoff)); err != nil {
		return "", fmt.Errorf("%s is not a handoff for %s", handoff, o.Job)
	}

	now := o.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	header := Header{
		Job:      o.Job,
		Handoff:  handoff,
		Exported: now.UTC().Format(format.RFC3339UTC),
		Site:     o.Site,
	}

	out := o.Out
	if out == "" {
		out = fmt.Sprintf("%s-%s.tar.gz", o.Job, now.UTC().Format(format.StampForm))
	}
	// The bundle is written outside the workspace, and never inside the payload it is
	// carrying: a bundle that contained itself would grow every time it was made.
	if err := refuseInsidePayload(l, out); err != nil {
		return "", err
	}

	headerBytes, err := format.Encode(header)
	if err != nil {
		return "", err
	}

	file, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)

	if err := writeFileTo(tw, HeaderFile, headerBytes, now); err != nil {
		return "", err
	}
	// The header goes first, so an import can refuse before unpacking anything.
	if err := writePayload(tw, l, o.Job, now); err != nil {
		return "", err
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return out, file.Close()
}

// writePayload walks a job's payload, leaving behind the directories that never travel.
func writePayload(tw *tar.Writer, l workspace.Layout, job string, now time.Time) error {
	dir := l.JobDir(job)
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(l.Root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}
		// The first component below job/<job-id>/ decides whether it travels.
		parts := strings.Split(strings.TrimPrefix(rel, "job/"+job+"/"), "/")
		if NotCarried[parts[0]] {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFileTo(tw, rel, data, now)
	})
}

func writeFileTo(tw *tar.Writer, name string, data []byte, now time.Time) error {
	header := &tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(data)),
		ModTime:  now,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// refuseInsidePayload keeps a bundle out of the thing it carries.
func refuseInsidePayload(l workspace.Layout, out string) error {
	abs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	jobs, err := filepath.Abs(l.Jobs())
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(jobs, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Errorf("a bundle is written outside the workspace: %s is inside the payloads", out)
	}
	return nil
}

// ImportResult is what an import did.
type ImportResult struct {
	Header Header
	Files  int
	// Discarded is every path the replacement removed, because nothing here is
	// removed silently.
	Discarded []string
}

// ImportOptions is what importing needs.
type ImportOptions struct {
	Path string
	// Replace allows what is already there to be discarded. Without it, an existing
	// payload is a refusal: a payload is disposable by construction, so nothing lost
	// was not disposable, but nothing is removed silently either.
	Replace bool
}

// Import unpacks a payload into the job it belongs to.
//
// It refuses unless the records are already local: the named handoff must be present,
// and the commit it names must resolve in the code repository. That single check
// replaces every merge rule the workspace would otherwise need, because the versioned
// half arrives through git and only the payload arrives in the bundle.
func Import(l workspace.Layout, o ImportOptions) (ImportResult, error) {
	var result ImportResult

	file, err := os.Open(o.Path)
	if err != nil {
		return result, err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return result, err
	}
	tr := tar.NewReader(gz)

	header, err := readHeader(tr)
	if err != nil {
		return result, err
	}
	result.Header = header

	if _, _, err := workspace.ReadBrief(l, header.Job); err != nil {
		return result, fmt.Errorf("no brief for %s: fetch the records before the payload", header.Job)
	}
	handoffPath := l.HandoffFile(header.Job, header.Handoff)
	front, _, err := format.LoadDocument(handoffPath)
	if err != nil {
		return result, fmt.Errorf("%s is not here: fetch the history the payload depends on, rather than guessing", header.Handoff)
	}
	var h format.Handoff
	if err := format.Decode(front, &h); err != nil {
		return result, err
	}
	if !gitx.Resolves(l.CodeRepo(), h.Head) {
		return result, fmt.Errorf("commit %s does not resolve here: the branch has to arrive too", h.Head)
	}

	dest := l.JobDir(header.Job)
	if _, err := os.Stat(dest); err == nil {
		if !o.Replace {
			return result, fmt.Errorf("job/%s is already here; ask to replace it if that is what you mean", header.Job)
		}
		discarded, err := listPayload(l, header.Job)
		if err != nil {
			return result, err
		}
		if err := os.RemoveAll(dest); err != nil {
			return result, err
		}
		result.Discarded = discarded
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return result, err
	}

	prefix := "job/" + header.Job + "/"
	for {
		entry, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}
		if entry.Typeflag == tar.TypeXGlobalHeader || entry.Typeflag == tar.TypeXHeader {
			continue
		}
		name := filepath.ToSlash(entry.Name)
		if name == HeaderFile {
			continue
		}
		if !strings.HasPrefix(name, prefix) || strings.Contains(name, "..") {
			return result, fmt.Errorf("%s would land outside job/%s", name, header.Job)
		}
		target := filepath.Join(l.Root, filepath.FromSlash(name))
		if entry.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return result, err
			}
			continue
		}
		if entry.Typeflag != tar.TypeReg {
			return result, fmt.Errorf("%s is not a file or a directory", name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return result, err
		}
		mode := os.FileMode(0o644)
		if entry.Mode&0o100 != 0 {
			mode = 0o755
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return result, err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return result, err
		}
		if err := out.Close(); err != nil {
			return result, err
		}
		result.Files++
	}
	return result, nil
}

// readHeader reads the first entry, which is always the header. Reading it before
// anything else is what lets an import refuse without having unpacked a byte.
func readHeader(tr *tar.Reader) (Header, error) {
	var header Header
	for {
		entry, err := tr.Next()
		if err == io.EOF {
			return header, fmt.Errorf("the bundle has no header")
		}
		if err != nil {
			return header, err
		}
		if entry.Typeflag == tar.TypeXGlobalHeader || entry.Typeflag == tar.TypeXHeader {
			continue
		}
		if filepath.ToSlash(entry.Name) != HeaderFile {
			return header, fmt.Errorf("a bundle starts with %s, and this one starts with %s", HeaderFile, entry.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return header, err
		}
		if err := format.DecodeLoose(data, &header); err != nil {
			return header, fmt.Errorf("%s: %w", HeaderFile, err)
		}
		break
	}
	if header.Job == "" || header.Handoff == "" {
		return header, fmt.Errorf("%s must name the job and the handoff it belongs to", HeaderFile)
	}
	if !format.ValidSlug(header.Job) {
		return header, fmt.Errorf("%q is not a job id", header.Job)
	}
	return header, nil
}

// listPayload lists what a job's payload currently holds, so that a replacement can
// report every path it discards.
func listPayload(l workspace.Layout, job string) ([]string, error) {
	dir := l.JobDir(job)
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(l.Root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}
