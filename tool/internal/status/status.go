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
	// Presence is what the occupant last said about itself, in one word, filled in by the
	// caller: this report is about the workspace, and what the tooling knows about names is
	// not read here. The time it said it is in the roster view, not repeated here.
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
// Print writes the report for a person.
//
// Columns are as wide as what is in them, because a project names its own jobs and a fixed
// width is wrong the first time one of them is longer. Everything that can be a paragraph —
// a baton, a list of paths — is cut to one line here: a board is read at a glance, and the
// whole of it is in `thrum job show`.
func (r *Report) Print(w io.Writer) {
	fmt.Fprintf(w, "task board\n")
	rows := make([][]string, 0, len(r.Jobs))
	for _, j := range r.Jobs {
		owner := j.Owner
		if owner == "" {
			owner = "unowned"
		}
		where := "doc/task/"
		if !j.Live {
			where = "archived"
		}
		// What comes next is a fact about a job that is still meant to happen. For a closed
		// one it is the tail of the last stretch, and printing it reads as work waiting.
		tail := ""
		if j.Live && j.Next != "" && j.Next != "none" {
			tail = "next: " + cut(boardCell(j.Next), boardTail)
		}
		rows = append(rows, []string{j.ID, j.State, owner, where, tail})
	}
	PrintRows(w, rows, "(no jobs)")

	fmt.Fprintf(w, "worktrees\n")
	rows = rows[:0]
	for _, e := range r.Worktrees {
		holds := e.Branch
		if holds == "" {
			holds = "detached"
		}
		rows = append(rows, []string{e.Label, holds})
	}
	for _, name := range r.Unmaterial {
		rows = append(rows, []string{name, "declared, not materialised here"})
	}
	for _, name := range r.Undeclared {
		rows = append(rows, []string{name, "directory with no declaration"})
	}
	PrintRows(w, rows, "(none materialised)")

	fmt.Fprintf(w, "leases\n")
	rows = rows[:0]
	now := time.Now().UTC()
	for _, lease := range r.Leases {
		free := "held"
		if Expired(lease, now) {
			free = "expired"
		}
		rows = append(rows, []string{lease.Resource + "." + lease.Unit, free, lease.Job, lease.Expires})
	}
	PrintRows(w, rows, "(none held)")

	fmt.Fprintf(w, "occupancy\n")
	rows = rows[:0]
	for _, o := range r.Occupancy {
		tail := o.Presence
		if o.Stale {
			tail = strings.TrimSpace(tail + "  the job is no longer live")
		}
		rows = append(rows, []string{o.Job, o.Occupant, "since " + o.Since, tail})
	}
	PrintRows(w, rows, "(nobody, as far as this site knows)")

	fmt.Fprintf(w, "payloads\n")
	rows = rows[:0]
	for _, c := range r.Collectible {
		if len(c.Now) == 0 && len(c.Later) == 0 {
			continue
		}
		keep := ""
		if c.Keep {
			keep = "  keep: true"
		}
		rows = append(rows, []string{c.Job, "on close: " + listOrDash(c.Now), "after the window: " + listOrDash(c.Later) + keep})
	}
	PrintRows(w, rows, "(no closed job is still carrying a payload)")
}

// boardTail is how much of a baton the board shows before cutting it. The rest is in the job's
// own view, which is where a paragraph belongs.
const boardTail = 64

// PrintRows writes one section of an aligned view: the cells as wide as what is in them, and a
// note when there is nothing, because an empty section that says nothing looks like a section
// that failed. It is here so that the sections a command adds — names, sites — line up with the
// ones the report prints.
func PrintRows(w io.Writer, rows [][]string, note string) {
	if len(rows) == 0 {
		fmt.Fprintf(w, "  %s\n", note)
		return
	}
	widths := columnWidths(rows)
	for _, row := range rows {
		line := "  "
		for i, cell := range row {
			if i == len(row)-1 {
				line += cell
				break
			}
			line += cell + strings.Repeat(" ", widths[i]-len([]rune(cell)))
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
}

// columnWidths is the widest cell in each column, plus the gap that keeps two of them apart.
func columnWidths(rows [][]string) []int {
	widths := []int{}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			if n := len([]rune(cell)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for i := range widths {
		widths[i] += 2
	}
	return widths
}

// boardCell is a value that may be several lines, as one line.
func boardCell(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// cut shortens to a width, marking that there was more of it.
func cut(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return strings.TrimRight(string(runes[:width-1]), " ") + "…"
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
