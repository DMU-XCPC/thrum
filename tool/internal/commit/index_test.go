package commit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// An event records the bytes that were staged for it, not whatever the working tree holds
// at the moment it commits. Without that, a change somebody else made to the same file
// while the event was being recorded would be recorded under this event's name.
func TestAnEventRecordsWhatItStaged(t *testing.T) {
	l := scratch(t)
	path := filepath.Join(l.Topic(), "note.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepare(l, []string{"doc/topic/note.md"}); err != nil {
		t.Fatal(err)
	}
	// Somebody else's hand edit arrives after this event staged its paths.
	if err := os.WriteFile(path, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := commitNow(l, "topic note: mine")
	if err != nil {
		t.Fatal(err)
	}
	recorded := gitOutput(t, l.Root, "show", id+":doc/topic/note.md")
	if recorded != "mine\n" {
		t.Errorf("the event should have recorded what it staged, and recorded %q", recorded)
	}
	if onDisk, _ := os.ReadFile(path); string(onDisk) != "theirs\n" {
		t.Errorf("the other writer's change should still be in the working tree, and is %q", onDisk)
	}
}

// A change this event did not make is refused rather than recorded under its name.
func TestAnEventRefusesWhatTheIndexHoldsAndItDidNotMake(t *testing.T) {
	l := scratch(t)
	mine := filepath.Join(l.Topic(), "mine.md")
	theirs := filepath.Join(l.Topic(), "theirs.md")
	for _, p := range []string{mine, theirs} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(theirs, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The other actor staged their own file and has not recorded it yet.
	if _, err := gitx.Run(l.Root, "add", "--", "doc/topic/theirs.md"); err != nil {
		t.Fatal(err)
	}

	err := prepare(l, []string{"doc/topic/mine.md"})
	if err == nil {
		t.Fatal("an index holding somebody else's change should be refused")
	}
	if !strings.Contains(err.Error(), "doc/topic/theirs.md") {
		t.Errorf("the refusal should name what is in the way: %v", err)
	}
	if _, err := gitx.Run(l.Root, "rev-parse", "--verify", "HEAD"); err != nil {
		t.Fatal(err)
	}
	out := gitOutput(t, l.Root, "log", "--pretty=%s")
	if strings.Contains(out, "mine") {
		t.Errorf("nothing should have been recorded:\n%s", out)
	}
}

// Nothing to record is a sentence rather than git's "nothing to commit".
func TestAnEventWithNothingToRecordSaysSo(t *testing.T) {
	l := scratch(t)
	path := filepath.Join(l.Topic(), "same.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepare(l, []string{"doc/topic/same.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := commitNow(l, "topic same: first"); err != nil {
		t.Fatal(err)
	}
	if err := prepare(l, []string{"doc/topic/same.md"}); err == nil {
		t.Fatal("recording the same bytes twice should say there is nothing to record")
	} else if !strings.Contains(err.Error(), "nothing to record") {
		t.Errorf("the message should say that: %v", err)
	}
}

func scratch(t *testing.T) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "build-01",
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitx.Run(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}
