// Package occupancy is the site-local note about which actor the tooling is running on
// a job here.
//
// It is written and cleared by the tooling and read through it, because it describes a
// running world rather than a decision. It is a claim and not a lock: a name stays
// behind after its process is gone, and nothing on disk can tell a working actor from
// a dead one.
package occupancy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

// Get returns the note for a job, if there is one.
func Get(l workspace.Layout, job string) (format.Occupy, bool, error) {
	raw, err := os.ReadFile(l.OccupyFile(job))
	if os.IsNotExist(err) {
		return format.Occupy{}, false, nil
	}
	if err != nil {
		return format.Occupy{}, false, err
	}
	var o format.Occupy
	if err := format.DecodeLoose(raw, &o); err != nil {
		return o, true, err
	}
	return o, true, nil
}

// Occupy records that an actor is on a job.
//
// An actor that finds another name in the note does not overwrite it and call the job
// its own: taking a job over is a decision rather than a field edit, so it has to be
// asked for.
func Occupy(l workspace.Layout, job, actor string, takeover bool, now time.Time) error {
	if !format.ValidSlug(actor) {
		return fmt.Errorf("%q is not an actor name", actor)
	}
	if _, _, err := workspace.ReadBrief(l, job); err != nil {
		return err
	}
	if err := os.MkdirAll(l.Occupy(), 0o755); err != nil {
		return err
	}

	existing, ok, err := Get(l, job)
	if err != nil {
		return err
	}
	if ok && existing.Occupant != actor && !takeover {
		return fmt.Errorf("%s is already occupied by %s (since %s); taking it over is a decision, so ask for it",
			job, existing.Occupant, existing.Since)
	}

	since := now.UTC().Format(format.RFC3339UTC)
	if ok && existing.Occupant == actor && existing.Since != "" {
		since = existing.Since // the same actor carrying on is the same claim
	}
	encoded, err := format.Encode(format.Occupy{Occupant: actor, Since: since})
	if err != nil {
		return err
	}
	return format.WriteFileAtomic(l.OccupyFile(job), encoded)
}

// Vacate clears the note and reports what it said, so that a caller can mention whose
// claim it was.
func Vacate(l workspace.Layout, job string) (format.Occupy, error) {
	existing, ok, err := Get(l, job)
	if err != nil {
		return format.Occupy{}, err
	}
	if !ok {
		return format.Occupy{}, nil
	}
	if err := os.Remove(l.OccupyFile(job)); err != nil && !os.IsNotExist(err) {
		return existing, err
	}
	return existing, nil
}

// Sweep removes the notes this site has no business holding.
//
// A note is stale in two ways, and both of them are this site's own business. The job
// has left doc/task/, so nobody is working it anywhere. Or this site no longer holds the
// job — its working directory was released or moved elsewhere — and a claim follows the
// working directory, because the site that holds a job is the only site whose claim
// means anything.
func Sweep(l workspace.Layout) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(l.Occupy(), "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	held := heldHere(l)

	var swept []string
	for _, path := range paths {
		job := strings.TrimSuffix(filepath.Base(path), ".yaml")
		if _, err := os.Stat(l.Brief(job)); err == nil && held[job] {
			continue // live, and here: somebody may still be on it
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return swept, err
		}
		swept = append(swept, job)
	}
	return swept, nil
}

// heldHere is the set of jobs a working directory at this site has checked out.
func heldHere(l workspace.Layout) map[string]bool {
	out := map[string]bool{}
	entries, err := worktrees.List(l)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if job := e.Job(); job != "" {
			out[job] = true
		}
	}
	return out
}
