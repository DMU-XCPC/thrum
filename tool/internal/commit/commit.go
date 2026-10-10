// Package commit records coordination events.
//
// One event, one commit: the coordination repository's subject line names the event,
// and nothing else about the commit is prescribed.
package commit

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Kind is the first word of a coordination commit's subject.
type Kind string

const (
	KindInit       Kind = "init"
	KindBrief      Kind = "brief"
	KindHandoff    Kind = "handoff"
	KindClose      Kind = "close"
	KindTopic      Kind = "topic"
	KindConvention Kind = "convention"
	KindAsset      Kind = "asset"
	KindResource   Kind = "resource"
	KindRetire     Kind = "retire"
)

// Subject builds the subject line. A kind whose subject is omitted — a resource
// declaration has no single subject — takes no second word.
func Subject(kind Kind, subject, summary string) string {
	if strings.TrimSpace(subject) == "" {
		return fmt.Sprintf("%s: %s", kind, summary)
	}
	return fmt.Sprintf("%s %s: %s", kind, subject, summary)
}

// Event stages the given paths and commits them as one coordination event.
//
// Only the paths named are staged, which is what one event per commit already
// requires: a commit that picked up another actor's files would merge two events into
// one, and the message could not be true of both.
func Event(l workspace.Layout, kind Kind, subject, summary string, paths ...string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("a commit needs at least one path")
	}
	args := []string{"add", "-A", "--"}
	args = append(args, paths...)
	if _, err := gitx.Run(l.Root, args...); err != nil {
		return "", fmt.Errorf("staging: %w", err)
	}

	commitArgs := gitx.IdentityArgs(l.Root, "thrum")
	commitArgs = append(commitArgs, "commit", "--quiet", "-m", Subject(kind, subject, summary))
	if _, err := gitx.Run(l.Root, commitArgs...); err != nil {
		return "", err
	}
	id, err := gitx.Run(l.Root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(id), nil
}

// Nothing reports whether there is anything to commit among the given paths, which is
// how callers avoid an empty event.
func Nothing(l workspace.Layout, paths ...string) (bool, error) {
	args := []string{"status", "--porcelain", "--"}
	args = append(args, paths...)
	out, err := gitx.Run(l.Root, args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

// Infer names the coordination event a document belongs to, from where it lives.
//
// It exists for the documents an actor writes by hand — a brief's body, a topic, a
// convention — because the design says those are written in an editor, and every commit
// still goes through the tooling, which is where the coordination lock lives. Inferring
// the event from the path is what lets one command commit any of them with the same
// message form as everything else.
func Infer(path string) (Kind, string, error) {
	// The archive is where a retired document goes, and the event that put it there is
	// a retirement: its subject is the live path, so the message reads as what was
	// retired rather than where it now sits.
	if rest, ok := strings.CutPrefix(path, "doc/archive/"); ok {
		live, err := trimExtension(rest)
		if err != nil {
			return "", "", err
		}
		return KindRetire, live, nil
	}

	switch {
	case strings.HasPrefix(path, "doc/task/"), strings.HasPrefix(path, "doc/archive/task/"):
		job, err := jobOf(path)
		if err != nil {
			return "", "", err
		}
		return KindBrief, job, nil

	case strings.HasPrefix(path, "handoff/"):
		job, err := jobOf(path)
		if err != nil {
			return "", "", err
		}
		return KindHandoff, job, nil

	case strings.HasPrefix(path, "doc/topic/"):
		rest := strings.TrimPrefix(path, "doc/topic/")
		topic, _, _ := strings.Cut(rest, "/")
		if topic == "" {
			return "", "", fmt.Errorf("%s is not inside a topic", path)
		}
		return KindTopic, topic, nil

	case strings.HasPrefix(path, "doc/convention/"):
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		return KindConvention, name, nil

	case strings.HasPrefix(path, "doc/asset/"):
		rest := strings.TrimPrefix(path, "doc/asset/")
		parts := strings.Split(rest, "/")
		if len(parts) < 2 {
			return "", "", fmt.Errorf("%s is not an asset manifest", path)
		}
		version := strings.TrimSuffix(parts[len(parts)-1], ".md")
		return KindAsset, parts[0] + "@" + version, nil

	case strings.HasPrefix(path, "doc/resource/"):
		return KindResource, "", nil
	}
	return "", "", fmt.Errorf("%s is not a document this can name an event for: say which one it is", path)
}

// jobOf reads the job id out of a path that has one.
func jobOf(path string) (string, error) {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "task" && i+1 < len(parts) {
			return parts[i+1], nil
		}
		if part == "handoff" && i+1 < len(parts) {
			return parts[i+1], nil
		}
	}
	return "", fmt.Errorf("%s does not name a job", path)
}

// trimExtension removes a document's extension, for a subject that reads the way a
// person says it.
func trimExtension(path string) (string, error) {
	if filepath.Ext(path) == "" {
		return "", fmt.Errorf("%s is not a document", path)
	}
	return strings.TrimSuffix(path, filepath.Ext(path)), nil
}
