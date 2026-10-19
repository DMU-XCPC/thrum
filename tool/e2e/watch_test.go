package e2e

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// startWatch runs a streaming command and gives the test one line at a time.
func startWatch(t *testing.T, w *workspace, actor string, args ...string) *watchProc {
	t.Helper()
	full := append([]string{"--root", w.root}, args...)
	cmd := exec.Command(thrumBin, full...)
	cmd.Env = envWith("THRUM_ROOT="+w.root, "THRUM_ACTOR="+actor)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 32)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return &watchProc{lines: lines}
}

type watchProc struct {
	lines chan string
	stdin io.WriteCloser
}

func (p *watchProc) send(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(p.stdin, line+"\n"); err != nil {
		t.Fatalf("writing to the watch: %v", err)
	}
}

func startWatchCmd(t *testing.T, w *workspace, actor string, args ...string) *watchProc {
	t.Helper()
	full := append([]string{"--root", w.root}, args...)
	cmd := exec.Command(thrumBin, full...)
	cmd.Env = envWith("THRUM_ROOT="+w.root, "THRUM_ACTOR="+actor)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 32)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return &watchProc{lines: lines, stdin: stdin}
}

func (p *watchProc) next(t *testing.T) string {
	t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok {
			t.Fatal("the watch ended")
		}
		return line
	case <-time.After(10 * time.Second):
		t.Fatal("no line from the watch")
		return ""
	}
}

// TestMessageListWatch: the backlog first, then each arrival, one JSON message per
// line. This is the interface the harness plugin's feed uses.
func TestMessageListWatch(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.as("a1").run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	r.as("a2").run("message", "send", "a1", "--subject", "backlog").ok(t, "first")

	p := startWatch(t, w, "a1", "message", "list", "--watch", "--json")
	if line := p.next(t); !strings.Contains(line, "backlog") {
		t.Fatalf("the backlog was not printed first: %s", line)
	}
	r.as("a2").run("message", "send", "a1", "--subject", "arrived").ok(t, "second")
	if line := p.next(t); !strings.Contains(line, "arrived") {
		t.Fatalf("the arrival was not streamed: %s", line)
	}
}

// TestEventsStream: the top-level event stream emits a message.delivered event.
func TestEventsStream(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.as("a1").run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.as("a2").run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	p := startWatch(t, w, "a1", "events", "--json", "--watch")
	r.as("a2").run("message", "send", "a1", "--subject", "event").ok(t, "send")
	line := p.next(t)
	if !strings.Contains(line, `"kind":"message.delivered"`) || !strings.Contains(line, "event") {
		t.Fatalf("the event line: %s", line)
	}
}

// TestThrumWatch: the two-way stream. One process, a negotiated scope, and messages
// routed by actor.
func TestThrumWatch(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")

	p := startWatchCmd(t, w, "a1", "watch")
	p.send(t, `{"op":"watch","actor":"a1"}`)
	if line := p.next(t); !strings.Contains(line, `"kind":"watching"`) || !strings.Contains(line, `"actor":"a1"`) {
		t.Fatalf("watch confirmation: %s", line)
	}
	p.send(t, `{"op":"scope","actors":["a1"]}`)
	if line := p.next(t); !strings.Contains(line, `"kind":"scope"`) {
		t.Fatalf("scope confirmation: %s", line)
	}
	r.as("a2").run("message", "send", "a1", "--subject", "x").ok(t, "send")
	if line := p.next(t); !strings.Contains(line, `"kind":"message.delivered"`) || !strings.Contains(line, `"actor":"a1"`) {
		t.Fatalf("delivery: %s", line)
	}
	p.send(t, `{"op":"watch","actor":"ghost"}`)
	if line := p.next(t); !strings.Contains(line, `"kind":"error"`) {
		t.Fatalf("undeclared actor: %s", line)
	}
	p.send(t, `{"op":"unwatch","actor":"a1"}`)
	if line := p.next(t); !strings.Contains(line, `"kind":"unwatching"`) {
		t.Fatalf("unwatch confirmation: %s", line)
	}
	p.send(t, `{"op":"bogus"}`)
	if line := p.next(t); !strings.Contains(line, `"kind":"error"`) {
		t.Fatalf("bad op: %s", line)
	}
}
