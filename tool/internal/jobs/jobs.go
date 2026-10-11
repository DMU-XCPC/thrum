// Package jobs is the life of a job: opening it, working it, handing it over, and
// closing it.
//
// Opening is not one file write. It touches the brief, the code repository and a
// working directory, and has to leave all three consistent or none, which is why it
// rolls back rather than half-succeeding.
package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/commit"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/leases"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

// OpenOptions is what opening a job needs.
type OpenOptions struct {
	Job        string
	Owner      string
	DependsOn  []string
	OwnedPaths []string
	Keep       bool
	// Label is the working directory the job will be worked in. It has to be
	// declared already: declaring a resource is a different event with a commit of
	// its own.
	Label string
	// Base is the commit to branch from. Empty means the code repository's default
	// branch.
	Base string
	// Summary is the commit's summary.
	Summary string
	// Body is the brief's body: the goal, the scope and the acceptance criteria. Empty
	// means the template's headings with nothing under them, which is a brief that says
	// nothing.
	Body []byte
}

// Open records a job: the brief, the branch, and a labelled working directory.
func Open(l workspace.Layout, o OpenOptions) (string, error) {
	if !format.ValidSlug(o.Job) {
		return "", fmt.Errorf("%q is not a job id: a job id is a slug", o.Job)
	}
	if workspace.JobExists(l, o.Job) {
		return "", fmt.Errorf("job id %q has been used before, and ids are taken forever", o.Job)
	}
	if o.Label == "" {
		return "", fmt.Errorf("a job needs a working directory: name one with --label")
	}
	declared, err := workspace.ReadResources(l, format.WorktreeKind)
	if err != nil {
		return "", err
	}
	if _, ok := declared[o.Label]; !ok {
		return "", fmt.Errorf("worktree %q is not declared: declare it first, which is its own commit", o.Label)
	}
	if held, materialised, err := worktrees.Holder(l, o.Label); err != nil {
		return "", err
	} else if materialised && held != "" {
		return "", fmt.Errorf("worktree %q already holds job %s", o.Label, held)
	}
	// Nothing is written or decided before the line is current: what this reads has to
	// be what the other sites have already published.
	if err := commit.Current(l); err != nil {
		return "", err
	}

	// A job branches from the project's history. With no history there is no branch,
	// and git will make one that resolves to nothing — so this says so now, rather than
	// leaving the first handoff to fail with something about common ancestors.
	if _, err := gitx.Run(l.CodeRepo(), "rev-parse", "--verify", "HEAD"); err != nil {
		return "", fmt.Errorf("the code repository has no commits yet: a job branches from the project's history, and there is none to branch from")
	}

	brief := format.Brief{
		Job:        o.Job,
		Status:     format.StatusActive,
		Branch:     "job/" + o.Job,
		OwnedPaths: o.OwnedPaths,
		Owner:      o.Owner,
		DependsOn:  o.DependsOn,
		Keep:       o.Keep,
	}
	if err := workspace.WriteBrief(l, brief, o.Body); err != nil {
		return "", err
	}
	// Before anything is written: an event that only discovers the index is in its way
	// when it goes to record itself leaves half of itself behind.
	if err := commit.Guard(l, rel(l, l.Brief(o.Job))); err != nil {
		return "", err
	}

	// The payload's shape is the design's, and it exists from the moment the job does:
	// an actor told that out/ is where results go should find out/ there. These are
	// empty directories under an ignored path, so they need no commit.
	for sub := range payloadSubdirs {
		if err := os.MkdirAll(l.JobSub(o.Job, sub), 0o755); err != nil {
			_ = os.RemoveAll(l.TaskDir(o.Job))
			_ = os.RemoveAll(l.JobDir(o.Job))
			return "", err
		}
	}

	if err := worktrees.Add(l, o.Label, o.Job, o.Base); err != nil {
		// The brief and the working directory go together: one without the other is
		// a job nobody can find or a directory nobody asked for.
		_ = os.RemoveAll(l.TaskDir(o.Job))
		_ = os.RemoveAll(l.JobDir(o.Job))
		return "", fmt.Errorf("materialising worktree %s: %w", o.Label, err)
	}

	// Whoever opens a job with an owner is on it: the claim is what says so, and a
	// person working by hand should not have to remember a second command to be
	// believed.
	if o.Owner != "" {
		if err := occupancy.Occupy(l, o.Job, o.Owner, false, time.Now().UTC()); err != nil {
			_ = worktrees.Remove(l, o.Label)
			_ = os.RemoveAll(l.TaskDir(o.Job))
			_ = os.RemoveAll(l.JobDir(o.Job))
			return "", err
		}
	}

	summary := o.Summary
	if summary == "" {
		summary = fmt.Sprintf("opened in worktree %s", o.Label)
	}
	id, err := commit.Event(l, commit.KindBrief, o.Job, summary, rel(l, l.Brief(o.Job)))
	if err != nil {
		_ = worktrees.Remove(l, o.Label)
		_ = os.RemoveAll(l.TaskDir(o.Job))
		_ = os.RemoveAll(l.JobDir(o.Job))
		return "", err
	}
	return id, nil
}

// payloadSubdirs is the shape of a job's payload, which the design fixes: what exists
// only to be thrown away, what is kept for a while, and where results go.
var payloadSubdirs = map[string]bool{
	"log": true, "tmp": true, "cache": true, "data": true,
	"state": true, "ckpt": true, "out": true,
}

// HandoffOptions is what writing a handoff needs. Base and Head are computed rather
// than supplied, because a hand gets them wrong.
type HandoffOptions struct {
	Job        string
	Agent      string
	Status     string
	Next       string
	Started    string // the start of the stretch, in either time form; empty means now
	Worktrees  []string
	Supersedes string
	BlockedBy  []string
	OwnedPaths []string
	Metrics    map[string]any
	Artifacts  []format.Artifact
	Assets     []string
	Summary    string
}

// WriteHandoff records one stretch of work. It refuses a file name that already
// exists: never overwriting is the minimum an append-only record owes.
func WriteHandoff(l workspace.Layout, o HandoffOptions) (string, error) {
	if err := commit.Current(l); err != nil {
		return "", err
	}
	brief, _, err := workspace.ReadBrief(l, o.Job)
	if err != nil {
		return "", err
	}
	// Only a job that is still meant to happen is worked, so only its stretches are
	// recorded: a job that is done or abandoned has none. A correction to a record on a
	// finished job covers no work at all, and is written by hand and committed like any
	// other document rather than through here.
	if brief.Status != format.StatusActive {
		return "", fmt.Errorf("%s is %s, and only a job that is still meant to happen is worked", o.Job, brief.Status)
	}
	if !format.ValidSlug(o.Agent) {
		return "", fmt.Errorf("%q is not an actor name: an actor name is a slug", o.Agent)
	}

	started, err := parseEitherForm(o.Started)
	if err != nil {
		return "", err
	}
	ended := time.Now().UTC()

	base, err := BaseFor(l, o.Job)
	if err != nil {
		return "", err
	}
	head, err := HeadFor(l, o.Job)
	if err != nil {
		return "", err
	}

	front, body, err := workspace.Template(l, "handoff.md")
	if err != nil {
		return "", err
	}
	var tmpl format.Handoff
	if err := format.Decode(front, &tmpl); err != nil {
		return "", fmt.Errorf("doc/template/handoff.md: %w", err)
	}

	h := format.Handoff{
		Job:        o.Job,
		Agent:      o.Agent,
		Branch:     "job/" + o.Job,
		Base:       base,
		Head:       head,
		Status:     o.Status,
		Started:    started.Format(format.RFC3339UTC),
		Ended:      ended.Format(format.RFC3339UTC),
		Next:       o.Next,
		Worktrees:  o.Worktrees,
		Supersedes: o.Supersedes,
		BlockedBy:  o.BlockedBy,
		OwnedPaths: o.OwnedPaths,
		Metrics:    o.Metrics,
		Artifacts:  o.Artifacts,
		Assets:     o.Assets,
	}
	if h.Status == format.HandoffDone && h.Next == "" {
		h.Next = "none"
	}
	// Whatever case it arrives in, the record carries the one the design gives.
	if strings.EqualFold(strings.TrimSpace(h.Next), "none") {
		h.Next = "none"
	}
	for _, a := range h.Artifacts {
		if !strings.HasPrefix(a.Path, "job/"+o.Job+"/") || strings.HasPrefix(a.Path, "../") || filepath.IsAbs(a.Path) {
			return "", fmt.Errorf("an artifact path lies under `job/%s/`, relative to the workspace root, and %q does not", o.Job, a.Path)
		}
	}
	if err := validateStatus(h); err != nil {
		return "", err
	}

	encoded, err := format.Encode(h)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(l.HandoffDir(o.Job), 0o755); err != nil {
		return "", err
	}
	name := format.HandoffFileName(started)
	path := l.HandoffFile(o.Job, name)
	if err := commit.Guard(l, rel(l, path)); err != nil {
		return "", err
	}
	// Claim the name before writing it, so that two stretches beginning in the same
	// second cannot silently become one record.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("handoff %s already exists: a second stretch starting in the same second is a collision, not an edit", name)
		}
		return "", err
	}
	f.Close()
	if err := format.SaveDocument(path, encoded, body); err != nil {
		_ = os.Remove(path)
		return "", err
	}

	summary := o.Summary
	if summary == "" {
		summary = fmt.Sprintf("%s", h.Status)
	}
	return commit.Event(l, commit.KindHandoff, o.Job, summary, rel(l, path))
}

// Close ends a job: it sets the final status, archives the brief, and releases the
// working directory. It merges nothing.
func Close(l workspace.Layout, job, status, summary string) (string, error) {
	if err := commit.Current(l); err != nil {
		return "", err
	}
	switch status {
	case format.StatusDone, format.StatusAbandoned:
	default:
		return "", fmt.Errorf("a job closes as %q or %q, not %q",
			format.StatusDone, format.StatusAbandoned, status)
	}
	brief, _, err := workspace.ReadBrief(l, job)
	if err != nil {
		return "", err
	}
	// Nothing is written before this is known. A refusal that has already changed a record
	// is worse than a refusal: a job is closed once, and asking twice must leave the brief
	// exactly as it was.
	if brief.Status != format.StatusActive {
		return "", fmt.Errorf("%s is %s already: a job is closed once", job, brief.Status)
	}
	if _, err := workspace.EditBrief(l, job, func(b *format.Brief) error {
		b.Status = status
		return nil
	}); err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Join(l.Archive(), "task"), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(l.TaskDir(job), l.ArchiveTask(job)); err != nil {
		return "", fmt.Errorf("archiving the brief: %w", err)
	}

	if err := releaseWorktrees(l, job); err != nil {
		return "", err
	}
	// A closed job's lease is as free as an expired one everywhere, because a lease
	// dies with the job that holds it. Deleting the records here is tidiness rather
	// than the rule, and it is what keeps the local picture honest.
	if err := releaseLeases(l, job); err != nil {
		return "", err
	}
	// And nobody is on it any more: a note left behind would claim somebody was still
	// working, which is the one lie that file must not tell.
	if _, err := occupancy.Vacate(l, job); err != nil {
		return "", err
	}

	if summary == "" {
		summary = fmt.Sprintf("closed as %s", status)
	}
	// The event owns one job's directory and its place in the archive, not the whole of
	// doc/task: another site's event elsewhere must not be swept into this commit.
	return commit.Event(l, commit.KindClose, job, summary,
		rel(l, l.TaskDir(job)), rel(l, filepath.Join(l.Archive(), "task", job)))
}

// releaseWorktrees leaves every directory carrying this job's branch clean and
// detached.
func releaseWorktrees(l workspace.Layout, job string) error {
	entries, err := worktrees.List(l)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Job() != job || e.Label == "" {
			continue
		}
		if _, err := worktrees.Release(l, e.Label); err != nil {
			return err
		}
	}
	return nil
}

// releaseLeases deletes the leases this job still holds here.
func releaseLeases(l workspace.Layout, job string) error {
	held, err := leases.Held(l)
	if err != nil {
		return err
	}
	for _, lease := range held {
		if lease.Job != job {
			continue
		}
		if err := leases.Release(l, lease.Resource, lease.Unit, job); err != nil {
			return err
		}
	}
	return nil
}

// SetOwner sets or clears a job's owner. A job with no owner is one nobody is
// responsible for: it has either never been assigned or been given up, and either way
// whoever takes it up may claim it without asking.
func SetOwner(l workspace.Layout, job, owner, by, summary string) (string, error) {
	if err := commit.Current(l); err != nil {
		return "", err
	}
	if owner != "" && !format.ValidSlug(owner) {
		return "", fmt.Errorf("%q is not an actor name", owner)
	}
	brief, _, err := workspace.ReadBrief(l, job)
	if err != nil {
		return "", err
	}
	// An owner is who the work was promised to, so ownership changes in only two ways: the
	// owner reassigns it or gives it up, or somebody takes up a job that has no owner. Any
	// other actor taking an owned job is taking a promise it did not make.
	if brief.Owner != "" && brief.Owner != by {
		return "", fmt.Errorf("%s is owned by %s, and only its owner may reassign it or give it up", job, brief.Owner)
	}
	if brief.Owner == owner {
		return "", nil // nothing to change, and nothing to record
	}
	if err := commit.Guard(l, rel(l, l.Brief(job))); err != nil {
		return "", err
	}
	path, err := workspace.EditBrief(l, job, func(b *format.Brief) error {
		b.Owner = owner
		return nil
	})
	if err != nil {
		return "", err
	}
	if summary == "" {
		if owner == "" {
			summary = "ownership given up"
		} else {
			summary = "owner is now " + owner
		}
	}
	return commit.Event(l, commit.KindBrief, job, summary, rel(l, path))
}

// BaseFor returns the commit a stretch started from: the previous handoff's head, or,
// for the first stretch, the point the job's branch left the default branch.
func BaseFor(l workspace.Layout, job string) (string, error) {
	if last, _, err := LastHandoff(l, job); err == nil && last != nil {
		return last.Head, nil
	}
	def, err := worktrees.DefaultBranch(l)
	if err != nil {
		return "", err
	}
	out, err := gitx.Run(l.CodeRepo(), "merge-base", "job/"+job, def)
	if err != nil {
		// A branch with no commits of its own has no common ancestor to find. Opening a
		// job refuses a project with no history, so reaching here means the job was
		// opened before there was any, and saying that is more use than what git says.
		return "", fmt.Errorf("the branch for %s has no commits and shares no ancestor with %s: this job was opened before the project had any history", job, def)
	}
	return strings.TrimSpace(out), nil
}

// HeadFor returns the commit the job's branch is at now.
func HeadFor(l workspace.Layout, job string) (string, error) {
	out, err := gitx.Run(l.CodeRepo(), "rev-parse", "job/"+job)
	if err != nil {
		return "", fmt.Errorf("the branch for %s does not resolve: %w", job, err)
	}
	return strings.TrimSpace(out), nil
}

// LastHandoff returns the most recent record for a job, or nil when there is none.
// File names sort by time, which is why the name carries the start.
func LastHandoff(l workspace.Layout, job string) (*format.Handoff, string, error) {
	paths, err := filepath.Glob(filepath.Join(l.HandoffDir(job), "*.md"))
	if err != nil || len(paths) == 0 {
		return nil, "", err
	}
	sort.Strings(paths)
	path := paths[len(paths)-1]

	front, _, err := format.LoadDocument(path)
	if err != nil {
		return nil, path, err
	}
	var h format.Handoff
	if err := format.Decode(front, &h); err != nil {
		return nil, path, err
	}
	return &h, path, nil
}

// Handoffs returns every record for a job, oldest first.
func Handoffs(l workspace.Layout, job string) ([]format.Handoff, error) {
	paths, err := filepath.Glob(filepath.Join(l.HandoffDir(job), "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	out := make([]format.Handoff, 0, len(paths))
	for _, path := range paths {
		front, _, err := format.LoadDocument(path)
		if err != nil {
			return nil, err
		}
		var h format.Handoff
		if err := format.Decode(front, &h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// Blocked reports whether a job is blocked, which is derived rather than stored: a
// job is blocked exactly while its latest handoff is.
func Blocked(l workspace.Layout, job string) bool {
	last, _, err := LastHandoff(l, job)
	return err == nil && last != nil && last.Status == format.HandoffBlocked
}

// Started reports whether any work has been recorded for a job, which is also derived
// rather than stored.
func Started(l workspace.Layout, job string) bool {
	paths, err := filepath.Glob(filepath.Join(l.HandoffDir(job), "*.md"))
	return err == nil && len(paths) > 0
}

func validateStatus(h format.Handoff) error {
	switch h.Status {
	case format.HandoffDone, format.HandoffPartial, format.HandoffBlocked, format.HandoffAborted:
	default:
		return fmt.Errorf("a handoff is done, partial, blocked or aborted, not %q", h.Status)
	}
	// `none` is a value rather than a spelling, and the two directions of this rule are
	// one rule: it is what a finished stretch says, and what no other stretch may say.
	nextIsNone := strings.EqualFold(strings.TrimSpace(h.Next), "none")
	if h.Status == format.HandoffDone && !nextIsNone {
		return fmt.Errorf("a finished handoff says next: none")
	}
	if h.Status != format.HandoffDone {
		if nextIsNone {
			return fmt.Errorf("`next` is `none` exactly when the status is `done`, and this stretch is %s", h.Status)
		}
		if strings.TrimSpace(h.Next) == "" {
			return fmt.Errorf("a stretch that is not finished leaves a baton: say what whoever continues does first")
		}
	}
	if (h.Status == format.HandoffBlocked) != (len(h.BlockedBy) > 0) {
		return fmt.Errorf("blocked_by belongs to a blocked handoff and to no other")
	}
	return nil
}

// parseEitherForm accepts a timestamp in either of the two forms the design gives
// time, because one of them is what a person types and the other is what a file name
// carries.
func parseEitherForm(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Now().UTC(), nil
	}
	if format.ValidStamp(s) {
		return format.ParseStamp(s)
	}
	return format.ParseUTC(s)
}

// rel makes a path relative to the workspace root, which is what git wants when it is
// invoked there.
func rel(l workspace.Layout, path string) string {
	r, err := filepath.Rel(l.Root, path)
	if err != nil {
		return path
	}
	return r
}
