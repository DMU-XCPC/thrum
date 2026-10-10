package format

import (
	"errors"
	"testing"
)

func TestSplitJoinRoundTrip(t *testing.T) {
	docs := []string{
		"---\njob: x\n---\n## Goal\n\nbody\n",
		"---\n---\n",
		"---\na: 1\n---\n",
		"---\nlist:\n  - one\n  - two\n---\n# Title\n\ntext with --- inside\n",
	}
	for _, d := range docs {
		front, body, err := Split([]byte(d))
		if err != nil {
			t.Fatalf("Split(%q): %v", d, err)
		}
		if got := string(Join(front, body)); got != d {
			t.Errorf("round trip changed the document:\n got %q\nwant %q", got, d)
		}
	}
}

func TestSplitRefusesPureYAML(t *testing.T) {
	_, _, err := Split([]byte("worktree:\n  - name: uploader\n"))
	if !errors.Is(err, ErrNoFrontMatter) {
		t.Fatalf("want ErrNoFrontMatter, got %v", err)
	}
}

func TestSplitRefusesAnUnclosedBlock(t *testing.T) {
	if _, _, err := Split([]byte("---\njob: x\n")); !errors.Is(err, ErrNoFrontMatter) {
		t.Fatalf("want ErrNoFrontMatter, got %v", err)
	}
}

func TestDecodeRefusesUnknownFields(t *testing.T) {
	var b Brief
	if err := Decode([]byte("job: x\ninvented: 1\n"), &b); err == nil {
		t.Fatal("a field no record defines should be refused")
	}
}

func TestStampAndUTCConvertBothWays(t *testing.T) {
	utc, err := StampToUTC("2026-10-09T02-10-00Z")
	if err != nil {
		t.Fatal(err)
	}
	if utc != "2026-10-09T02:10:00Z" {
		t.Fatalf("got %q", utc)
	}
	back, err := UTCToStamp(utc)
	if err != nil {
		t.Fatal(err)
	}
	if back != "2026-10-09T02-10-00Z" {
		t.Fatalf("got %q", back)
	}
}

func TestParseUTCRefusesAnOffset(t *testing.T) {
	// The design says UTC in file contents: a record that says 02:10+08:00 is a
	// different statement from one that says 18:10Z, even though the instant matches.
	if _, err := ParseUTC("2026-10-09T02:10:00+08:00"); err == nil {
		t.Fatal("an offset should be refused")
	}
}

func TestNamingRules(t *testing.T) {
	good := []string{"a", "add-retry-to-uploader", "spike-gc", "2026-10-09"}
	bad := []string{"", "Upper", "under_score", "dot.ted", "-lead", "trail-", "two--hyphens"}
	for _, s := range good {
		if !ValidSlug(s) {
			t.Errorf("%q should be a slug", s)
		}
	}
	for _, s := range bad {
		if ValidSlug(s) {
			t.Errorf("%q should not be a slug", s)
		}
	}

	if n, ok := ConventionFile("03-naming.md"); !ok || n != 3 {
		t.Errorf("convention number: got %d, %v", n, ok)
	}
	if _, ok := ConventionFile("3-naming.md"); ok {
		t.Error("a one-digit convention number should be refused")
	}

	res, unit, err := SplitLeaseFileName("gpu.2.yaml")
	if err != nil || res != "gpu" || unit != "2" {
		t.Errorf("lease name: %q %q %v", res, unit, err)
	}
}
