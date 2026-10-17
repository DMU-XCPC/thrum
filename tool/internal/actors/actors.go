// Package actors owns a name's standing and the last-known place of an actor.
package actors

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// Declare writes a name's standing, once. A name is never reused.
func Declare(l layout.Layout, name, kind, about, home string, now time.Time) error {
	if !format.ValidSlug(name) {
		return fmt.Errorf("%q is not an actor name: an actor name is a slug", name)
	}
	if kind != "human" && kind != "agent" {
		return fmt.Errorf("%q is not a kind of actor: use human or agent", kind)
	}
	if _, ok, err := Find(l, name); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("actor %q is already declared", name)
	}
	if home == "" {
		home = Site(l)
	}
	decl := records.Actor{
		Actor:      name,
		Kind:       kind,
		About:      about,
		Home:       home,
		Registered: now.UTC().Format(format.RFC3339UTC),
	}
	if err := os.MkdirAll(l.Actors(), 0o755); err != nil {
		return err
	}
	if err := records.Write(l.Actor(name), decl); err != nil {
		return err
	}
	return line.Commit(l.Root, []string{l.Actor(name)},
		fmt.Sprintf("actor %s: declared as a %s", name, kind))
}

// Find returns a name's standing, live or archived.
func Find(l layout.Layout, name string) (records.Actor, bool, error) {
	var a records.Actor
	ok, err := records.Read(l.Actor(name), &a)
	if err != nil || ok {
		return a, ok, err
	}
	ok, err = records.Read(l.ArchivedActor(name), &a)
	return a, ok, err
}

// List returns every name that exists, in name order.
func List(l layout.Layout) ([]records.Actor, error) {
	entries, err := os.ReadDir(l.Actors())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []records.Actor
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		var a records.Actor
		ok, err := records.Read(filepath.Join(l.Actors(), e.Name()), &a)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Actor < out[j].Actor })
	return out, nil
}

// Retire moves a name's standing to the archive. The name stays taken.
func Retire(l layout.Layout, name string, now time.Time) error {
	a, ok, err := Find(l, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no actor named %q", name)
	}
	if a.Retired != "" {
		return fmt.Errorf("actor %q is already retired", name)
	}
	a.Retired = now.UTC().Format(format.DayForm)
	archived := l.ArchivedActor(name)
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		return err
	}
	if err := records.Write(archived, a); err != nil {
		return err
	}
	if err := os.Remove(l.Actor(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return line.Commit(l.Root, []string{l.Actor(name), archived},
		fmt.Sprintf("retire actor/%s: retired", name))
}

// Hello records where an actor's mail is now. It is an address, not a claim.
func Hello(l layout.Layout, name string, now time.Time) error {
	if _, ok, err := Find(l, name); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("actor %q is not declared", name)
	}
	return SetPlace(l, name, Site(l), now)
}

// SetPlace caches where a name was last heard. It is an address, not a claim that
// the actor is working.
func SetPlace(l layout.Layout, name, site string, now time.Time) error {
	return SetPlaceAt(l, name, site, now.UTC().Format(format.RFC3339UTC))
}

// SetPlaceAt records a place only if it is at least as new as the one already
// known, so a late broadcast cannot undo a later hello.
func SetPlaceAt(l layout.Layout, name, site, heard string) error {
	places := Places(l)
	if places.Actors == nil {
		places.Actors = map[string]records.Place{}
	}
	if existing, ok := places.Actors[name]; ok && existing.Heard > heard {
		return nil
	}
	places.Actors[name] = records.Place{Site: site, Heard: heard}
	if err := os.MkdirAll(l.Var(), 0o755); err != nil {
		return err
	}
	return records.Write(l.Locations(), places)
}

// Places is the last-known place of every actor this site has heard from.
func Places(l layout.Layout) records.Places {
	var p records.Places
	_, _ = records.Read(l.Locations(), &p)
	return p
}

// Site is the name this machine's workspace goes by.
func Site(l layout.Layout) string {
	b, err := os.ReadFile(l.SiteFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
