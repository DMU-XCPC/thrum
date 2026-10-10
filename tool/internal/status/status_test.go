package status_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/status"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// What comes next is a fact about a job that is still meant to happen, so the board
// prints it for a live job and not for a closed one. Printing it for a closed job reads
// as work waiting to be done, which is the one thing it is not.
func TestTheBoardOnlySaysWhatComesNextForLiveJobs(t *testing.T) {
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "build-01",
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}

	writeJob(t, l, "live-job", "doc/task", "carry on")
	writeJob(t, l, "closed-job", "doc/archive/task", "compare against the baseline")

	report, err := status.Gather(l)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	report.Print(&out)

	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "closed-job") && strings.Contains(line, "next:") {
			t.Errorf("a closed job should not advertise what comes next: %s", line)
		}
		if strings.Contains(line, "live-job") && !strings.Contains(line, "carry on") {
			t.Errorf("a live job's next stretch should be shown: %s", line)
		}
	}
}

// writeJob writes a brief and the handoff that gives it a next stretch.
func writeJob(t *testing.T, l workspace.Layout, job, dir, next string) {
	t.Helper()
	briefDir := filepath.Join(l.Root, dir, job)
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: " + job + "\nstatus: active\nbranch: job/" + job + "\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(filepath.Join(briefDir, "brief.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}

	handoffDir := l.HandoffDir(job)
	if err := os.MkdirAll(handoffDir, 0o755); err != nil {
		t.Fatal(err)
	}
	handoff := "---\n" +
		"job: " + job + "\n" +
		"agent: tester\n" +
		"branch: job/" + job + "\n" +
		"base: 1111111111111111111111111111111111111111\n" +
		"head: 1111111111111111111111111111111111111111\n" +
		"status: partial\n" +
		"started: 2026-10-09T02:10:00Z\n" +
		"ended: 2026-10-09T03:00:00Z\n" +
		"next: " + next + "\n" +
		"---\n\n## What changed\n\n## What was verified\n\n## What remains\n\n## Open questions\n"
	if err := os.WriteFile(l.HandoffFile(job, "2026-10-09T02-10-00Z.md"), []byte(handoff), 0o644); err != nil {
		t.Fatal(err)
	}
}
