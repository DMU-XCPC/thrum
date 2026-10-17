package e2e

import (
	"bytes"
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
	cmd := exec.Command(thrumBin, "--root", w.root, "daemon", "run", "--listen", listen)
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
