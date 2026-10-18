// Package jobs owns a job's life: the brief, the branch and working directory it
// opens, the handoffs that record a stretch, and the close that ends it.
package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/repos"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

// OpenOptions is what opening a job needs.
type OpenOptions struct {
	Job       string
	Repo      string
	Label     string
	Purpose   string
	Goal      string
	Scope     string
	Criteria  string
	Owner     string
	Touches   []string
	DependsOn []string
	Keep      bool
	Ephemeral bool
	BodyFile  string
}

// HandoffOptions is what writing a handoff needs.
type HandoffOptions struct {
	Job        string
	Status     string
	Next       string
	BodyFile   string
	Artifacts  []string
	Worktrees  []string
	BlockedBy  []string
	Supersedes string
	Metrics    map[string]any
}

// Board is one row of the task board.
type Board struct {
	Job     string `json:"job"`
	Repo    string `json:"repo"`
	Status  string `json:"status"`
	Owner   string `json:"owner,omitempty"`
	State   string `json:"state"`
	Next    string `json:"next,omitempty"`
	Started string `json:"started,omitempty"`
}

// Open writes the brief, creates the branch, and materialises the working directory.
// It writes everything or nothing.
func Open(l layout.Layout, o OpenOptions, now time.Time) (string, error) {
	if !format.ValidSlug(o.Job) {
		return "", fmt.Errorf("%q is not a job id: a job id is a slug", o.Job)
	}
	if used, err := used(l, o.Job); err != nil {
		return "", err
	} else if used {
		return "", fmt.Errorf("job id %q has been used before", o.Job)
	}
	if _, ok, err := repos.Find(l, o.Repo); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("no repository named %q", o.Repo)
	}
	if o.Owner != "" {
		if _, ok, err := actors.Find(l, o.Owner); err != nil {
			return "", err
		} else if !ok {
			return "", fmt.Errorf("owner %q is not a declared actor", o.Owner)
		}
	}

	body := briefBody(o.Goal, o.Scope, o.Criteria)
	if o.BodyFile != "" {
		b, err := bodyFrom(o.BodyFile)
		if err != nil {
			return "", err
		}
		body = b
	}
	if _, err := worktrees.Declare(l, o.Label, o.Repo, o.Purpose, o.Ephemeral); err != nil {
		return "", err
	}
	path, err := worktrees.Add(l, o.Label, o.Repo, o.Job)
	if err != nil {
		return "", err
	}

	brief := records.Brief{
		Job: o.Job, Status: "active", Repo: o.Repo, Branch: "job/" + o.Job,
		Owner: o.Owner, Touches: o.Touches, DependsOn: o.DependsOn, Keep: o.Keep,
	}
	if err := writeBrief(l, brief, body); err != nil {
		worktrees.Discard(l, o.Label, o.Repo, o.Job)
		return "", err
	}
	if err := line.Commit(l.Root, []string{l.Brief(o.Job)},
		fmt.Sprintf("brief %s: opened", o.Job)); err != nil {
		worktrees.Discard(l, o.Label, o.Repo, o.Job)
		_ = os.RemoveAll(l.TaskDir(o.Job))
		return "", err
	}
	return path, nil
}

// Handoff writes one stretch of work into the ledger.
func Handoff(l layout.Layout, o HandoffOptions, agent string, now time.Time) error {
	b, _, ok, err := ReadBrief(l, o.Job)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no job named %q", o.Job)
	}
	if _, ok, err := actors.Find(l, agent); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("actor %q is not declared; declare it first", agent)
	}
	if b.Status != "active" {
		return fmt.Errorf("job %q is %s, so no stretch of work is recorded for it", o.Job, b.Status)
	}
	switch o.Status {
	case "done", "partial", "blocked", "aborted":
	default:
		return fmt.Errorf("%q is not a handoff status: done, partial, blocked or aborted", o.Status)
	}
	switch {
	case o.Status == "done" && o.Next != "" && o.Next != "none":
		return fmt.Errorf("a done stretch has no baton: next must be none")
	case o.Status != "done" && (o.Next == "" || o.Next == "none"):
		return fmt.Errorf("a stretch that is not done leaves a baton: --next is required")
	case o.Status == "blocked" && len(o.BlockedBy) == 0:
		return fmt.Errorf("a blocked stretch names the jobs it waits on: --blocked-by is required")
	}

	gitdir := l.CodeRepo(b.Repo)
	head, err := revParse(gitdir, "refs/heads/"+b.Branch)
	if err != nil {
		return fmt.Errorf("commit first: the branch %s has no commit yet", b.Branch)
	}

	base, started := head, now
	if prev, ok, err := Last(l, o.Job); err != nil {
		return err
	} else if ok {
		base = prev.Head
		if t, err := format.ParseUTC(prev.Ended); err == nil {
			started = t
		}
		// The file name carries the start. A stretch beginning in the second the
		// previous one began would reuse its name, and a handoff is never rewritten.
		if t, err := format.ParseUTC(prev.Started); err == nil && !started.After(t) {
			started = t.Add(time.Second)
		}
	} else {
		if t, err := commitTime(gitdir, head); err == nil {
			started = t
		}
		if point, err := mergeBase(gitdir, b.Branch); err == nil {
			base = point
		}
	}

	end := now
	if end.Before(started) {
		end = started
	}
	h := records.Handoff{
		Job: o.Job, Repo: b.Repo, Agent: agent, Branch: b.Branch,
		Base: base, Head: head, Status: o.Status,
		Started: started.UTC().Format(format.RFC3339UTC),
		Ended:   end.UTC().Format(format.RFC3339UTC),
		Next:    o.Next, Worktrees: o.Worktrees, Supersedes: o.Supersedes,
		BlockedBy: o.BlockedBy, Metrics: o.Metrics,
	}
	if h.Next == "" {
		h.Next = "none"
	}
	for _, p := range o.Artifacts {
		a, err := artifact(l, o.Job, p)
		if err != nil {
			return err
		}
		h.Artifacts = append(h.Artifacts, a)
	}

	name := format.HandoffFileName(started)
	path := l.Handoff(o.Job, name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("handoff %s already exists: this record is append-only", name)
	}
	body, err := handoffBody(o.BodyFile)
	if err != nil {
		return err
	}
	front, err := format.MarshalYAML(h)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.HandoffDir(o.Job), 0o755); err != nil {
		return err
	}
	if err := format.SaveDocument(path, front, body); err != nil {
		return err
	}
	return line.Commit(l.Ledger(), []string{path},
		fmt.Sprintf("handoff %s: %s", o.Job, o.Status))
}

// Close ends a job: the final status, the brief to the archive, the directory
// released. It merges nothing.
func Close(l layout.Layout, jobID, status string, now time.Time) error {
	b, body, ok, err := ReadBrief(l, jobID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no job named %q", jobID)
	}
	if b.Status != "active" {
		return fmt.Errorf("job %q has already closed as %s", jobID, b.Status)
	}
	if status != "done" && status != "abandoned" {
		return fmt.Errorf("%q is not a closing status: done or abandoned", status)
	}

	if path, ok, err := worktrees.Where(l, b.Repo, jobID); err != nil {
		return err
	} else if ok {
		label := filepath.Base(path)
		if err := worktrees.Release(l, label); err != nil {
			return err
		}
	}

	b.Status = status
	archived := l.ArchivedTaskDir(jobID)
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		return err
	}
	if err := os.Rename(l.TaskDir(jobID), archived); err != nil {
		return err
	}
	if err := writeBriefAt(filepath.Join(archived, "brief.md"), b, body); err != nil {
		return err
	}
	return line.Commit(l.Root, []string{l.TaskDir(jobID), archived},
		fmt.Sprintf("close %s: %s", jobID, status))
}

// Where prints the directory holding a job's branch.
func Where(l layout.Layout, jobID string) (string, bool, error) {
	b, _, ok, err := ReadBrief(l, jobID)
	if err != nil || !ok {
		return "", false, err
	}
	return worktrees.Where(l, b.Repo, jobID)
}

// List is the task board: every live job, with the newest handoff's word for it.
func List(l layout.Layout) ([]Board, error) {
	entries, err := os.ReadDir(l.Tasks())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Board
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, _, ok, err := ReadBrief(l, e.Name())
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		row := Board{Job: b.Job, Repo: b.Repo, Status: b.Status, Owner: b.Owner, State: "no handoff yet"}
		if h, ok, err := Last(l, b.Job); err != nil {
			return nil, err
		} else if ok {
			row.State, row.Next, row.Started = h.Status, h.Next, h.Started
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Job < out[j].Job })
	return out, nil
}

// ReadBrief reads a live job's brief and body.
func ReadBrief(l layout.Layout, jobID string) (records.Brief, []byte, bool, error) {
	return readBriefAt(l.Brief(jobID))
}

// Last returns the newest handoff for a job.
func Last(l layout.Layout, jobID string) (records.Handoff, bool, error) {
	entries, err := os.ReadDir(l.HandoffDir(jobID))
	if os.IsNotExist(err) {
		return records.Handoff{}, false, nil
	}
	if err != nil {
		return records.Handoff{}, false, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return records.Handoff{}, false, nil
	}
	sort.Strings(names)
	var h records.Handoff
	front, _, err := format.LoadDocument(l.Handoff(jobID, names[len(names)-1]))
	if err != nil {
		return records.Handoff{}, false, err
	}
	if err := format.Decode(front, &h); err != nil {
		return records.Handoff{}, false, fmt.Errorf("%s: %w", names[len(names)-1], err)
	}
	return h, true, nil
}

func readBriefAt(path string) (records.Brief, []byte, bool, error) {
	front, body, err := format.LoadDocument(path)
	if os.IsNotExist(err) {
		return records.Brief{}, nil, false, nil
	}
	if err != nil {
		return records.Brief{}, nil, false, err
	}
	var b records.Brief
	if err := format.Decode(front, &b); err != nil {
		return records.Brief{}, nil, false, fmt.Errorf("%s: %w", path, err)
	}
	return b, body, true, nil
}

func writeBrief(l layout.Layout, b records.Brief, body []byte) error {
	if err := os.MkdirAll(l.TaskDir(b.Job), 0o755); err != nil {
		return err
	}
	return writeBriefAt(l.Brief(b.Job), b, body)
}

func writeBriefAt(path string, b records.Brief, body []byte) error {
	front, err := format.MarshalYAML(b)
	if err != nil {
		return err
	}
	return format.SaveDocument(path, front, body)
}

// bodyFrom reads a brief body from a file, or stdin for "-".
func bodyFrom(file string) ([]byte, error) {
	if file == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(file)
}

func briefBody(goal, scope, criteria string) []byte {
	return []byte("## Goal\n\n" + none(goal) +
		"\n\n## Scope\n\n" + none(scope) +
		"\n\n## Acceptance criteria\n\n" + none(criteria) + "\n")
}

func handoffBody(file string) ([]byte, error) {
	if file == "" {
		return []byte("## What changed\n\nNone.\n\n## What was verified\n\nNone.\n\n" +
			"## What remains\n\nNone.\n\n## Open questions\n\nNone.\n"), nil
	}
	if file == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(file)
}

func none(s string) string {
	if strings.TrimSpace(s) == "" {
		return "None."
	}
	return s
}

func used(l layout.Layout, jobID string) (bool, error) {
	if _, _, ok, err := ReadBrief(l, jobID); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	_, _, ok, err := readBriefAt(l.ArchivedBrief(jobID))
	return ok, err
}

func mergeBase(gitdir, branch string) (string, error) {
	out, err := gitx.Run("", "--git-dir", gitdir, "merge-base", "refs/heads/"+branch, "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func revParse(gitdir, rev string) (string, error) {
	out, err := gitx.Run("", "--git-dir", gitdir, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func commitTime(gitdir, rev string) (time.Time, error) {
	out, err := gitx.Run("", "--git-dir", gitdir, "show", "-s", "--format=%cI", rev)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(out))
}

func artifact(l layout.Layout, jobID, arg string) (records.Artifact, error) {
	abs := arg
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(l.Root, abs)
	}
	rel, err := filepath.Rel(l.Payload(jobID), abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return records.Artifact{}, fmt.Errorf("artifact %s is not under job/%s/", arg, jobID)
	}
	f, err := os.Open(abs)
	if err != nil {
		return records.Artifact{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return records.Artifact{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return records.Artifact{}, err
	}
	return records.Artifact{
		Path:   filepath.ToSlash(filepath.Join("job", jobID, rel)),
		SHA256: hex.EncodeToString(h.Sum(nil)),
		Bytes:  info.Size(),
	}, nil
}
