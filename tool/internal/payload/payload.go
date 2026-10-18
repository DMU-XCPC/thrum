// Package payload owns the disposable bytes under job/<id>: the close's partial
// collection, the retention window, and the sweep the tools run.
package payload

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// DefaultWindow is the retention window when no convention sets collect.window.
const DefaultWindow = 14 * 24 * time.Hour

// Window reads collect.window from the conventions, or DefaultWindow.
func Window(l layout.Layout) (time.Duration, error) {
	entries, err := os.ReadDir(l.Conventions())
	if os.IsNotExist(err) {
		return DefaultWindow, nil
	}
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	found := false
	var window time.Duration
	for _, name := range names {
		if _, ok := format.ConventionFile(name); !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(l.Conventions(), name))
		if err != nil {
			return 0, err
		}
		vals, ok, err := format.Values(string(data))
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		if !ok {
			continue
		}
		s, ok := vals["collect.window"]
		if !ok {
			continue
		}
		if found {
			return 0, fmt.Errorf("collect.window is declared twice: %s and an earlier convention", name)
		}
		found = true
		window, err = format.ParseWindow(s)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
	}
	if !found {
		return DefaultWindow, nil
	}
	return window, nil
}

// AtClose removes the scratch a closed job no longer needs, unless it was kept.
func AtClose(l layout.Layout, job string, keep bool) error {
	if keep {
		return nil
	}
	for _, sub := range []string{"tmp", "cache", "ckpt"} {
		if err := os.RemoveAll(l.PayloadSub(job, sub)); err != nil {
			return err
		}
	}
	return nil
}

// Entry is one payload and whether it can be collected.
type Entry struct {
	Job           string `json:"job"`
	State         string `json:"state"`
	Bytes         int64  `json:"bytes"`
	Closed        string `json:"closed,omitempty"`
	CollectibleAt string `json:"collectible_at,omitempty"`
}

// Entries lists every payload under job/: live, kept, waiting, collectible, or an
// orphan with no brief.
func Entries(l layout.Layout) ([]Entry, error) {
	window, err := Window(l)
	if err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(l.Payloads())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []Entry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		job := d.Name()
		e := Entry{Job: job, Bytes: bytesOf(l.Payload(job))}
		if _, ok, err := briefAt(filepath.Join(l.TaskDir(job), "brief.md")); err != nil {
			return nil, err
		} else if ok {
			e.State = "in use"
			out = append(out, e)
			continue
		}
		b, ok, err := briefAt(filepath.Join(l.ArchivedTaskDir(job), "brief.md"))
		if err != nil {
			return nil, err
		}
		if !ok {
			e.State = "orphan"
			out = append(out, e)
			continue
		}
		if b.Keep {
			e.State = "kept"
			out = append(out, e)
			continue
		}
		closed, err := closedAt(l, filepath.Join(l.ArchivedTaskDir(job), "brief.md"))
		if err != nil {
			return nil, err
		}
		e.Closed = closed.UTC().Format(time.RFC3339)
		at := closed.Add(window)
		e.CollectibleAt = at.UTC().Format(time.RFC3339)
		if !now.Before(at) {
			e.State = "collectible"
		} else {
			e.State = "waiting"
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Job < out[j].Job })
	return out, nil
}

// Sweep removes every collectible payload and returns the job ids.
func Sweep(l layout.Layout) ([]string, error) {
	entries, err := Entries(l)
	if err != nil {
		return nil, err
	}
	var collected []string
	for _, e := range entries {
		if e.State != "collectible" {
			continue
		}
		if err := os.RemoveAll(l.Payload(e.Job)); err != nil {
			return collected, err
		}
		collected = append(collected, e.Job)
	}
	return collected, nil
}

func briefAt(path string) (records.Brief, bool, error) {
	front, _, err := format.LoadDocument(path)
	if os.IsNotExist(err) {
		return records.Brief{}, false, nil
	}
	if err != nil {
		return records.Brief{}, false, err
	}
	var b records.Brief
	if err := format.Decode(front, &b); err != nil {
		return records.Brief{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return b, true, nil
}

// closedAt is the time of the line commit that moved the brief to the archive. The
// records do not duplicate what git already records.
func closedAt(l layout.Layout, brief string) (time.Time, error) {
	out, err := gitx.Run(l.Root, "log", "-1", "--format=%ct", "--", brief)
	if err != nil {
		return time.Time{}, err
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("the close of %s has no recorded time", brief)
	}
	return time.Unix(secs, 0), nil
}

func bytesOf(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
