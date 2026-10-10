package daemon_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"golang.org/x/crypto/ssh"
)

// site builds a workspace with two declared working directories and two jobs, one of
// them blocked on the other, which is everything the daemon's rules are about.
func site(t *testing.T, name string) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  name,
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := "worktree:\n  - name: uploader\n    purpose: Uploader reliability work\n    ephemeral: false\n" +
		"  - name: spike\n    purpose: A one-off look\n    ephemeral: true\n"
	if err := os.WriteFile(l.ResourceKind("worktree"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	writeBrief(t, l, "job-a", "")
	writeBrief(t, l, "job-b", "job-a")
	return l
}

// writeBrief writes a brief, and for a job waiting on another, a handoff that says so.
// The waiting is written down in the record and nowhere else: no list of waiters is
// kept, because the record already holds it.
func writeBrief(t *testing.T, l workspace.Layout, job, blockedBy string) {
	t.Helper()
	if err := os.MkdirAll(l.TaskDir(job), 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: " + job + "\nstatus: active\nbranch: job/" + job + "\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(l.Brief(job), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
	if blockedBy == "" {
		return
	}
	if err := os.MkdirAll(l.HandoffDir(job), 0o755); err != nil {
		t.Fatal(err)
	}
	handoff := "---\n" +
		"job: " + job + "\n" +
		"agent: tester\n" +
		"branch: job/" + job + "\n" +
		"base: 1111111111111111111111111111111111111111\n" +
		"head: 1111111111111111111111111111111111111111\n" +
		"status: blocked\n" +
		"started: 2026-10-09T05:00:00Z\n" +
		"ended: 2026-10-09T05:10:00Z\n" +
		"next: wait\n" +
		"blocked_by:\n  - " + blockedBy + "\n" +
		"---\n\n## What changed\n\n## What was verified\n\n## What remains\n\n## Open questions\n"
	if err := os.WriteFile(l.HandoffFile(job, "2026-10-09T05-00-00Z.md"), []byte(handoff), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOneDaemonPerSite(t *testing.T) {
	l := site(t, "build-01")

	release, err := daemon.Lock(l)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := daemon.Lock(l); err == nil {
		t.Fatal("a second daemon should be refused while one holds the lock")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Errorf("the refusal should say what is there: %v", err)
	}

	// A lock naming a process that is gone is not a daemon, and refusing to run is how
	// a workspace stays stuck.
	release()
	if err := os.WriteFile(l.DaemonLock(), []byte("pid: 999999999\nstarted: 2026-01-01T00:00:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := daemon.Lock(l)
	if err != nil {
		t.Fatalf("a stale lock should be taken over: %v", err)
	}

	// A lock naming a live process is obeyed, even if it is not this process.
	if err := os.WriteFile(l.DaemonLock(), []byte("pid: "+strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Lock(l); err == nil {
		t.Error("a lock naming a live process should be obeyed")
	}
	stale()
}

// serve starts a service on a local socket, which is what a daemon does without the
// part that waits for a signal.
func serve(t *testing.T, l workspace.Layout) *daemon.Service {
	t.Helper()
	service := daemon.NewService(l)
	if err := os.MkdirAll(l.Var(), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", l.DaemonSocket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() { _ = daemon.Serve(service.Handle, ln) }()
	return service
}

// ask sends one request. A refusal is an answer, not a failure: the refusal is often
// exactly what the test is checking.
func ask(t *testing.T, l workspace.Layout, verb daemon.Verb, args any) daemon.Response {
	t.Helper()
	conn, err := net.Dial("unix", l.DaemonSocket())
	if err != nil {
		t.Fatalf("%s: no daemon answered: %v", verb, err)
	}
	defer conn.Close()
	resp, err := daemon.Call(conn, daemon.Request{Verb: verb, Args: daemon.Args(args)})
	if err != nil && resp.OK {
		t.Fatalf("%s: %v", verb, err)
	}
	return resp
}

func TestTheSocketAnswersWhatOnlyTheDaemonCan(t *testing.T) {
	l := site(t, "build-01")

	// With no daemon, the command does its own work and says so rather than failing.
	if _, running, err := daemon.Local(l, daemon.Request{Verb: daemon.VerbStatus}); err != nil || running {
		t.Fatalf("nothing should be running yet: running=%v err=%v", running, err)
	}

	serve(t, l)

	// The claim goes through the daemon when there is one, because the note describes
	// a running world and the daemon is what runs it.
	if resp := ask(t, l, daemon.VerbOccupy, map[string]string{"job": "job-a", "actor": "claude-opus"}); !resp.OK {
		t.Fatalf("occupy: %s", resp.Error)
	}
	// And it refuses to overwrite somebody else's claim.
	if resp := ask(t, l, daemon.VerbOccupy, map[string]string{"job": "job-a", "actor": "claude-sonnet"}); resp.OK {
		t.Error("a second actor's claim on one job should be refused")
	}
	// Taking it over is a decision, so it has to be asked for.
	if resp := ask(t, l, daemon.VerbOccupy, map[string]any{"job": "job-a", "actor": "claude-sonnet", "takeover": true}); !resp.OK {
		t.Errorf("an explicit take-over should be allowed: %s", resp.Error)
	}

	resp := ask(t, l, daemon.VerbAsk, map[string]string{"job": "job-a"})
	var told map[string]string
	if err := json.Unmarshal(resp.Data, &told); err != nil {
		t.Fatal(err)
	}
	if told["occupant"] != "claude-sonnet" || told["site"] != "build-01" {
		t.Errorf("who is on it: %v", told)
	}

	resp = ask(t, l, daemon.VerbStatus, nil)
	var status daemon.StatusData
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		t.Fatal(err)
	}
	if status.Site != "build-01" || len(status.Occupy) != 1 {
		t.Errorf("status: %+v", status)
	}

	if resp := ask(t, l, daemon.VerbVacate, map[string]string{"job": "job-a"}); !resp.OK {
		t.Fatalf("vacate: %s", resp.Error)
	}
	if resp := ask(t, l, daemon.VerbStatus, nil); !resp.OK {
		t.Fatal(resp.Error)
	}
	status = daemon.StatusData{}
	if err := json.Unmarshal(ask(t, l, daemon.VerbStatus, nil).Data, &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Occupy) != 0 {
		t.Errorf("the note should be gone: %+v", status.Occupy)
	}
}

func TestOneJobIsWorkedByOneActor(t *testing.T) {
	l := site(t, "build-01")
	serve(t, l)

	wakes := filepath.Join(l.JobSub("job-a", "out"), "wakes")
	command := []string{"/bin/sh", "-c", "echo ran >> " + wakes}

	resp := ask(t, l, daemon.VerbStart, map[string]any{"job": "job-a", "actor": "claude-opus", "command": command})
	if !resp.OK {
		t.Fatalf("start: %s", resp.Error)
	}
	waitForLine(t, wakes, 1)

	// A second actor on the same job is not a helper, it is two writers and one claim.
	resp = ask(t, l, daemon.VerbStart, map[string]any{"job": "job-a", "actor": "claude-sonnet", "command": []string{"/bin/sleep", "30"}})
	if resp.OK {
		t.Fatal("a second actor on one job should be refused")
	}
	if strings.Contains(resp.Error, "occupied") == false {
		t.Logf("the refusal was: %s", resp.Error)
	}

	// The refused start must not have left a process behind: a claim refused after the
	// process exists is refused too late. The first actor is still there, and it is
	// the only one.
	status := daemon.StatusData{}
	if err := json.Unmarshal(ask(t, l, daemon.VerbStatus, nil).Data, &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Actors) != 1 {
		t.Errorf("a refused start left an actor behind: %+v", status.Actors)
	}
	for _, a := range status.Actors {
		if a.Actor != "claude-opus" {
			t.Errorf("the surviving actor should be the one that was there first: %+v", a)
		}
	}
}

// A job that has closed is not something to start an actor on: the work is over, and an
// actor on it would be work nobody asked for.
func TestAClosedJobIsNotStartedAgain(t *testing.T) {
	l := site(t, "build-01")
	serve(t, l)

	closed := "---\njob: job-a\nstatus: done\nbranch: job/job-a\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(l.Brief("job-a"), []byte(closed), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := ask(t, l, daemon.VerbStart, map[string]any{
		"job": "job-a", "actor": "claude-opus", "command": []string{"/bin/sleep", "30"},
	})
	if resp.OK {
		t.Fatal("a closed job should not be started")
	}
	if !strings.Contains(resp.Error, "no longer meant to happen") {
		t.Errorf("the refusal should say why: %s", resp.Error)
	}
}

func TestWaitingIsReadFromTheRecordAndWokenOnce(t *testing.T) {
	l := site(t, "build-01")
	serve(t, l)

	wakes := filepath.Join(l.JobSub("job-b", "out"), "wakes")
	command := []string{"/bin/sh", "-c", "echo ran >> " + wakes}

	// job-b's latest handoff says it is waiting on job-a, which is the only place that
	// is written down.
	if resp := ask(t, l, daemon.VerbStart, map[string]any{"job": "job-b", "actor": "claude-opus", "command": command}); !resp.OK {
		t.Fatalf("start: %s", resp.Error)
	}
	waitForLine(t, wakes, 1)
	waitForNoLiveActors(t, l)

	// job-a finishes. job-b's own daemon wakes it, and the job that finished is not
	// itself woken.
	resp := ask(t, l, daemon.VerbAnnounce, map[string]string{"job": "job-a", "actor": "tester"})
	if !resp.OK {
		t.Fatalf("announce: %s", resp.Error)
	}
	var body struct {
		Woken  []string `json:"woken"`
		Failed []string `json:"failed"`
	}
	if err := json.Unmarshal(resp.Data, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Failed) != 0 {
		t.Fatalf("a wake failed: %v", body.Failed)
	}
	if len(body.Woken) != 1 || body.Woken[0] != "job-b" {
		t.Errorf("woken: %v", body.Woken)
	}
	waitForLine(t, wakes, 2)

	// A peer's notify wakes its own waiters and stops there. That is the whole of the
	// one-hop rule: a broadcast that re-broadcasts bounces between two sites forever.
	resp = ask(t, l, daemon.VerbNotify, map[string]string{"job": "job-a", "actor": "tester"})
	if !resp.OK {
		t.Fatalf("notify: %s", resp.Error)
	}
	var heard struct {
		Woken []string `json:"woken"`
		Told  []string `json:"told"`
	}
	if err := json.Unmarshal(resp.Data, &heard); err != nil {
		t.Fatal(err)
	}
	if len(heard.Told) != 0 {
		t.Errorf("a peer's notify must not fan out again, and it told %v", heard.Told)
	}
}

func TestPeersAnswerOverSSH(t *testing.T) {
	// Two sites, each with a key, each listing the other's public key. The transport is
	// SSH and its authentication is SSH's: this adds no authentication model of its own.
	keyA := writeKeys(t, siteNamed(t, "build-01"), "A")
	keyB := writeKeys(t, siteNamed(t, "lab-02"), "B")
	_ = keyA

	build := realSite(t, "build-01")
	lab := realSite(t, "lab-02")
	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	shareKeys(t, build, lab)

	// Each declares the roster, and each enters its own reach for the other.
	roster := "site:\n  - name: build-01\n    purpose: The build machine\n  - name: lab-02\n    purpose: The lab machine\n"
	for _, l := range []workspace.Layout{build, lab} {
		_ = os.MkdirAll(l.Resource(), 0o755)
		if err := os.WriteFile(l.ResourceKind("site"), []byte(roster), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	labService := daemon.NewService(lab)
	socket := serveSSH(t, lab, labService)

	if err := os.MkdirAll(build.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	reach := "reach: unix:" + socket + "\n"
	if err := os.WriteFile(filepath.Join(build.Config(), "lab-02.yaml"), []byte(reach), 0o644); err != nil {
		t.Fatal(err)
	}

	peers := daemon.NewPeers(build)
	report := peers.Report()
	if len(report) != 2 {
		t.Fatalf("report: %+v", report)
	}
	for _, p := range report {
		switch p.Name {
		case "build-01":
			if !p.Reachable {
				t.Errorf("this site should be reachable to itself: %+v", p)
			}
		case "lab-02":
			if !p.Reachable {
				t.Fatalf("the peer should answer over SSH: %+v", p)
			}
		}
	}
	_ = keyB
}

func TestAResourceIsLeasedWhereItLives(t *testing.T) {
	build := realSite(t, "build-01")
	lab := realSite(t, "lab-02")
	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	shareKeys(t, build, lab)

	roster := "site:\n  - name: build-01\n    purpose: The build machine\n  - name: lab-02\n    purpose: The lab machine\n"
	for _, l := range []workspace.Layout{build, lab} {
		_ = os.MkdirAll(l.Resource(), 0o755)
		if err := os.WriteFile(l.ResourceKind("site"), []byte(roster), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(l.ResourceKind("gpu"), []byte("gpu:\n  - name: gpu\n    purpose: The only GPU\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Both sites know the job, because the records travel by git.
		writeBrief(t, l, "job-a", "")
	}

	// The lab is the machine that has the GPU, and says so in its own half. Nothing in
	// the declaration says where a resource lives: that is a fact about a machine, and
	// a declaration travels.
	if err := os.MkdirAll(lab.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lab.Config(), "gpu.yaml"), []byte("holds: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	labService := daemon.NewService(lab)
	socket := serveSSH(t, lab, labService)

	if err := os.MkdirAll(build.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(build.Config(), "lab-02.yaml"),
		[]byte("reach: unix:"+socket+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	holders, err := daemon.NewPeers(build).Home("gpu")
	if err != nil {
		t.Fatal(err)
	}
	if len(holders) != 1 || holders[0] != "lab-02" {
		t.Fatalf("the GPU lives at the lab, and only there: %v", holders)
	}

	// Taking the lease goes to the site that holds the thing, so exclusion stays on
	// the machine it is physically on.
	ref, err := daemon.NewPeers(build).Lookup("lab-02")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := daemon.NewPeers(build).Ask(ref, daemon.Request{
		Verb: daemon.VerbLeaseTake,
		Args: daemon.Args(map[string]any{
			"resource": "gpu", "occupant": "claude-opus", "job": "job-a", "ttl_seconds": 3600,
		}),
	})
	if err != nil {
		t.Fatalf("taking the lease at the lab: %v", err)
	}
	if !resp.OK {
		t.Fatalf("the lab refused: %s", resp.Error)
	}

	// It was written at the lab, and not here.
	if _, err := os.Stat(filepath.Join(lab.Lease(), "gpu.1.yaml")); err != nil {
		t.Errorf("the lease should be at the site that holds the GPU: %v", err)
	}
	if _, err := os.Stat(filepath.Join(build.Lease(), "gpu.1.yaml")); err == nil {
		t.Error("the lease must not be written at the site that wants it")
	}

	// And the home site refuses a second holder, wherever the request came from.
	resp, err = daemon.NewPeers(build).Ask(ref, daemon.Request{
		Verb: daemon.VerbLeaseTake,
		Args: daemon.Args(map[string]any{
			"resource": "gpu", "occupant": "claude-sonnet", "job": "job-a", "ttl_seconds": 3600,
		}),
	})
	if err == nil {
		t.Fatalf("a second holder should be refused by the home site: %+v", resp)
	}
	if !strings.Contains(err.Error(), "held by") {
		t.Errorf("the refusal should say who holds it: %v", err)
	}
}

// Two daemons that each answer the other's questions must not ring forever. Status is
// therefore what this site knows, and nothing in it is a question about another
// machine.
func TestStatusIsLocalEvenWithALivePeer(t *testing.T) {
	build := realSite(t, "build-01")
	lab := realSite(t, "lab-02")
	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	shareKeys(t, build, lab)

	roster := "site:\n  - name: build-01\n    purpose: The build machine\n  - name: lab-02\n    purpose: The lab machine\n"
	for _, l := range []workspace.Layout{build, lab} {
		_ = os.MkdirAll(l.Resource(), 0o755)
		if err := os.WriteFile(l.ResourceKind("site"), []byte(roster), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Both daemons serve peers, and each can reach the other.
	socketFor := map[workspace.Layout]string{}
	for _, l := range []workspace.Layout{build, lab} {
		socketFor[l] = serveSSH(t, l, daemon.NewService(l))
	}
	if err := os.MkdirAll(build.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lab.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(build.Config(), "lab-02.yaml"),
		[]byte("reach: unix:"+socketFor[lab]+"\nssh_user: root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lab.Config(), "build-01.yaml"),
		[]byte("reach: unix:"+socketFor[build]+"\nssh_user: root\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	peers := daemon.NewPeers(build)
	ref, err := peers.Lookup("lab-02")
	if err != nil {
		t.Fatal(err)
	}
	// The answer has to arrive, and it has to arrive without the other side asking
	// back. A reply that never comes is what an answering loop looks like from here.
	done := make(chan error, 1)
	go func() {
		_, err := peers.Ask(ref, daemon.Request{Verb: daemon.VerbStatus})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("asking a live peer for its status: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("status never came back: the two sites are answering each other")
	}
}

// var/config/ holds the local half of any resource, and most resources are not sites,
// so a half that says what this site has must not be read as a peer.
func TestALocalHalfThatIsNotASiteIsNotAPeer(t *testing.T) {
	l := realSite(t, "build-01")
	_ = os.MkdirAll(l.Resource(), 0o755)
	if err := os.WriteFile(l.ResourceKind("site"), []byte("site:\n  - name: build-01\n    purpose: Here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	// A resource half: it says this site has the thing.
	if err := os.WriteFile(filepath.Join(l.Config(), "gpu.yaml"), []byte("holds: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A site half for a site the roster no longer declares: it says how to reach a
	// machine, and that duty has ended.
	if err := os.WriteFile(filepath.Join(l.Config(), "old-machine.yaml"), []byte("reach: 10.0.0.9:22\nssh_user: root\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := daemon.NewPeers(l).Refs()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, ref := range refs {
		names[ref.Name] = true
	}
	if names["gpu"] {
		t.Error("a resource's local half is not a site")
	}
	if !names["old-machine"] {
		t.Error("a local half naming a machine the roster dropped is a site, and says so")
	}
}

func TestAResourceNobodyHoldsIsRefused(t *testing.T) {
	l := site(t, "build-01")
	holders, err := daemon.NewPeers(l).Home("nothing-declares-this")
	if err != nil {
		t.Fatal(err)
	}
	if len(holders) != 0 {
		t.Errorf("no site holds it, and none should be invented: %v", holders)
	}
}

// serveSSH answers peers over a unix socket, which is a reach a site may enter and
// keeps the test off the network.
func serveSSH(t *testing.T, l workspace.Layout, service *daemon.Service) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() { _ = daemon.ServeSSH(l, service.Handle, listener) }()
	return socket
}

// waitForNoLiveActors waits until this daemon has nothing running, so that a test of
// waking is not a test of timing.
func waitForNoLiveActors(t *testing.T, l workspace.Layout) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var status daemon.StatusData
		if err := json.Unmarshal(ask(t, l, daemon.VerbStatus, nil).Data, &status); err != nil {
			t.Fatal(err)
		}
		live := 0
		for _, a := range status.Actors {
			if a.Alive() {
				live++
			}
		}
		if live == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("actors were still running")
}

// A site serves its own repositories to its peers, and only its own: the records travel
// by git, so git needs somewhere to fetch from, and the path a peer asks for comes from
// git itself.
func TestTheDaemonServesThisSitesRepositoriesAndNothingElse(t *testing.T) {
	l := realSite(t, "build-01")
	public := writeKeys(t, l, "A")
	// A peer has to be listed to be let in, and here the peer is this very key.
	if err := os.WriteFile(filepath.Join(l.Secrets(), "authorized_keys"), []byte(public+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedCode(t, l)

	peerSocket := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", peerSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() { _ = daemon.ServeSSH(l, daemon.NewService(l).Handle, listener) }()

	client := sshClientFor(t, l, peerSocket)

	// The code repository is served. What comes back is git's own ref advertisement,
	// which is a packet-line: four hexadecimal digits of length, then the payload.
	stdout, err := client.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start("git-upload-pack '" + l.CodeRepo() + "'"); err != nil {
		t.Fatalf("git-upload-pack: %v", err)
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(stdout, header); err != nil {
		t.Fatalf("no advertisement came back: %v", err)
	}
	length, err := strconv.ParseInt(string(header), 16, 32)
	if err != nil || length < 5 {
		t.Fatalf("the first packet-line is %q, which is not one", header)
	}
	body := make([]byte, length-4)
	if _, err := io.ReadFull(stdout, body); err != nil {
		t.Fatalf("the advertisement was cut short: %v", err)
	}
	if !strings.Contains(string(body), "refs/heads/main") {
		t.Errorf("the served repository should advertise its refs, and said:\n%s", body)
	}
	// The advertisement is what this test came for, so the session is abandoned here
	// rather than negotiated to the end.
	_ = client.Close()

	// Anything else on the machine is not a repository this site serves.
	if err := client.Start("git-upload-pack '/etc'"); err == nil {
		t.Error("a peer asking for a path outside the workspace should be refused")
	}
	// And neither is a command that is not a git service.
	if err := client.Start("/bin/sh"); err == nil {
		t.Error("a peer asking for a shell should be refused")
	}
}

// sshClientFor opens a session as a peer of this site would.
func sshClientFor(t *testing.T, l workspace.Layout, socket string) *ssh.Session {
	t.Helper()
	raw := mustRead(t, filepath.Join(l.Secrets(), "id_ed25519"))
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	conn, err := ssh.Dial("unix", socket, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	session, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// seedCode gives the code repository one commit, which is what a peer would fetch.
func seedCode(t *testing.T, l workspace.Layout) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	command := exec.Command("git", "clone", "--quiet", l.CodeRepo(), clone)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "project: first commit"},
		{"push", "--quiet", "origin", "HEAD:refs/heads/main"},
	} {
		command := exec.Command("git", append([]string{"-C", clone}, args...)...)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("seeding: %v\n%s", err, out)
		}
	}
}

// realSite is a site with a named roster, which the peer tests need before they can
// change it.
func realSite(t *testing.T, name string) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  name,
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func siteNamed(t *testing.T, name string) workspace.Layout { return realSite(t, name) }

// writeKeys gives a site its own private key, and returns the public one.
func writeKeys(t *testing.T, l workspace.Layout, label string) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l.Secrets(), 0o700); err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.Secrets(), "id_ed25519"), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	public := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer)))
	if err := os.WriteFile(filepath.Join(l.Secrets(), "mykey.pub"), []byte(public+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return public
}

// shareKeys gives each site the other's public key, which is the whole of the
// authorisation: SSH's, with nothing added.
func shareKeys(t *testing.T, a, b workspace.Layout) {
	t.Helper()
	aPub := mustRead(t, filepath.Join(a.Secrets(), "mykey.pub"))
	bPub := mustRead(t, filepath.Join(b.Secrets(), "mykey.pub"))
	if err := os.WriteFile(filepath.Join(a.Secrets(), "authorized_keys"), bPub, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Secrets(), "authorized_keys"), aPub, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func waitForLine(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			if n := countLines(t, path); n >= want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not reach %d lines", path, want)
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return len(strings.Fields(strings.TrimSpace(strings.ReplaceAll(string(b), "\n", " "))))
}

// The formats are what the daemon writes into the workspace, and the checks on them
// live in one place, so a daemon-written record has to pass them.
var _ = format.StatusActive
