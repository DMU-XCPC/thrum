// Package status prints the current picture.
//
// Everything here is derived: the design keeps no separate board, because a board is
// a second copy of the truth and a second copy drifts. The brief says whether a job is
// still meant to happen, the last handoff says how the last stretch went, git says
// which directory holds which branch, and the runtime state under var/ says who is on
// what.
package status

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

// Job is one line of the task board.
type Job struct {
	ID          string
	Status      string
	Owner       string
	Live        bool // still in doc/task/
	Started     bool
	Blocked     bool
	LastHandoff string
	Next        string
}

// Collectible is a payload the retention policy would take something from.
type Collectible struct {
	Job    string
	Closed string   // when the brief says it closed, so far as a file can say
	Now    []string // subdirectories collected at once
	Later  []string // subdirectories collected after the window
	Keep   bool
}

// Report is everything the tool can answer without asking another machine.
type Report struct {
	Jobs        []Job
	Worktrees   []worktrees.Entry
	Undeclared  []string // directories with no entry: the error direction
	Unmaterial  []string // entries with no directory: normal, worth seeing
	Leases      []format.Lease
	Occupancy   []Occupant
	Collectible []Collectible
}

// Occupant is one site-local note about who is on a job here.
type Occupant struct {
	Job      string
	Occupant string
	Since    string
	Stale    bool // the job is no longer live, so the note is
}

// Gather builds the report.
func Gather(l workspace.Layout) (*Report, error) {
	r := &Report{}

	if err := r.gatherJobs(l); err != nil {
		return nil, err
	}
	if err := r.gatherWorktrees(l); err != nil {
		return nil, err
	}
	if err := r.gatherRuntime(l); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Report) gatherJobs(l workspace.Layout) error {
	live, err := jobIDs(filepath.Join(l.Task(), "*", "brief.md"))
	if err != nil {
		return err
	}
	closed, err := jobIDs(filepath.Join(l.ArchiveTask(""), "*", "brief.md"))
	if err != nil {
		return err
	}

	add := func(id string, isLive bool) error {
		b, _, err := workspace.ReadBrief(l, id)
		if err != nil {
			return err
		}
		line := Job{
			ID:      id,
			Status:  b.Status,
			Owner:   b.Owner,
			Live:    isLive,
			Started: jobs.Started(l, id),
			Blocked: jobs.Blocked(l, id),
		}
		if last, _, err := jobs.LastHandoff(l, id); err == nil && last != nil {
			line.LastHandoff = last.Status
			line.Next = last.Next
		}
		r.Jobs = append(r.Jobs, line)

		if !isLive && (b.Status == format.StatusDone || b.Status == format.StatusAbandoned) {
			r.Collectible = append(r.Collectible, collectible(l, id, b.Keep))
		}
		return nil
	}

	for _, id := range live {
		if err := add(id, true); err != nil {
			return err
		}
	}
	for _, id := range closed {
		if err := add(id, false); err != nil {
			return err
		}
	}
	sort.Slice(r.Jobs, func(i, j int) bool {
		if r.Jobs[i].Live != r.Jobs[j].Live {
			return r.Jobs[i].Live
		}
		return r.Jobs[i].ID < r.Jobs[j].ID
	})
	return nil
}

// collectible reports what a job's payload is still carrying.
func collectible(l workspace.Layout, job string, keep bool) Collectible {
	c := Collectible{Job: job, Keep: keep}
	now := []string{"tmp", "cache", "ckpt"}
	later := []string{"log", "data", "state", "out"}

	for _, sub := range now {
		if nonEmpty(l.JobSub(job, sub)) {
			c.Now = append(c.Now, sub)
		}
	}
	for _, sub := range later {
		if nonEmpty(l.JobSub(job, sub)) {
			c.Later = append(c.Later, sub)
		}
	}
	return c
}

func nonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

func (r *Report) gatherWorktrees(l workspace.Layout) error {
	entries, err := worktrees.List(l)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Label != "" {
			r.Worktrees = append(r.Worktrees, e)
		}
	}

	declared, err := workspace.ReadResources(l, format.WorktreeKind)
	if err != nil {
		return err
	}
	materialised := map[string]bool{}
	for _, e := range r.Worktrees {
		materialised[e.Label] = true
	}
	for _, name := range sortedKeys(declared) {
		if !materialised[name] {
			r.Unmaterial = append(r.Unmaterial, name)
		}
	}

	dirs, err := os.ReadDir(l.Worktrees())
	if err != nil {
		return nil // no worktree/ directory simply means nothing is materialised
	}
	for _, d := range dirs {
		if d.IsDir() && !materialised[d.Name()] {
			r.Undeclared = append(r.Undeclared, d.Name())
		}
	}
	return nil
}

func (r *Report) gatherRuntime(l workspace.Layout) error {
	leasePaths, _ := filepath.Glob(filepath.Join(l.Lease(), "*.yaml"))
	for _, path := range leasePaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var lease format.Lease
		if err := format.DecodeLoose(raw, &lease); err != nil {
			continue
		}
		r.Leases = append(r.Leases, lease)
	}
	sort.Slice(r.Leases, func(i, j int) bool {
		return r.Leases[i].Resource+r.Leases[i].Unit < r.Leases[j].Resource+r.Leases[j].Unit
	})

	liveJobs := map[string]bool{}
	for _, j := range r.Jobs {
		if j.Live {
			liveJobs[j.ID] = true
		}
	}
	occupyPaths, _ := filepath.Glob(filepath.Join(l.Occupy(), "*.yaml"))
	for _, path := range occupyPaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var o format.Occupy
		if err := format.DecodeLoose(raw, &o); err != nil {
			continue
		}
		job := strings.TrimSuffix(filepath.Base(path), ".yaml")
		r.Occupancy = append(r.Occupancy, Occupant{
			Job:      job,
			Occupant: o.Occupant,
			Since:    o.Since,
			Stale:    !liveJobs[job],
		})
	}
	sort.Slice(r.Occupancy, func(i, j int) bool { return r.Occupancy[i].Job < r.Occupancy[j].Job })
	return nil
}

// Expired reports whether a lease is free, which is true once its time has passed or
// once the job holding it has closed.
func Expired(lease format.Lease, now time.Time) bool {
	t, err := format.ParseUTC(lease.Expires)
	if err != nil {
		return true
	}
	return t.Before(now)
}

// Print writes the report in a form meant to be read, and grepped.
func (r *Report) Print(w io.Writer) {
	fmt.Fprintf(w, "task board\n")
	if len(r.Jobs) == 0 {
		fmt.Fprintf(w, "  (no jobs)\n")
	}
	for _, j := range r.Jobs {
		// A job that has closed is described by its brief: the state of its last
		// stretch is a detail of how it got there. A live job is described by what
		// the derived facts say, because for one of those they are the interesting
		// part.
		state := j.Status
		if j.Live {
			switch {
			case j.Blocked:
				state = "blocked"
			case !j.Started:
				state = "not started"
			}
		}
		owner := j.Owner
		if owner == "" {
			owner = "unowned"
		}
		where := "doc/task/"
		if !j.Live {
			where = "archived"
		}
		fmt.Fprintf(w, "  %-28s %-11s %-14s %s", j.ID, state, owner, where)
		// What comes next is a fact about a job that is still meant to happen. For a
		// closed one it is the tail of the last stretch, and printing it reads as work
		// waiting to be done.
		if j.Live && j.Next != "" && j.Next != "none" {
			fmt.Fprintf(w, "  next: %s", firstLine(j.Next))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "worktrees\n")
	if len(r.Worktrees) == 0 {
		fmt.Fprintf(w, "  (none materialised)\n")
	}
	for _, e := range r.Worktrees {
		holds := e.Branch
		if holds == "" {
			holds = "detached"
		}
		fmt.Fprintf(w, "  %-20s %s\n", e.Label, holds)
	}
	for _, name := range r.Unmaterial {
		fmt.Fprintf(w, "  %-20s declared, not materialised here\n", name)
	}
	for _, name := range r.Undeclared {
		fmt.Fprintf(w, "  %-20s directory with no declaration\n", name)
	}

	fmt.Fprintf(w, "leases\n")
	if len(r.Leases) == 0 {
		fmt.Fprintf(w, "  (none held)\n")
	}
	now := time.Now().UTC()
	for _, lease := range r.Leases {
		free := "held"
		if Expired(lease, now) {
			free = "expired"
		}
		fmt.Fprintf(w, "  %-20s %-10s %-28s %s\n",
			lease.Resource+"."+lease.Unit, free, lease.Job, lease.Expires)
	}

	fmt.Fprintf(w, "occupancy\n")
	if len(r.Occupancy) == 0 {
		fmt.Fprintf(w, "  (nobody, as far as this site knows)\n")
	}
	for _, o := range r.Occupancy {
		note := ""
		if o.Stale {
			note = "  (the job is no longer live)"
		}
		fmt.Fprintf(w, "  %-28s %-14s since %s%s\n", o.Job, o.Occupant, o.Since, note)
	}

	fmt.Fprintf(w, "payloads\n")
	printed := false
	for _, c := range r.Collectible {
		if len(c.Now) == 0 && len(c.Later) == 0 {
			continue
		}
		keep := ""
		if c.Keep {
			keep = "  keep: true"
		}
		fmt.Fprintf(w, "  %-28s on close: %-14s after the window: %s%s\n",
			c.Job, listOrDash(c.Now), listOrDash(c.Later), keep)
		printed = true
	}
	if !printed {
		fmt.Fprintf(w, "  (no closed job is still carrying a payload)\n")
	}
}

func listOrDash(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func jobIDs(pattern string) ([]string, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, p := range paths {
		ids = append(ids, filepath.Base(filepath.Dir(p)))
	}
	sort.Strings(ids)
	return ids, nil
}

func sortedKeys(m map[string]format.Resource) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
