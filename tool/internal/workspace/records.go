package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
)

// Template reads one of the copy-me files a workspace carries under doc/template/.
//
// Records are built from these rather than from strings in the code, so that an
// instance's own template is what an instance's records look like, and a project that
// edits its template gets its own shape.
func Template(l Layout, name string) (front, body []byte, err error) {
	path := filepath.Join(l.DocTemplate(), name)
	front, body, err = format.LoadDocument(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading doc/template/%s: %w", name, err)
	}
	return front, body, nil
}

// WriteBrief writes a brief from the workspace's own template, with the body the actor
// gave it.
//
// A brief whose Goal, Scope and Acceptance criteria are empty is a brief that says
// nothing, so the body is an argument rather than something to be filled in later: an
// actor that opens a job already knows what it is for, and the design says opening is
// one commit.
func WriteBrief(l Layout, b format.Brief, body []byte) error {
	front, templateBody, err := Template(l, "brief.md")
	if err != nil {
		return err
	}
	var tmpl format.Brief
	if err := format.Decode(front, &tmpl); err != nil {
		return fmt.Errorf("doc/template/brief.md: %w", err)
	}
	if len(body) == 0 {
		body = templateBody
	}
	encoded, err := format.Encode(b)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.TaskDir(b.Job), 0o755); err != nil {
		return err
	}
	return format.SaveDocument(l.Brief(b.Job), encoded, body)
}

// ReadBrief reads a brief, from doc/task/ or, when the job has closed, from the
// archive. It returns the path it found it at.
func ReadBrief(l Layout, job string) (format.Brief, string, error) {
	for _, path := range []string{l.Brief(job), l.ArchivedBrief(job)} {
		front, _, err := format.LoadDocument(path)
		if err != nil {
			continue
		}
		var b format.Brief
		if err := format.Decode(front, &b); err != nil {
			return b, path, fmt.Errorf("%s: %w", path, err)
		}
		return b, path, nil
	}
	return format.Brief{}, "", fmt.Errorf("no brief for job %q in doc/task/ or doc/archive/", job)
}

// EditBrief loads a brief, hands it to edit, and writes it back where it was.
func EditBrief(l Layout, job string, edit func(*format.Brief) error) (string, error) {
	b, path, err := ReadBrief(l, job)
	if err != nil {
		return "", err
	}
	if err := edit(&b); err != nil {
		return "", err
	}
	_, body, err := format.LoadDocument(path)
	if err != nil {
		return "", err
	}
	encoded, err := format.Encode(b)
	if err != nil {
		return "", err
	}
	if err := format.SaveDocument(path, encoded, body); err != nil {
		return "", err
	}
	return path, nil
}

// JobExists reports whether a job id has ever been used, which is what "taken
// forever" means: the check looks in both places a brief can be.
func JobExists(l Layout, job string) bool {
	for _, path := range []string{l.Brief(job), l.ArchivedBrief(job)} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}
