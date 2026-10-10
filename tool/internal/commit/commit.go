// Package commit records coordination events.
//
// One event, one commit: the coordination repository's subject line names the event,
// and nothing else about the commit is prescribed.
package commit

import (
	"fmt"
	"strings"

	"github.com/multiagentworkspace/ws/tool/internal/gitx"
	"github.com/multiagentworkspace/ws/tool/internal/workspace"
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

	commitArgs := gitx.IdentityArgs(l.Root, "ws")
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
