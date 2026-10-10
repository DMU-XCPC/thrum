// Package payload collects what a closed job left behind.
//
// Collection never makes a record false: a handoff keeps the raw hash of every
// artifact it names, so a collected artifact is a detectable absence rather than a
// silent one. That is also why a payload is disposable by construction.
package payload

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// DefaultWindow is how long the expensive half of a payload is kept after a job
// closes. The design states it as a default and puts the value itself in
// doc/convention/, where everyone in the workspace reads the same one.
const DefaultWindow = 14 * 24 * time.Hour

// Options is what collecting needs.
type Options struct {
	// Window is how long the larger half is kept after the close. Nil means the
	// default; a window of zero means everything goes as soon as the job is closed,
	// which is a different instruction from saying nothing.
	Window *time.Duration
	// DryRun reports what would go without removing anything.
	DryRun bool
	// Now is the clock, so that callers can be tested.
	Now time.Time
}

// Outcome is what happened to one job's payload.
type Outcome struct {
	Job     string
	Skipped string   // why nothing was taken, when nothing was
	Removed []string // subdirectories, relative to the job's payload
}

// OnClose are the subdirectories collected the moment a job closes: the ones that cost
// nothing to lose.
var OnClose = []string{"tmp", "cache", "ckpt"}

// AfterWindow are the ones worth looking at for a while.
var AfterWindow = []string{"log", "data", "state", "out"}

// Collect applies the retention policy.
func Collect(l workspace.Layout, o Options) ([]Outcome, error) {
	window := DefaultWindow
	if o.Window != nil {
		window = *o.Window
	}
	if o.Now.IsZero() {
		o.Now = time.Now().UTC()
	}

	closed, err := closedJobs(l)
	if err != nil {
		return nil, err
	}

	var out []Outcome
	for _, job := range closed {
		brief, _, err := workspace.ReadBrief(l, job)
		if err != nil {
			continue
		}
		result := Outcome{Job: job}

		if brief.Keep {
			result.Skipped = "keep: true"
			out = append(out, result)
			continue
		}
		if archived(l, job) == false {
			result.Skipped = "not closed"
			out = append(out, result)
			continue
		}

		for _, sub := range OnClose {
			removed, err := take(l, job, sub, o)
			if err != nil {
				return out, err
			}
			if removed {
				result.Removed = append(result.Removed, sub)
			}
		}

		// The rest waits for the window, measured from the close. Git knows when
		// that was, which is the one thing a file cannot say reliably and the one
		// thing an audit trail is for.
		closedAt, err := closedAt(l, job)
		if err != nil {
			result.Skipped = fmt.Sprintf("the close is not in the history yet: %v", err)
			out = append(out, result)
			continue
		}
		if o.Now.Sub(closedAt) < window {
			out = append(out, result)
			continue
		}
		for _, sub := range AfterWindow {
			removed, err := take(l, job, sub, o)
			if err != nil {
				return out, err
			}
			if removed {
				result.Removed = append(result.Removed, sub)
			}
		}
		out = append(out, result)
	}
	return out, nil
}

// take removes one subdirectory, or reports that it would. Removing something that is
// not there is not an error: collection is idempotent, so two collectors cannot harm
// each other.
func take(l workspace.Layout, job, sub string, o Options) (bool, error) {
	dir := l.JobSub(job, sub)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	if o.DryRun {
		return true, nil
	}
	return true, os.RemoveAll(dir)
}

// closedJobs lists jobs whose brief is in the archive.
func closedJobs(l workspace.Layout) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(l.ArchiveTask(""), "*", "brief.md"))
	if err != nil {
		return nil, err
	}
	var jobs []string
	for _, p := range paths {
		jobs = append(jobs, filepath.Base(filepath.Dir(p)))
	}
	sort.Strings(jobs)
	return jobs, nil
}

// archived reports whether the brief has left doc/task/, which is the only place a
// collection must not touch: a job still there is still meant to happen.
func archived(l workspace.Layout, job string) bool {
	_, err := os.Stat(l.Brief(job))
	return err != nil
}

// closedAt reads when the brief was archived, from the commit that moved it.
func closedAt(l workspace.Layout, job string) (time.Time, error) {
	rel, err := filepath.Rel(l.Root, l.ArchivedBrief(job))
	if err != nil {
		return time.Time{}, err
	}
	out, err := gitx.Run(l.Root, "log", "-1", "--format=%cI", "--", rel)
	if err != nil {
		return time.Time{}, err
	}
	s := strings.TrimSpace(out)
	if s == "" {
		return time.Time{}, fmt.Errorf("no commit mentions %s", rel)
	}
	return time.Parse(time.RFC3339, s)
}

// Summary renders an outcome for a person.
func (o Outcome) Summary() string {
	switch {
	case o.Skipped != "" && len(o.Removed) == 0:
		return fmt.Sprintf("%s: kept (%s)", o.Job, o.Skipped)
	case len(o.Removed) == 0:
		return fmt.Sprintf("%s: nothing to take", o.Job)
	default:
		return fmt.Sprintf("%s: took %s", o.Job, strings.Join(o.Removed, ", "))
	}
}
