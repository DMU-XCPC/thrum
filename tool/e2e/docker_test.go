package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDockerMultiSite runs two sites as separate containers on one Docker network:
// a real git transport for the histories, and the daemon's own protocol for mail.
func TestDockerMultiSite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the Docker test in -short mode")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("docker is not available: %v\n%s", err, out)
	}
	if out, err := exec.Command("docker", "image", "inspect", "alpine/git:latest").CombinedOutput(); err != nil {
		t.Skipf("the alpine/git image is not present: %v\n%s", err, out)
	}

	const (
		network = "thrum-e2e-net"
		hub     = "thrum-e2e-hub"
		siteA   = "thrum-e2e-a"
		siteB   = "thrum-e2e-b"
	)
	dockerQuiet("rm", "-f", hub, siteA, siteB)
	dockerQuiet("network", "rm", network)

	base := t.TempDir()

	dockerOK(t, "network", "create", network)
	t.Cleanup(func() {
		dockerQuiet("rm", "-f", hub, siteA, siteB)
		dockerQuiet("network", "rm", network)
	})

	dockerOK(t, "run", "-d", "--name", hub, "--network", network,
		"--entrypoint", "sh", "alpine/git", "-c", "sleep infinity")
	for _, name := range []string{siteA, siteB} {
		dockerOK(t, "run", "-d", "--name", name, "--network", network,
			"--entrypoint", "sh", "alpine/git", "-c", "sleep infinity")
	}
	// Copy the two static binaries in, rather than bind-mounting a file onto a path
	// that does not exist (Docker would create a directory there).
	for _, c := range []struct{ name, binary, target string }{
		{hub, hubBin, "/usr/local/bin/thrum-hub"},
		{siteA, thrumBin, "/usr/local/bin/thrum"},
		{siteB, thrumBin, "/usr/local/bin/thrum"},
	} {
		dockerOK(t, "exec", c.name, "mkdir", "-p", "/usr/local/bin")
		dockerOK(t, "cp", c.binary, c.name+":"+c.target)
		dockerOK(t, "exec", c.name, "chmod", "+x", c.target)
	}
	dockerOK(t, "exec", hub, "mkdir", "-p", "/srv")
	for _, name := range []string{"line.git", "ledger.git", "store.git", "code.git"} {
		dockerOK(t, "exec", hub, "git", "init", "--bare", "-b", "main", "/srv/"+name)
	}
	dockerOK(t, "exec", siteA, "mkdir", "-p", "/w")
	dockerOK(t, "exec", siteB, "mkdir", "-p", "/w")
	dockerOK(t, "exec", "-d", hub, "/usr/local/bin/thrum-hub", "-root", "/srv", "-listen", "0.0.0.0:8080")
	waitFor(t, 10*time.Second, "the git hub", func() bool {
		_, err := dockerTry("exec", siteA, "sh", "-c", "git ls-remote http://"+hub+":8080/line.git")
		return err == nil
	})

	// Site one: a code source, a workspace, and the histories published to the hub.
	thrumIn(t, siteA, "sh", "-c",
		"git init -q -b main /tmp/code && cd /tmp/code && echo base > README && git add -A && git -c user.name=t -c user.email=t@t commit -qm init")
	thrumIn(t, siteA, "thrum", "create", "/w", "--site", "s1", "--code", "code=/tmp/code")
	thrumIn(t, siteA, "sh", "-c",
		"cd /w && git remote add origin http://"+hub+":8080/line.git && git push -q -u origin main")
	thrumIn(t, siteA, "sh", "-c",
		"cd /w/handoff && git remote add origin http://"+hub+":8080/ledger.git && git push -q -u origin main")
	thrumIn(t, siteA, "sh", "-c",
		"cd /w/repos/code.git && git push -q http://"+hub+":8080/code.git --all")
	thrumIn(t, siteA, "thrum", "--root", "/w", "--actor", "a1", "actor", "declare", "a1", "--kind", "agent")
	thrumIn(t, siteA, "sh", "-c",
		"cd /w/repos/store.git && git remote add origin http://"+hub+":8080/store.git")
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	token := strings.TrimSpace(thrumIn(t, siteA, "thrum", "--root", "/w", "site", "invite", "s2",
		"--at", siteB+":7702",
		"--line", "http://"+hub+":8080/line.git",
		"--ledger", "http://"+hub+":8080/ledger.git",
		"--store", "http://"+hub+":8080/store.git",
		"--code", "code=http://"+hub+":8080/code.git"))
	if token == "" {
		t.Fatal("invite printed no token")
	}
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")

	// Site two joins over the network and registers its key.
	thrumIn(t, siteB, "thrum", "--root", "/w", "--dir", "/w", "join", token)
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	if keyed := thrumIn(t, siteA, "thrum", "--root", "/w", "site", "show", "s2"); !strings.Contains(keyed, "ssh-ed25519") {
		t.Fatalf("site one did not learn site two's key: %s", keyed)
	}
	thrumIn(t, siteB, "thrum", "--root", "/w", "--actor", "a2", "actor", "declare", "a2", "--kind", "agent")
	thrumIn(t, siteB, "thrum", "--root", "/w", "sync")
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")

	// The two daemons, on their own ports, over the Docker network.
	dockerOK(t, "exec", "-d", siteA, "sh", "-c",
		"thrum --root /w daemon run --listen 0.0.0.0:7701 >/tmp/daemon.log 2>&1")
	dockerOK(t, "exec", "-d", siteB, "sh", "-c",
		"thrum --root /w daemon run --listen 0.0.0.0:7702 >/tmp/daemon.log 2>&1")
	waitFor(t, 10*time.Second, "site one's daemon", func() bool {
		_, err := dockerTry("exec", siteA, "thrum", "--root", "/w", "daemon", "status")
		return err == nil
	})
	waitFor(t, 10*time.Second, "site two's daemon", func() bool {
		_, err := dockerTry("exec", siteB, "thrum", "--root", "/w", "daemon", "status")
		return err == nil
	})
	thrumIn(t, siteA, "thrum", "--root", "/w", "site", "reach", "s2", "--at", siteB+":7702")
	thrumIn(t, siteB, "thrum", "--root", "/w", "site", "reach", "s1", "--at", siteA+":7701")
	thrumIn(t, siteA, "thrum", "--root", "/w", "--actor", "a1", "actor", "hello")

	// A message from site two to an actor who lives at site one.
	thrumIn(t, siteB, "sh", "-c",
		"echo it-crossed-sites | thrum --root /w --actor a2 message send a1 --subject 'docker hello' --body -")
	mail := thrumIn(t, siteA, "thrum", "--root", "/w", "--actor", "a1", "message", "list")
	if !strings.Contains(mail, "docker hello") || !strings.Contains(mail, "a2") {
		t.Fatalf("the cross-site message did not arrive: %s", mail)
	}
	doctor := thrumIn(t, siteB, "thrum", "--root", "/w", "site", "doctor")
	if !strings.Contains(doctor, "answered") {
		t.Fatalf("site two cannot reach site one: %s", doctor)
	}

	// A job, its handoff, the code, and a bundle.
	thrumIn(t, siteA, "thrum", "--root", "/w", "--actor", "a1", "job", "open", "j1",
		"--repo", "code", "--label", "up", "--goal", "cross the network")
	thrumIn(t, siteA, "sh", "-c", "mkdir -p /w/job/j1/log && echo evidence > /w/job/j1/log/x.log")
	thrumIn(t, siteA, "thrum", "--root", "/w", "--actor", "a1", "job", "handoff", "j1",
		"--status", "partial", "--next", "finish")
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	thrumIn(t, siteA, "sh", "-c", "cd /w/repos/code.git && git push -q http://"+hub+":8080/code.git --all")
	thrumIn(t, siteB, "thrum", "--root", "/w", "sync")
	board := thrumIn(t, siteB, "thrum", "--root", "/w", "job", "list")
	if !strings.Contains(board, "j1") || !strings.Contains(board, "partial") {
		t.Fatalf("site two does not see the handoff: %s", board)
	}
	if v := thrumIn(t, siteB, "thrum", "--root", "/w", "verify"); !strings.Contains(v, "ok") {
		t.Fatalf("site two does not verify: %s", v)
	}

	thrumIn(t, siteA, "thrum", "--root", "/w", "bundle", "export", "j1", "--out", "/tmp/j1.tgz")
	bundle := filepath.Join(base, "j1.tgz")
	dockerOK(t, "cp", siteA+":/tmp/j1.tgz", bundle)
	dockerOK(t, "cp", bundle, siteB+":/tmp/j1.tgz")
	thrumIn(t, siteB, "thrum", "--root", "/w", "bundle", "import", "/tmp/j1.tgz")
	if got := thrumIn(t, siteB, "sh", "-c", "cat /w/job/j1/log/x.log"); !strings.Contains(got, "evidence") {
		t.Fatalf("the bundle did not carry the payload: %s", got)
	}

	// A cross-site asset: the store is a repository too, and sync exchanges it.
	thrumIn(t, siteA, "sh", "-c", "echo cross-asset > /tmp/asset.bin")
	thrumIn(t, siteA, "thrum", "--root", "/w", "asset", "ingest", "/tmp/asset.bin")
	thrumIn(t, siteA, "thrum", "--root", "/w", "asset", "publish", "cross", "2026-10-18", "--description", "cross-site")
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	thrumIn(t, siteB, "thrum", "--root", "/w", "sync")
	thrumIn(t, siteB, "thrum", "--root", "/w", "asset", "materialise", "cross", "2026-10-18", "--job", "j1")
	if got := thrumIn(t, siteB, "sh", "-c", "cat /w/job/j1/data/asset.bin"); !strings.Contains(got, "cross-asset") {
		t.Fatalf("the asset did not cross: %s", got)
	}

	// The same name claimed on both sites, settled deterministically.
	thrumIn(t, siteA, "thrum", "--root", "/w", "repo", "add", "shared", "--about", "from-a")
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	time.Sleep(1100 * time.Millisecond)
	thrumIn(t, siteB, "thrum", "--root", "/w", "repo", "add", "shared", "--about", "from-b")
	settled := thrumIn(t, siteB, "thrum", "--root", "/w", "sync")
	if !strings.Contains(settled, "settled") {
		t.Fatalf("the claim was not settled: %s", settled)
	}
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	a := thrumIn(t, siteA, "sh", "-c", "cat /w/doc/repo/shared.yaml")
	b := thrumIn(t, siteB, "sh", "-c", "cat /w/doc/repo/shared.yaml")
	if a != b || !strings.Contains(a, "from-a") {
		t.Fatalf("the sites disagree on a claimed name:\n--- s1 ---\n%s\n--- s2 ---\n%s", a, b)
	}

	if v := thrumIn(t, siteA, "thrum", "--root", "/w", "verify"); !strings.Contains(v, "ok") {
		t.Fatalf("site one does not verify: %s", v)
	}
	if v := thrumIn(t, siteB, "thrum", "--root", "/w", "verify"); !strings.Contains(v, "ok") {
		t.Fatalf("site two does not verify: %s", v)
	}

	// A partition: B is cut off. Local work continues, a message to B cannot be
	// placed, and both histories converge when the link returns.
	dockerOK(t, "network", "disconnect", network, siteB)
	thrumIn(t, siteA, "thrum", "--root", "/w", "repo", "add", "part-a", "--about", "while apart")
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	thrumIn(t, siteB, "thrum", "--root", "/w", "repo", "add", "part-b", "--about", "while apart")
	if out, err := dockerTry("exec", siteB, "thrum", "--root", "/w", "sync"); err == nil {
		t.Fatalf("site two synced while disconnected: %s", out)
	}
	if out, err := dockerTry("exec", siteA, "thrum", "--root", "/w", "--actor", "a1",
		"message", "send", "a2", "--subject", "during the partition"); err == nil {
		t.Fatalf("a message crossed a partition: %s", out)
	}
	dockerOK(t, "network", "connect", network, siteB)
	thrumIn(t, siteB, "thrum", "--root", "/w", "sync")
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	for _, site := range []string{siteA, siteB} {
		list := thrumIn(t, site, "thrum", "--root", "/w", "repo", "list")
		if !strings.Contains(list, "part-a") || !strings.Contains(list, "part-b") {
			t.Fatalf("%s did not converge after the partition: %s", site, list)
		}
	}
	if v := thrumIn(t, siteB, "thrum", "--root", "/w", "verify"); !strings.Contains(v, "ok") {
		t.Fatalf("site two does not verify after the partition: %s", v)
	}

	// The hub is down: recording and reading continue, sync fails, then it heals.
	dockerOK(t, "stop", hub)
	thrumIn(t, siteA, "thrum", "--root", "/w", "repo", "add", "part-c", "--about", "while the hub is down")
	if out, err := dockerTry("exec", siteA, "thrum", "--root", "/w", "sync"); err == nil {
		t.Fatalf("site one synced with the hub down: %s", out)
	}
	dockerOK(t, "start", hub)
	dockerOK(t, "exec", "-d", hub, "/usr/local/bin/thrum-hub", "-root", "/srv", "-listen", "0.0.0.0:8080")
	waitFor(t, 10*time.Second, "the hub to come back", func() bool {
		_, err := dockerTry("exec", siteA, "sh", "-c", "git ls-remote http://"+hub+":8080/line.git")
		return err == nil
	})
	thrumIn(t, siteA, "thrum", "--root", "/w", "sync")
	thrumIn(t, siteB, "thrum", "--root", "/w", "sync")
	if list := thrumIn(t, siteB, "thrum", "--root", "/w", "repo", "list"); !strings.Contains(list, "part-c") {
		t.Fatalf("site two did not see the record made while the hub was down: %s", list)
	}

	// A restart: the workspace survives; the daemon must be started again, and the
	// lock from the process the restart killed must not stop it.
	dockerOK(t, "restart", siteB)
	dockerOK(t, "exec", "-d", siteB, "sh", "-c",
		"thrum --root /w daemon run --listen 0.0.0.0:7702 >/tmp/daemon.log 2>&1")
	waitFor(t, 10*time.Second, "the restarted daemon", func() bool {
		_, err := dockerTry("exec", siteB, "thrum", "--root", "/w", "daemon", "status")
		return err == nil
	})
	if v := thrumIn(t, siteB, "thrum", "--root", "/w", "verify"); !strings.Contains(v, "ok") {
		t.Fatalf("site two does not verify after a restart: %s", v)
	}
}

func dockerTry(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	return string(out), err
}

func dockerOK(t *testing.T, args ...string) string {
	t.Helper()
	out, err := dockerTry(args...)
	if err != nil {
		t.Fatalf("docker %v: %v\n%s", args, err, out)
	}
	return out
}

func dockerQuiet(args ...string) {
	_, _ = dockerTry(args...)
}

func thrumIn(t *testing.T, container string, args ...string) string {
	t.Helper()
	out, err := dockerTry(append([]string{"exec", container}, args...)...)
	if err != nil {
		t.Fatalf("docker exec %s %v: %v\n%s", container, args, err, out)
	}
	return out
}

func waitFor(t *testing.T, d time.Duration, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if f() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not come up within %s", what, d)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
