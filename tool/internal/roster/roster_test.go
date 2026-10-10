package roster_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

func fixture(t *testing.T, site string) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  site,
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// Being a name is being an actor: the first time it is used, it exists, and nothing
// else about it is recorded but when and where it was heard from.
func TestUsingANameMakesItExist(t *testing.T) {
	l := fixture(t, "build-01")
	at := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)

	if err := roster.Seen(l, "claude-opus", "add-retry", at); err != nil {
		t.Fatal(err)
	}
	entries, err := roster.Local(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the roster: %+v", entries)
	}
	e := entries[0]
	if e.Name != "claude-opus" || e.Site != "build-01" || e.Job != "add-retry" {
		t.Errorf("what was recorded: %+v", e)
	}
	if e.FirstSeen != "2026-10-09T02:00:00Z" || e.FirstSeen != e.LastSeen {
		t.Errorf("first and last seen: %+v", e)
	}

	// Seeing it again moves the last hearing, and leaves the first where it was.
	later := at.Add(3 * time.Hour)
	if err := roster.Seen(l, "claude-opus", "", later); err != nil {
		t.Fatal(err)
	}
	entries, _ = roster.Local(l)
	if len(entries) != 1 {
		t.Fatalf("a name seen twice should be one entry: %+v", entries)
	}
	if entries[0].FirstSeen != "2026-10-09T02:00:00Z" || entries[0].LastSeen != "2026-10-09T05:00:00Z" {
		t.Errorf("seen twice: %+v", entries[0])
	}
	// A job it was not heard about this time stays as it was: a hint is not a claim.
	if entries[0].Job != "add-retry" {
		t.Errorf("the job hint was lost: %+v", entries[0])
	}

	if err := roster.Seen(l, "Not A Slug", "", at); err == nil {
		t.Error("a name is a slug")
	}
}

// Where an actor is comes from who heard from it most recently, and nothing expires: a
// person does not expire, and a name not heard from for a year is still a name.
func TestWhereAnActorIsIsTheNewestHearing(t *testing.T) {
	early := []roster.Entry{
		{Name: "claude-opus", Site: "build-01", FirstSeen: "2026-10-01T00:00:00Z", LastSeen: "2026-10-01T00:00:00Z"},
		{Name: "claude-sonnet", Site: "build-01", FirstSeen: "2026-10-01T00:00:00Z", LastSeen: "2026-10-05T00:00:00Z"},
	}
	late := []roster.Entry{
		{Name: "claude-opus", Site: "lab-02", FirstSeen: "2026-10-08T00:00:00Z", LastSeen: "2026-10-08T00:00:00Z"},
	}

	merged := roster.Merge("build-01", early, late)
	if len(merged) != 2 {
		t.Fatalf("merged: %+v", merged)
	}
	// The move shows: the newest hearing wins, and the older site is not a second actor.
	entry, ok := roster.Find("claude-opus", early, late)
	if !ok || entry.Site != "lab-02" {
		t.Errorf("an actor that moved should be where it was last heard from: %+v", entry)
	}
	// An actor heard from nowhere recently is still an actor.
	if entry, ok := roster.Find("claude-sonnet", early, late); !ok || entry.Site != "build-01" {
		t.Errorf("a quiet actor is still an actor: %+v", entry)
	}
	if _, ok := roster.Find("nobody", early, late); ok {
		t.Error("a name nobody has seen is not in the roster")
	}
}
