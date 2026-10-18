package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type daemonProc struct {
	cmd  *exec.Cmd
	out  *bytes.Buffer
	once sync.Once
}

// startDaemon runs a site's daemon and waits until its local socket answers.
func startDaemon(t *testing.T, w *workspace, listen string) *daemonProc {
	t.Helper()
	return startDaemonWith(t, w, listen, false)
}

// startDaemonDiscover runs a daemon with the LAN hint on, for the discovery tests.
func startDaemonDiscover(t *testing.T, w *workspace, listen string) *daemonProc {
	t.Helper()
	return startDaemonWith(t, w, listen, true)
}

func startDaemonWith(t *testing.T, w *workspace, listen string, discover bool) *daemonProc {
	t.Helper()
	args := []string{"--root", w.root, "daemon", "run", "--listen", listen,
		"--route-interval", "200ms", "--discover=false"}
	if discover {
		args = []string{"--root", w.root, "daemon", "run", "--listen", listen,
			"--route-interval", "200ms", "--discover=true", "--discover-interval", "200ms"}
	}
	cmd := exec.Command(thrumBin, args...)
	cmd.Env = envWith("THRUM_ROOT="+w.root, "THRUM_ACTOR=a1")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the daemon: %v", err)
	}
	d := &daemonProc{cmd: cmd, out: &out}
	t.Cleanup(d.stop)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if w.r().run("daemon", "status").code == 0 {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon did not start: %s", out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (d *daemonProc) stop() {
	d.once.Do(func() {
		if d.cmd.Process != nil {
			_ = d.cmd.Process.Signal(os.Interrupt)
		}
		done := make(chan struct{})
		go func() {
			_ = d.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = d.cmd.Process.Kill()
		}
	})
}

func TestDaemonLifecycle(t *testing.T) {
	w := newWorkspace(t)
	d := startDaemon(t, w, "127.0.0.1:0")

	st, err := os.Stat(w.path("var", "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("the socket is %o, want 600", perm)
	}
	status := w.r().run("daemon", "status", "--json").ok(t, "status")
	if !strings.Contains(status.stdout, `"site":"s1"`) {
		t.Fatalf("status does not name the site: %s", status.stdout)
	}
	if !strings.Contains(status.stdout, `"version":"thrum/1"`) {
		t.Fatalf("status does not report the protocol: %s", status.stdout)
	}
	if doctor := w.r().run("site", "doctor").ok(t, "doctor"); !strings.Contains(doctor.stdout, "this site") {
		t.Fatalf("doctor does not recognize this site: %s", doctor.stdout)
	}

	w.r().run("daemon", "stop").ok(t, "stop")
	deadline := time.Now().Add(5 * time.Second)
	for w.r().run("daemon", "status").code == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the daemon is still running: %s", d.out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	d.stop()
}

func TestDaemonLock(t *testing.T) {
	w := newWorkspace(t)
	startDaemon(t, w, "127.0.0.1:0")
	// A second daemon on the same workspace must refuse, not fight for the socket.
	res := w.r().run("daemon", "run", "--listen", "127.0.0.1:0")
	res.fails(t, "a second daemon", "already running")
}

func TestDaemonStatusHolds(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")
	r.run("message", "send", "a2", "--subject", "held").ok(t, "send")
	startDaemon(t, w, "127.0.0.1:0")

	res := r.run("daemon", "status", "--json").ok(t, "status")
	var data struct {
		Held int `json:"held"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &data); err != nil {
		t.Fatalf("status json: %v\n%s", err, res.stdout)
	}
	if data.Held != 1 {
		t.Fatalf("the daemon reports %d messages, want 1", data.Held)
	}
}

func TestDoctorReportsPeerState(t *testing.T) {
	ts := setupTwoSites(t)
	p2 := ts.p2
	ts.w2.r().run("daemon", "stop").ok(t, "stop site two")
	if out := ts.w1.r().run("site", "doctor").ok(t, "doctor down"); !strings.Contains(out.stdout, "did not answer") {
		t.Fatalf("doctor with the peer down: %s", out.stdout)
	}
	startDaemon(t, ts.w2, p2)
	if out := ts.w1.r().run("site", "doctor").ok(t, "doctor up"); !strings.Contains(out.stdout, "answered") {
		t.Fatalf("doctor with the peer up: %s", out.stdout)
	}
	ts.w2.r().run("daemon", "stop").ok(t, "stop")
	if out := ts.w1.r().run("site", "doctor").ok(t, "doctor down again"); !strings.Contains(out.stdout, "did not answer") {
		t.Fatalf("doctor after the stop: %s", out.stdout)
	}
}

func TestEventsWatchStreams(t *testing.T) {
	w := newWorkspace(t)
	r := w.r()
	r.run("actor", "declare", "a1", "--kind", "agent").ok(t, "a1")
	r.run("actor", "declare", "a2", "--kind", "agent").ok(t, "a2")

	cmd := exec.Command(thrumBin, "--root", w.root, "events", "--watch")
	cmd.Env = envWith("THRUM_ROOT="+w.root, "THRUM_ACTOR=a2")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	lines := make(chan string, 8)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			lines <- s.Text()
		}
	}()
	time.Sleep(300 * time.Millisecond)
	id := strings.TrimSpace(r.as("a1").run("message", "send", "a2", "--subject", "watched").ok(t, "send").stdout)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ln := <-lines:
			if strings.Contains(ln, id) {
				return
			}
		case <-deadline:
			t.Fatal("the event stream did not deliver the message")
		}
	}
}
