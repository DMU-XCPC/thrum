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
	ID      string `json:"id"`
	Status  string `json:"status"`
	Owner   string `json:"owner,omitempty"`
	Live    bool   `json:"live"`
	Started bool   `json:"started"`
	Blocked bool   `json:"blocked"`
	// State is Board's answer, carried rather than derived by the reader: the text and the
	// JSON describe a job the same way, and only one of them can call a method.
	State       string `json:"state"`
	LastHandoff string `json:"last_handoff,omitempty"`
	Next        string `json:"next,omitempty"`
}

// Collectible is a payload the retention policy would take something from.
type Collectible struct {
	Job    string   `json:"job"`
	Closed string   `json:"closed,omitempty"` // when the brief says it closed, so far as a file can say
	Now    []string `json:"now,omitempty"`    // subdirectories collected at once
	Later  []string `json:"later,omitempty"`  // subdirectories collected after the window
	Keep   bool     `json:"keep,omitempty"`
}

// Report is everything the tool can answer without asking another machine.
type Report struct {
	Jobs        []Job             `json:"jobs"`
	Worktrees   []worktrees.Entry `json:"worktrees"`
	Undeclared  []string          `json:"undeclared,omitempty"`     // directories with no entry: the error direction
	Unmaterial  []string          `json:"unmaterialised,omitempty"` // entries with no directory: normal, worth seeing
	Leases      []format.Lease    `json:"leases"`
	Occupancy   []Occupant        `json:"occupancy"`
	Collectible []Collectible     `json:"collectible,omitempty"`
}

// Board is how the board describes a job, and it is what the design says the board is: the
// brief's status together with the last handoff's. A closed job is described by its brief,
// because the state of its last stretch is a detail of how it got there; a live one is
// described by what the last stretch said — *done*, *partial*, *blocked*, *aborted* — which is
// the question somebody reading the board is asking.
//
// A job that has been opened and has no handoff yet is the case that has no such answer, and it
// says exactly that. It does not say "not started": an actor may be on it, and the claim beside
// the board is where that is written.
func (j Job) Board() string {
	if !j.Live {
		return j.Status
	}
	if j.LastHandoff == "" {
		return "no handoff yet"
	}
	return j.LastHandoff
}

// Occupant is one site-local note about who is on a job here.
type Occupant struct {
	Job      string `json:"job"`
	Occupant string `json:"occupant"`
	Since    string `json:"since"`
	Stale    bool   `json:"stale,omitempty"` // the job is no longer live, so the note is
	// Presence is what the occupant last said about itself, already worded for a reader
	// and filled in by the caller: this report is about the workspace, and what the
	// tooling knows about names is not read here.
	Presence string `json:"presence,omitempty"`
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
		line.State = line.Board()
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
		state := j.State
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
		if o.Presence != "" {
			note = "  (" + o.Presence + ")" + note
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
