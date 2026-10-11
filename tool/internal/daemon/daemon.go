package daemon

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// ListenSpec is where this site accepts peers, as written in var/daemon.yaml.
//
// It is site-local and never committed: it says how to reach this machine, which is the
// machine's own business. A site that says nothing accepts nobody, which is the right
// default for one that uses nothing remote.
func ListenSpec(l workspace.Layout) string {
	raw, err := os.ReadFile(l.DaemonConfig())
	if err != nil {
		return ""
	}
	var config struct {
		Listen string `yaml:"listen"`
	}
	if err := format.DecodeLoose(raw, &config); err != nil {
		return ""
	}
	return config.Listen
}

// Options is how a daemon is started. Neither field is a rule: one is where the local
// socket goes, which the layout already answers, and the other is whether this site
// answers peers at all.
type Options struct {
	// SSHListen is where to accept peers: host:port, or unix:<path>. Empty means the
	// site's own var/daemon.yaml has the answer, and an empty answer there means this
	// site accepts nobody.
	SSHListen string
	// Ready is called once, after the daemon is listening.
	Ready func(socket, sshAddress string)
}

// Run takes the site's lock, listens, and serves until it is asked to stop.
//
// Restarting is safe and losing the process is safe: the workspace is the state, and a
// record, a lease and a claim are all on disk. What a restart loses is supervision —
// what this daemon was running and how it was started — which is exactly the part that
// cannot be written down. Nothing is recovered by guessing: a name stays behind after
// its process is gone, and the note that says so is a claim, not a lock.
func Run(l workspace.Layout, o Options) error {
	release, err := Lock(l)
	if err != nil {
		return err
	}
	defer release()

	socket := l.DaemonSocket()
	// A socket left behind by a daemon that is gone is not a daemon.
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	local, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer local.Close()
	if err := os.Chmod(socket, 0o600); err != nil {
		return err
	}

	service := NewService(l)
	go func() { _ = Serve(service.Handle, local) }()

	listen := o.SSHListen
	if listen == "" {
		listen = ListenSpec(l)
	}
	sshAddress := ""
	if listen != "" {
		network, address := listenTarget(listen)
		peers, err := net.Listen(network, address)
		if err != nil {
			return err
		}
		defer peers.Close()
		sshAddress = listen
		go func() { _ = ServeSSH(l, service.Handle, peers) }()
	}
	if o.Ready != nil {
		o.Ready(socket, sshAddress)
	}

	// It stays until it is asked to stop, because being reachable is the whole of why
	// it exists: a site that accepted peers a moment ago and then left is a site whose
	// actors cannot be written to. Stopping it is `daemon stop`, and stopping it is
	// safe — the workspace is the state, and a message already in a mailbox stays there.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return nil
}

// Stop asks the daemon on this site to leave.
//
// It is the only way to end one, because a daemon that exits on its own is a daemon
// whose peers cannot tell the difference between a quiet site and a stopped one.
func Stop(l workspace.Layout) error {
	pid := lockPID(l.DaemonLock())
	if pid == 0 {
		return fmt.Errorf("no daemon is running here")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(sigTerm); err != nil {
		return fmt.Errorf("the daemon at pid %d did not take the signal: %w", pid, err)
	}
	return nil
}

// listenTarget reads where to accept peers. The forms are the same ones a reach uses,
// so that what a site writes in its own half is what the other site runs.
func listenTarget(spec string) (network, address string) {
	if path, ok := strings.CutPrefix(spec, "unix:"); ok {
		return "unix", path
	}
	if strings.HasPrefix(spec, "/") {
		return "unix", spec
	}
	return "tcp", spec
}

// Lock takes the one lock a site has, the way every other lock in this workspace is
// taken: create-if-absent, with the holder written inside so that a stale one can be
// recognised rather than obeyed.
//
// One daemon per site, and the lock is the whole of the enforcement. It is released by
// the function it returns, and a lock left behind by a process that is gone is taken
// over by whoever runs next — because refusing to run is how a workspace stays stuck.
func Lock(l workspace.Layout) (func(), error) {
	path := l.DaemonLock()
	if err := os.MkdirAll(l.Var(), 0o755); err != nil {
		return nil, err
	}

	body := fmt.Sprintf("pid: %d\nstarted: %s\n", os.Getpid(), time.Now().UTC().Format(format.RFC3339UTC))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if !os.IsExist(err) {
			return nil, err
		}
		// Something is written here. If the process it names is gone, this site has no
		// daemon and refusing to run is how a workspace stays stuck.
		pid := lockPID(path)
		if pid > 0 && processAlive(pid) {
			return nil, fmt.Errorf("a daemon is already running here, as pid %d (%s)", pid, path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
	}
	if _, err := file.WriteString(body); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}

	return func() {
		os.Remove(path)
		os.Remove(l.DaemonSocket())
	}, nil
}

// lockPID reads the pid a lock names.
func lockPID(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "pid:"); ok {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(rest), "%d", &pid); err == nil {
				return pid
			}
		}
	}
	return 0
}

// processAlive answers the one question a lock file cannot: whether the process it
// names is still there.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(sigZero) == nil
}

// Holding is one place a resource lives, as a person would read it.
type Holding struct {
	Site  string `json:"site"`
	Local bool   `json:"local"`
	// Unknown is a site that was asked and did not answer, so whether it holds the resource
	// cannot be established — which is not the same as its not holding it.
	Unknown bool `json:"unknown,omitempty"`
}

// Held reports where a resource lives, from this site's point of view.
func Held(l workspace.Layout, resource string) ([]Holding, error) {
	peers := NewPeers(l)
	sites, unanswered, err := peers.Home(resource)
	if err != nil {
		return nil, err
	}
	out := make([]Holding, 0, len(sites)+len(unanswered))
	for _, site := range sites {
		out = append(out, Holding{Site: site, Local: site == peers.Site()})
	}
	for _, site := range unanswered {
		out = append(out, Holding{Site: site, Unknown: true})
	}
	return out, nil
}
