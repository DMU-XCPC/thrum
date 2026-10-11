// Package leases is the one runtime record with a mechanism behind it.
//
// A lease is the only way to say "this unit of this resource is mine until then". It
// is held across commands rather than for the life of one process, so nothing can
// release it when its holder dies — which is why a record whose expiry has passed is
// free, and why the file is the promise rather than the process.
package leases

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/atomic"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Request is what taking or renewing a lease needs. A lease is held by a job, so the
// job is the whole of who is asking.
type Request struct {
	Resource string
	// Unit is which unit, or empty to take the first free one.
	Unit string
	Job  string
	TTL  time.Duration
}

// Take writes a lease, refusing a unit that somebody else holds.
func Take(l workspace.Layout, r Request, now time.Time) (format.Lease, error) {
	if r.Job == "" {
		return format.Lease{}, fmt.Errorf("a lease always belongs to a job")
	}
	brief, _, err := workspace.ReadBrief(l, r.Job)
	if err != nil {
		return format.Lease{}, fmt.Errorf("no job %q: a lease always belongs to a job the workspace knows", r.Job)
	}
	// A job that is done or abandoned is no longer meant to happen, so nothing is held
	// for it. A record left behind by such a job is still free for others — its holder
	// is gone — but that is a different thing from leasing for a job that is over.
	if brief.Status != format.StatusActive {
		return format.Lease{}, fmt.Errorf("%s is %s, and nothing is leased for a job that is no longer meant to happen", r.Job, brief.Status)
	}
	if r.TTL <= 0 {
		return format.Lease{}, fmt.Errorf("a lease needs a time to run until")
	}
	capacity, err := Capacity(l, r.Resource)
	if err != nil {
		return format.Lease{}, err
	}
	// An explicit unit is bounded here rather than only noticed by verify afterwards: a
	// unit above the capacity is not a unit, and holding one would hold nothing — which is
	// the one thing a lease exists to prevent.
	if r.Unit != "" {
		n, err := strconv.Atoi(r.Unit)
		if err != nil || n < 1 {
			return format.Lease{}, fmt.Errorf("%q is not a unit: a unit is a number from 1 up", r.Unit)
		}
		if n > capacity {
			return format.Lease{}, fmt.Errorf("%s has %d unit(s), so %q is not one of them", r.Resource, capacity, r.Unit)
		}
	}

	units := []string{r.Unit}
	if r.Unit == "" {
		units = units[:0]
		for i := 1; i <= capacity; i++ {
			units = append(units, strconv.Itoa(i))
		}
	}

	var refused error
	for _, unit := range units {
		lease, err := takeUnit(l, r, unit, now)
		if err == nil {
			return lease, nil
		}
		refused = err
	}
	if refused == nil {
		refused = fmt.Errorf("%s has no units", r.Resource)
	}
	return format.Lease{}, refused
}

func takeUnit(l workspace.Layout, r Request, unit string, now time.Time) (format.Lease, error) {
	if n, err := strconv.Atoi(unit); err != nil || n < 1 {
		return format.Lease{}, fmt.Errorf("%q is not a unit: a unit is a number from 1 up", unit)
	}
	path := l.LeaseFile(r.Resource, unit)
	lease := format.Lease{
		Resource: r.Resource,
		Unit:     unit,
		Job:      r.Job,
		Since:    now.UTC().Format(format.RFC3339UTC),
		Expires:  now.Add(r.TTL).UTC().Format(format.RFC3339UTC),
	}
	encoded, err := format.Encode(lease)
	if err != nil {
		return format.Lease{}, err
	}

	err = atomic.Replace(path, func(current []byte) ([]byte, error) {
		if current == nil {
			return encoded, nil
		}
		var held format.Lease
		if err := format.DecodeLoose(current, &held); err != nil {
			return nil, fmt.Errorf("%s does not decode: %w", path, err)
		}
		free, err := Free(l, held, now)
		if err != nil {
			return nil, err
		}
		if !free {
			return nil, fmt.Errorf("%s.%s is held for %s until %s", r.Resource, unit, held.Job, held.Expires)
		}
		// Taking an expired record means overwriting it.
		return encoded, nil
	})
	if err != nil {
		return format.Lease{}, err
	}
	return lease, nil
}

// Renew extends a lease its holder still has.
func Renew(l workspace.Layout, r Request, now time.Time) (format.Lease, error) {
	path := l.LeaseFile(r.Resource, r.Unit)
	var out format.Lease
	err := atomic.Replace(path, func(current []byte) ([]byte, error) {
		if current == nil {
			return nil, fmt.Errorf("%s.%s is not held", r.Resource, r.Unit)
		}
		var held format.Lease
		if err := format.DecodeLoose(current, &held); err != nil {
			return nil, err
		}
		if held.Job != r.Job {
			return nil, fmt.Errorf("%s.%s is held for %s, not %s", r.Resource, r.Unit, held.Job, r.Job)
		}
		held.Expires = now.Add(r.TTL).UTC().Format(format.RFC3339UTC)
		out = held
		return format.Encode(held)
	})
	return out, err
}

// Release deletes a lease, and only the job that holds it may. Releasing early means
// deleting the file.
func Release(l workspace.Layout, resource, unit, job string) error {
	if job == "" {
		return fmt.Errorf("a lease is released by the job that holds it, so releasing one names a job")
	}
	path := l.LeaseFile(resource, unit)
	return atomic.Replace(path, func(current []byte) ([]byte, error) {
		if current == nil {
			return nil, nil
		}
		var held format.Lease
		if err := format.DecodeLoose(current, &held); err != nil {
			return nil, err
		}
		if held.Job != job {
			return nil, fmt.Errorf("%s.%s is held for %s, not %s", resource, unit, held.Job, job)
		}
		return nil, nil
	})
}

// Held returns every lease on this site, in a stable order.
func Held(l workspace.Layout) ([]format.Lease, error) {
	paths, err := filepath.Glob(filepath.Join(l.Lease(), "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	out := make([]format.Lease, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var lease format.Lease
		if err := format.DecodeLoose(raw, &lease); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, lease)
	}
	return out, nil
}

// Capacity is how many holders a resource allows at once. An omitted capacity means
// one, which means exclusive.
func Capacity(l workspace.Layout, resource string) (int, error) {
	declared, err := declarations(l)
	if err != nil {
		return 0, err
	}
	entry, ok := declared[resource]
	if !ok {
		return 0, fmt.Errorf("%q is not declared in doc/resource/, and a name that resolves to no entry is a name with no referent", resource)
	}
	if entry.Capacity < 1 {
		return 1, nil
	}
	return entry.Capacity, nil
}

// free reports whether a lease may be taken: once its time has passed, or once the job
// holding it has closed, it is as good as gone.
func Free(l workspace.Layout, lease format.Lease, now time.Time) (bool, error) {
	if t, err := format.ParseUTC(lease.Expires); err != nil || t.Before(now) {
		return true, nil
	}
	if lease.Job == "" {
		return true, nil
	}
	if _, err := os.Stat(l.Brief(lease.Job)); err == nil {
		return false, nil // the job is live, so the promise stands
	}
	return true, nil // the job has left doc/task/, so the lease died with it
}

// declarations reads every registry into one name space, because a name is unique
// across every kind.
func declarations(l workspace.Layout) (map[string]format.Resource, error) {
	out := map[string]format.Resource{}
	kinds, err := resourceKinds(l)
	if err != nil {
		return out, err
	}
	for _, kind := range kinds {
		entries, err := workspace.ReadResources(l, kind)
		if err != nil {
			return nil, err
		}
		for name, e := range entries {
			out[name] = e
		}
	}
	return out, nil
}

func resourceKinds(l workspace.Layout) ([]string, error) {
	dirs, err := os.ReadDir(l.Resource())
	if err != nil {
		return nil, nil
	}
	var kinds []string
	for _, d := range dirs {
		name := d.Name()
		if d.IsDir() || filepath.Ext(name) != ".yaml" {
			continue
		}
		kinds = append(kinds, name[:len(name)-len(".yaml")])
	}
	sort.Strings(kinds)
	return kinds, nil
}
