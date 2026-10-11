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
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
	"golang.org/x/crypto/ssh"
)

// site builds a workspace with a declared working directory and a code repository with
// one commit in it, which is what a site looks like before anything has happened.
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
	registry := "worktree:\n  - name: uploader\n    purpose: Uploader reliability work\n    ephemeral: false\n"
	if err := os.WriteFile(l.ResourceKind("worktree"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCode(t, l)
	return l
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

func TestTheSocketAnswersWhatFilesCannot(t *testing.T) {
	l := site(t, "build-01")

	// With no daemon, nothing is lost: the claim is a file, and the command does its
	// own work.
	if _, running, err := daemon.Local(l, daemon.Request{Verb: daemon.VerbStatus}); err != nil || running {
		t.Fatalf("nothing should be running yet: running=%v err=%v", running, err)
	}

	writeBrief(t, l, "job-a")
	serve(t, l)

	if resp := ask(t, l, daemon.VerbOccupy, map[string]string{"job": "job-a", "actor": "claude-opus"}); !resp.OK {
		t.Fatalf("occupy: %s", resp.Error)
	}
	// A second actor does not overwrite somebody else's claim and call the job its own.
	if resp := ask(t, l, daemon.VerbOccupy, map[string]string{"job": "job-a", "actor": "claude-sonnet"}); resp.OK {
		t.Error("a second actor's claim on one job should be refused")
	}
	if resp := ask(t, l, daemon.VerbOccupy, map[string]any{"job": "job-a", "actor": "claude-sonnet", "takeover": true}); !resp.OK {
		t.Errorf("an explicit take-over should be allowed: %s", resp.Error)
	}

	// The answer is the claim, and the daemon does not pretend it is more.
	var told jobAnswer
	if err := json.Unmarshal(ask(t, l, daemon.VerbJob, map[string]string{"job": "job-a"}).Data, &told); err != nil {
		t.Fatal(err)
	}
	if told.Occupant != "claude-sonnet" || told.Site != "build-01" {
		t.Errorf("who is on it: %+v", told)
	}

	// Being named makes an actor exist, and the roster says where it was heard from.
	if err := roster.Seen(l, "claude-opus", "job-a", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var status daemon.StatusData
	if err := json.Unmarshal(ask(t, l, daemon.VerbStatus, nil).Data, &status); err != nil {
		t.Fatal(err)
	}
	if status.Site != "build-01" || len(status.Occupy) != 1 || len(status.Actors) != 1 {
		t.Errorf("status: %+v", status)
	}

	var ro rosterBody
	if err := json.Unmarshal(ask(t, l, daemon.VerbRoster, nil).Data, &ro); err != nil {
		t.Fatal(err)
	}
	if len(ro.Actors) != 1 || ro.Actors[0].Name != "claude-opus" || ro.Actors[0].Site != "build-01" {
		t.Errorf("roster: %+v", ro)
	}

	if resp := ask(t, l, daemon.VerbVacate, map[string]string{"job": "job-a"}); !resp.OK {
		t.Fatalf("vacate: %s", resp.Error)
	}
	if err := json.Unmarshal(ask(t, l, daemon.VerbStatus, nil).Data, &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Occupy) != 0 {
		t.Errorf("the claim should be gone: %+v", status.Occupy)
	}
}

type rosterBody struct {
	Site   string         `json:"site"`
	Actors []roster.Entry `json:"actors"`
}

// A message is delivered to the site of the actor it is addressed to, and its bytes
// arrive with it.
func TestAMessageIsDeliveredToTheActorsSite(t *testing.T) {
	build := site(t, "build-01")
	lab := site(t, "lab-02")
	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	shareKeys(t, build, lab)

	labService := daemon.NewService(lab)
	socket := serveSSH(t, labService)
	writeReach(t, build, "lab-02", socket)

	// The recipient exists because it was heard from at the lab.
	if err := roster.Seen(lab, "claude-opus", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	message := messages.Message{
		ID:      messages.NewID(),
		From:    "claude-sonnet",
		To:      "claude-opus",
		Site:    "build-01",
		Sent:    messages.Sent(time.Now().UTC()),
		Subject: "the uploader timeout is explained",
	}
	body := "It was the retry budget.\n"
	contents := []byte("a,b\n1,2\n")
	message.Attachments = []messages.Attachment{{Name: "sample.csv", Size: int64(len(contents))}}

	header, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	peers := daemon.NewPeers(build)
	ref, err := peers.Lookup("lab-02")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := peers.Ask(ref, daemon.Request{
		Verb: daemon.VerbDeliver,
		Args: daemon.Args(map[string]any{
			"message":     json.RawMessage(header),
			"body":        body,
			"attachments": message.Attachments,
		}),
		Files: contents,
	})
	if err != nil {
		t.Fatalf("delivering to the lab: %v", err)
	}
	if !resp.OK {
		t.Fatalf("the lab refused it: %s", resp.Error)
	}

	// It is in the mailbox at the lab, with its attachment intact, and the recipient is
	// on the roster there.
	list, err := messages.List(lab, "claude-opus")
	if err != nil || len(list) != 1 {
		t.Fatalf("the mailbox: %v %v", list, err)
	}
	if list[0].Subject != message.Subject || list[0].From != "claude-sonnet" {
		t.Errorf("what arrived: %+v", list[0])
	}
	if list[0].Site != "build-01" {
		t.Errorf("a message says where it was sent from: %+v", list[0])
	}
	got, err := messages.AttachmentBytes(lab, "claude-opus", message.ID, "sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(contents) {
		t.Errorf("the attachment changed on the way: %q", got)
	}
	if entry, ok := roster.Find("claude-opus", mustRoster(t, lab)); !ok || entry.Site != "lab-02" {
		t.Errorf("being written to is being named: %+v", entry)
	}

	// Nothing was written at the sending site: a mailbox belongs to the actor, and the
	// actor is at the lab.
	if list, err := messages.List(build, "claude-opus"); err != nil || len(list) != 0 {
		t.Errorf("the message must not be left at the sender: %v %v", list, err)
	}
}

func mustRoster(t *testing.T, l workspace.Layout) []roster.Entry {
	t.Helper()
	entries, err := roster.Local(l)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// A site answers for the jobs it holds, and says plainly when it holds none: the site
// that holds a job is the one whose claim counts, so "I do not hold it" is an answer
// rather than a silence.
func TestASiteAnswersForTheJobsItHolds(t *testing.T) {
	l := site(t, "build-01")
	writeBrief(t, l, "job-a")
	serve(t, l)

	answer := func() jobAnswer {
		t.Helper()
		var body jobAnswer
		if err := json.Unmarshal(ask(t, l, daemon.VerbJob, map[string]string{"job": "job-a"}).Data, &body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	if answer().Holds {
		t.Error("a job nobody materialised is held nowhere")
	}

	// Materialising it is what gives it a home, and the branch is made here because the
	// working directory is the first thing that needs one.
	if err := worktrees.Add(l, "uploader", "job-a", ""); err != nil {
		t.Fatalf("materialising: %v", err)
	}
	if got := answer(); !got.Holds || got.Occupant != "" {
		t.Errorf("a materialised job is held here, and nobody is on it yet: %+v", got)
	}

	if resp := ask(t, l, daemon.VerbOccupy, map[string]string{"job": "job-a", "actor": "claude-opus"}); !resp.OK {
		t.Fatalf("occupy: %s", resp.Error)
	}
	if got := answer(); got.Occupant != "claude-opus" || got.Since == "" {
		t.Errorf("the claim should be what the site answers with: %+v", got)
	}
}

type jobAnswer struct {
	Site     string `json:"site"`
	Holds    bool   `json:"holds"`
	Occupant string `json:"occupant"`
	Since    string `json:"since"`
}

func TestPeersAnswerOverSSH(t *testing.T) {
	build := site(t, "build-01")
	lab := site(t, "lab-02")
	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	shareKeys(t, build, lab)
	writeRoster(t, build, lab)
	writeReach(t, build, "lab-02", serveSSH(t, daemon.NewService(lab)))

	report := daemon.NewPeers(build).Report()
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
}

func TestAResourceIsLeasedWhereItLives(t *testing.T) {
	build := site(t, "build-01")
	lab := site(t, "lab-02")
	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	shareKeys(t, build, lab)
	writeRoster(t, build, lab)
	for _, l := range []workspace.Layout{build, lab} {
		if err := os.WriteFile(l.ResourceKind("gpu"), []byte("gpu:\n  - name: gpu\n    purpose: The only GPU\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeBrief(t, l, "job-a")
	}
	// The lab is the machine that has the GPU, and says so in its own half.
	if err := os.MkdirAll(lab.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lab.Config(), "gpu.yaml"), []byte("holds: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	socket := serveSSH(t, daemon.NewService(lab))
	writeReach(t, build, "lab-02", socket)

	peers := daemon.NewPeers(build)
	holders, err := peers.Home("gpu")
	if err != nil {
		t.Fatal(err)
	}
	if len(holders) != 1 || holders[0] != "lab-02" {
		t.Fatalf("the GPU lives at the lab, and only there: %v", holders)
	}

	ref, err := peers.Lookup("lab-02")
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := peers.Ask(ref, daemon.Request{
		Verb: daemon.VerbLeaseTake,
		Args: daemon.Args(map[string]any{
			"resource": "gpu", "job": "job-a", "ttl_seconds": 3600,
		}),
	}); err != nil {
		t.Fatalf("taking the lease at the lab: %v", err)
	} else if !resp.OK {
		t.Fatalf("the lab refused: %s", resp.Error)
	}

	// It was written at the lab, and not here.
	if _, err := os.Stat(filepath.Join(lab.Lease(), "gpu.1.yaml")); err != nil {
		t.Errorf("the lease should be at the site that holds the GPU: %v", err)
	}
	if _, err := os.Stat(filepath.Join(build.Lease(), "gpu.1.yaml")); err == nil {
		t.Error("the lease must not be written at the site that wants it")
	}

	// The home site refuses a second holder, wherever the request came from.
	if _, err := peers.Ask(ref, daemon.Request{
		Verb: daemon.VerbLeaseTake,
		Args: daemon.Args(map[string]any{
			"resource": "gpu", "job": "job-a", "ttl_seconds": 3600,
		}),
	}); err == nil {
		t.Error("a second holder should be refused by the home site")
	}
}

// A site serves its own repositories to its peers, and only its own: the records travel
// by git, so git needs somewhere to fetch from.
func TestTheDaemonServesThisSitesRepositoriesAndNothingElse(t *testing.T) {
	l := site(t, "build-01")
	public := writeKeys(t, l, "A")
	if err := os.WriteFile(filepath.Join(l.Secrets(), "authorized_keys"), []byte(public+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	peerSocket := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", peerSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() { _ = daemon.ServeSSH(l, daemon.NewService(l).Handle, listener) }()

	client := sshClientFor(t, l, peerSocket)

	stdout, err := client.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start("git-upload-pack '" + l.CodeRepo() + "'"); err != nil {
		t.Fatalf("git-upload-pack: %v", err)
	}
	// What comes back is git's own ref advertisement: four hexadecimal digits of
	// length, then the payload.
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
	_ = client.Close()

	if err := client.Start("git-upload-pack '/etc'"); err == nil {
		t.Error("a peer asking for a path outside the workspace should be refused")
	}
	if err := client.Start("/bin/sh"); err == nil {
		t.Error("a peer asking for a shell should be refused")
	}
}

// Two daemons that each answer the other's questions must not ring forever: nothing in
// one site's status is a question about another machine.
func TestStatusIsLocalEvenWithALivePeer(t *testing.T) {
	build := site(t, "build-01")
	lab := site(t, "lab-02")
	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	shareKeys(t, build, lab)
	writeRoster(t, build, lab)
	writeReach(t, build, "lab-02", serveSSH(t, daemon.NewService(lab)))

	peers := daemon.NewPeers(build)
	ref, err := peers.Lookup("lab-02")
	if err != nil {
		t.Fatal(err)
	}
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

// var/config/ holds the local half of any resource, and most resources are not sites.
func TestALocalHalfThatIsNotASiteIsNotAPeer(t *testing.T) {
	l := site(t, "build-01")
	writeRoster(t, l)
	if err := os.MkdirAll(l.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.Config(), "gpu.yaml"), []byte("holds: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

func writeRoster(t *testing.T, lists ...workspace.Layout) {
	t.Helper()
	roster := "site:\n  - name: build-01\n    purpose: The build machine\n  - name: lab-02\n    purpose: The lab machine\n"
	for _, l := range lists {
		if err := os.WriteFile(l.ResourceKind("site"), []byte(roster), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// writeReach says how this site reaches that one, which is its own business and never
// travels.
func writeReach(t *testing.T, l workspace.Layout, peer, socket string) {
	t.Helper()
	if err := os.MkdirAll(l.Config(), 0o755); err != nil {
		t.Fatal(err)
	}
	reach := "reach: unix:" + socket + "\nssh_user: root\n"
	if err := os.WriteFile(filepath.Join(l.Config(), peer+".yaml"), []byte(reach), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeBrief(t *testing.T, l workspace.Layout, job string) {
	t.Helper()
	if err := os.MkdirAll(l.TaskDir(job), 0o755); err != nil {
		t.Fatal(err)
	}
	brief := "---\njob: " + job + "\nstatus: active\nbranch: job/" + job + "\nowned_paths: []\n---\n\n## Goal\n\n## Scope\n\n## Acceptance criteria\n"
	if err := os.WriteFile(l.Brief(job), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
}

// serveSSH answers peers over a unix socket, which is a reach a site may enter and keeps
// the test off the network.
func serveSSH(t *testing.T, service *daemon.Service) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() { _ = daemon.ServeSSH(service.L, service.Handle, listener) }()
	return socket
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
