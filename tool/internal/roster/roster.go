// Package roster is what the tooling knows about names.
//
// An actor is a name, and becoming one takes nothing but using one. Which names exist,
// where each was last heard from, and which job each says it is on is therefore the
// tooling's knowledge rather than a file in the workspace: the workspace records names
// and claims, and this records what has been seen.
//
// It is never used to refuse anything. A name that has been seen is not thereby
// entitled to do anything, and a name that has not is not thereby forbidden — the
// roster answers questions, and nothing else.
package roster

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/atomic"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"gopkg.in/yaml.v3"
)

// Entry is one name this site has seen.
type Entry struct {
	Name      string `yaml:"name" json:"name"`
	Site      string `yaml:"site" json:"site"`
	FirstSeen string `yaml:"first_seen" json:"first_seen"`
	LastSeen  string `yaml:"last_seen" json:"last_seen"`
	// Job is what the name says it is on, which is what it last worked: a hint, not a
	// claim. The claim is `var/occupy/`.
	Job string `yaml:"job,omitempty" json:"job,omitempty"`
}

// Seen records that a name did something here, making it exist if it did not.
//
// This is the whole of registration: there is no join, no invite and no list to be
// added to, because being a name is being an actor.
func Seen(l workspace.Layout, name, job string, now time.Time) error {
	if !format.ValidSlug(name) {
		return fmt.Errorf("%q is not an actor name: an actor name is a slug", name)
	}
	stamp := now.UTC().Format(format.RFC3339UTC)

	return atomic.Replace(l.ActorRoster(), func(current []byte) ([]byte, error) {
		entries, err := decode(current)
		if err != nil {
			return nil, err
		}
		found := false
		for i := range entries {
			if entries[i].Name != name {
				continue
			}
			found = true
			entries[i].Site = Site(l)
			entries[i].LastSeen = stamp
			if job != "" {
				entries[i].Job = job
			}
		}
		if !found {
			entries = append(entries, Entry{
				Name:      name,
				Site:      Site(l),
				FirstSeen: stamp,
				LastSeen:  stamp,
				Job:       job,
			})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		return yaml.Marshal(map[string][]Entry{"actors": entries})
	})
}

// Local is what this site has seen, in name order.
func Local(l workspace.Layout) ([]Entry, error) {
	raw, err := os.ReadFile(l.ActorRoster())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decode(raw)
}

// Site is the name this machine's workspace goes by.
func Site(l workspace.Layout) string {
	raw, err := os.ReadFile(l.SiteFile())
	if err != nil {
		return ""
	}
	return string(trimNewline(raw))
}

// Merge answers "where is this actor" from what several sites said.
//
// The site that has heard from a name most recently is where it is: an actor that moves
// leaves a trace behind it, and the newest trace is the true one. Nothing expires,
// because a person does not expire, and a name that has not been heard from for a year
// is still a name.
func Merge(site string, lists ...[]Entry) []Entry {
	newest := map[string]Entry{}
	for _, list := range lists {
		for _, e := range list {
			if e.Site == "" {
				e.Site = site
			}
			prev, seen := newest[e.Name]
			if !seen || e.LastSeen > prev.LastSeen {
				newest[e.Name] = e
			}
		}
	}

	out := make([]Entry, 0, len(newest))
	for _, e := range newest {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Find returns one entry, from what several sites said.
func Find(name string, lists ...[]Entry) (Entry, bool) {
	for _, e := range Merge("", lists...) {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

func decode(raw []byte) ([]Entry, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var file struct {
		Actors []Entry `yaml:"actors"`
	}
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	return file.Actors, nil
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
