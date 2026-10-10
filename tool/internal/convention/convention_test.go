package convention_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/convention"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// A convention is where a project's policy reaches the tooling, so a value in one is read
// by name and acted on.
func TestAValueIsReadByTheNameAToolLooksFor(t *testing.T) {
	l := fixture(t)
	write(t, l, "06-what-survives.md", "# What survives\n\nKept for a while.\n\n```values\ncollect.window: 14d\n```\n")

	values, problems, err := convention.Read(l)
	if err != nil || len(problems) != 0 {
		t.Fatalf("reading: %v %v", problems, err)
	}
	if values["collect.window"] != "14d" {
		t.Fatalf("the value should be read: %+v", values)
	}
	window, err := values.Duration("collect.window", time.Hour)
	if err != nil || window != 14*24*time.Hour {
		t.Errorf("days are how policy is written: %v %v", window, err)
	}
	// A key the workspace says nothing about is the caller's default, not an error.
	if window, err := values.Duration("nothing.reads.this", 3*time.Hour); err != nil || window != 3*time.Hour {
		t.Errorf("an absent value is the default: %v %v", window, err)
	}
	// Go's own forms are read too.
	write(t, l, "06-what-survives.md", "# What survives\n\n```values\ncollect.window: 336h\n```\n")
	if values, _, _ := convention.Read(l); mustDuration(t, values, "collect.window") != 336*time.Hour {
		t.Error("336h should be read as well as 14d")
	}
}

// An example of a values block is not a declaration, which is why the fence opens at the
// left margin: a fence inside a list item is indented.
func TestAnIndentedFenceDeclaresNothing(t *testing.T) {
	l := fixture(t)
	write(t, l, "07-writing-one.md", "# Writing one\n\nA value looks like this:\n\n- the block:\n\n  ```values\n  collect.window: 14d\n  ```\n")

	values, problems, err := convention.Read(l)
	if err != nil || len(problems) != 0 {
		t.Fatalf("reading: %v %v", problems, err)
	}
	if len(values) != 0 {
		t.Errorf("an indented example should declare nothing: %+v", values)
	}
}

// The workspace cannot say two things at once, and a value nobody can read is not a value.
func TestWhatCannotBeReadIsReported(t *testing.T) {
	cases := []struct {
		name string
		file string
		want string
	}{
		{"two blocks", "# Two\n\n```values\ncollect.window: 14d\n```\n\n```values\ncollect.window: 7d\n```\n", "one values block"},
		{"two conventions, one key", "", "cannot say two things at once"},
		{"a key that is not dotted", "# Bare\n\n```values\nwindow: 14d\n```\n", "dotted slug"},
		{"a value that is not scalar", "# Nested\n\n```values\ncollect.window:\n  days: 14\n```\n", "not a scalar"},
		{"a known key in the wrong shape", "# Soon\n\n```values\ncollect.window: soon\n```\n", "reads it as a length of time"},
		{"a block that is not YAML", "# Bad\n\n```values\ncollect.window: [14d\n```\n", "not YAML"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := fixture(t)
			if c.name == "two conventions, one key" {
				write(t, l, "01-one.md", "# One\n\n```values\ncollect.window: 14d\n```\n")
				write(t, l, "02-two.md", "# Two\n\n```values\ncollect.window: 7d\n```\n")
			} else {
				write(t, l, "01-one.md", c.file)
			}
			values, problems, err := convention.Read(l)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) == 0 {
				t.Fatalf("this should have been reported: %+v", values)
			}
			joined := ""
			for _, p := range problems {
				joined += p.String() + "\n"
			}
			if !strings.Contains(joined, c.want) {
				t.Errorf("the report should mention %q, and says:\n%s", c.want, joined)
			}
		})
	}
}

// A known value the tooling cannot read is kept, so that a tool which reads it fails
// rather than quietly using its own default.
func TestAnUnreadableKnownValueIsNotSilentlyDropped(t *testing.T) {
	l := fixture(t)
	write(t, l, "01-soon.md", "# Soon\n\n```values\ncollect.window: soon\n```\n")

	values, problems, err := convention.Read(l)
	if err != nil || len(problems) == 0 {
		t.Fatalf("reading: %v %v", problems, err)
	}
	if _, err := values.Duration("collect.window", time.Hour); err == nil {
		t.Error("a tool reading this value should fail rather than use its default")
	}
}

// A workspace with no conventions says nothing, and that is not a problem.
func TestAWorkspaceWithNoConventionsIsFine(t *testing.T) {
	l := fixture(t)
	values, problems, err := convention.Read(l)
	if err != nil || len(problems) != 0 || len(values) != 0 {
		t.Errorf("nothing declared, nothing wrong: %+v %v %v", values, problems, err)
	}
}

func mustDuration(t *testing.T, values convention.Values, key string) time.Duration {
	t.Helper()
	d, err := values.Duration(key, 0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fixture(t *testing.T) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "build-01",
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The template's own conventions would otherwise be read along with the test's.
	entries, err := os.ReadDir(l.Convention())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(l.Convention(), e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func write(t *testing.T, l workspace.Layout, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(l.Convention(), name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
