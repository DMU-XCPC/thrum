// Package bundles carries one job's payload between sites: a transport artifact,
// with the handoff it belongs to as its only check.
package bundles

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// Header is the first entry of every bundle: what it is, and which handoff it
// belongs to.
type Header struct {
	Job      string `yaml:"job"`
	Handoff  string `yaml:"handoff"`
	Exported string `yaml:"exported"`
	Site     string `yaml:"site,omitempty"`
}

var payloadDirs = []string{"log", "data", "ckpt", "out"}

// Export writes one job's payload and the handoff it belongs to.
func Export(l layout.Layout, jobID, out string, now time.Time) error {
	if _, _, ok, err := jobs.ReadBrief(l, jobID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("no job named %q", jobID)
	}
	h, ok, err := jobs.Last(l, jobID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("job %q has no handoff: export runs after one", jobID)
	}
	if out == "" {
		return fmt.Errorf("--out is required")
	}
	if rel, err := filepath.Rel(l.Root, out); err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%s is inside the workspace: write the bundle outside it", out)
	}
	stamp, err := format.UTCToStamp(h.Started)
	if err != nil {
		return err
	}
	header := Header{
		Job: jobID, Handoff: stamp + ".md",
		Exported: now.UTC().Format(format.RFC3339UTC), Site: readSite(l),
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	head, err := format.MarshalYAML(header)
	if err != nil {
		return err
	}
	if err := writeTar(tw, "bundle.yaml", head); err != nil {
		return err
	}
	for _, sub := range payloadDirs {
		root := l.PayloadSub(jobID, sub)
		err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(l.Payload(jobID), path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return writeTar(tw, filepath.ToSlash(filepath.Join("job", jobID, rel)), data)
		})
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Import unpacks a bundle into job/<job-id>/. It commits nothing.
func Import(l layout.Layout, file string, replace bool) error {
	header, names, err := inspect(file)
	if err != nil {
		return err
	}
	if header.Job == "" || header.Handoff == "" {
		return fmt.Errorf("the bundle names no job or no handoff")
	}
	if _, err := os.Stat(l.Handoff(header.Job, header.Handoff)); err != nil {
		return fmt.Errorf("the handoff %s is not here: sync the records before importing", header.Handoff)
	}

	brief, ok, err := briefOf(l, header.Job)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no brief names job %q", header.Job)
	}
	var h records.Handoff
	front, _, err := format.LoadDocument(l.Handoff(header.Job, header.Handoff))
	if err != nil {
		return err
	}
	if err := format.Decode(front, &h); err != nil {
		return err
	}
	if h.Repo != brief.Repo {
		return fmt.Errorf("the handoff is in repository %q, but the brief says %q", h.Repo, brief.Repo)
	}
	gitdir := l.CodeRepo(brief.Repo)
	if _, err := os.Stat(gitdir); err == nil && h.Head != "" {
		if _, err := gitx.Run("", "--git-dir", gitdir, "rev-parse", "--verify", "--quiet", h.Head+"^{commit}"); err != nil {
			return fmt.Errorf("commit %s does not resolve in %s: fetch the code first", h.Head, brief.Repo)
		}
	}

	target := l.Payload(header.Job)
	if _, err := os.Stat(target); err == nil {
		if !replace {
			return fmt.Errorf("job/%s already holds a payload; --replace discards it", header.Job)
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	_ = names
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if name == "bundle.yaml" {
			continue
		}
		dest := filepath.Join(l.Root, filepath.FromSlash(name))
		if !strings.HasPrefix(dest, target+string(filepath.Separator)) {
			return fmt.Errorf("the bundle names %q, which is outside job/%s/", hdr.Name, header.Job)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
}

// inspect reads the header and validates every path without extracting anything.
func inspect(file string) (Header, []string, error) {
	f, err := os.Open(file)
	if err != nil {
		return Header{}, nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return Header{}, nil, err
	}
	tr := tar.NewReader(gz)
	var header Header
	first := true
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Header{}, nil, err
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if first {
			first = false
			if name != "bundle.yaml" {
				return Header{}, nil, fmt.Errorf("a bundle starts with bundle.yaml")
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return Header{}, nil, err
			}
			if err := format.DecodeWhole(data, &header); err != nil {
				return Header{}, nil, fmt.Errorf("bundle.yaml: %w", err)
			}
			continue
		}
		if !strings.HasPrefix(name, "job/") || strings.Contains(name, "..") || filepath.IsAbs(name) {
			return Header{}, nil, fmt.Errorf("the bundle names %q, which is outside the payload", hdr.Name)
		}
		names = append(names, name)
	}
	return header, names, nil
}

func briefOf(l layout.Layout, job string) (records.Brief, bool, error) {
	front, _, err := format.LoadDocument(l.Brief(job))
	if os.IsNotExist(err) {
		front, _, err = format.LoadDocument(l.ArchivedBrief(job))
	}
	if os.IsNotExist(err) {
		return records.Brief{}, false, nil
	}
	if err != nil {
		return records.Brief{}, false, err
	}
	var b records.Brief
	if err := format.Decode(front, &b); err != nil {
		return records.Brief{}, false, err
	}
	return b, true, nil
}

func writeTar(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: time.Now()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func readSite(l layout.Layout) string {
	b, err := os.ReadFile(l.SiteFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
