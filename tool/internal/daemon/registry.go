package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Actor is one thing this daemon is running.
type Actor struct {
	ID      string    `json:"id"`
	Job     string    `json:"job"`
	Actor   string    `json:"actor"`
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	Command []string  `json:"command"`
	// Note is why it stopped, when it has.
	Note string `json:"note,omitempty"`
}

// Alive reports whether the process is still running. Nothing on disk can tell a
// working actor from a dead one; this can, because it started it.
func (a Actor) Alive() bool {
	if a.PID == 0 {
		return false
	}
	proc, err := os.FindProcess(a.PID)
	if err != nil {
		return false
	}
	// On a unix, signal 0 asks whether the process is there without disturbing it.
	return proc.Signal(sigZero) == nil
}

// Registry is what the daemon knows about the running world at this site. It is held
// in memory and nowhere else, because that is the only place where it is true.
type Registry struct {
	mu     sync.Mutex
	actors map[string]*Actor
	// commands remembers how a job was started, so that a blocked actor can be woken
	// by starting it again the same way.
	commands map[string][]string
}

func NewRegistry() *Registry {
	return &Registry{
		actors:   map[string]*Actor{},
		commands: map[string][]string{},
	}
}

// Start runs an actor on a job and records it.
//
// The environment is the design's: two facts — the workspace root and the job id — and
// the base variables that point a job's tools at the workspace rather than at their own
// temporary and cache directories.
func (r *Registry) Start(l workspace.Layout, actor, job string, command []string) (Actor, error) {
	if len(command) == 0 {
		return Actor{}, fmt.Errorf("nothing to run for %s", job)
	}
	brief, _, err := workspace.ReadBrief(l, job)
	if err != nil {
		return Actor{}, err
	}
	// A job that is done or abandoned is no longer meant to happen, so nothing is
	// started on it: an actor working a closed job is work nobody asked for, and
	// nobody is waiting for it.
	if brief.Status != format.StatusActive {
		return Actor{}, fmt.Errorf("%s is %s, and nothing is started on a job that is no longer meant to happen", job, brief.Status)
	}
	// One job is worked by one actor. A second one on the same job is not a helper,
	// it is two writers and one claim.
	if existing := r.running(l, job); existing != nil {
		return Actor{}, fmt.Errorf("%s is already being worked by %s (pid %d); stop it first",
			job, existing.Actor, existing.PID)
	}

	// The claim comes first. Claiming after spawning would leave an actor running a
	// job that somebody else had claimed, and refusing the claim after the process
	// exists is refusing too late.
	if err := occupancy.Occupy(l, job, actor, false, time.Now().UTC()); err != nil {
		return Actor{}, err
	}

	for _, sub := range []string{"tmp", "cache", "state", "log", "data", "ckpt", "out"} {
		if err := os.MkdirAll(l.JobSub(job, sub), 0o755); err != nil {
			_, _ = occupancy.Vacate(l, job)
			return Actor{}, err
		}
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = l.Root
	cmd.Env = append(os.Environ(), ActorEnv(l, job)...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_, _ = occupancy.Vacate(l, job)
		return Actor{}, err
	}

	a := &Actor{
		ID:      fmt.Sprintf("%s-%d", job, cmd.Process.Pid),
		Job:     job,
		Actor:   actor,
		PID:     cmd.Process.Pid,
		Started: time.Now().UTC(),
		Command: command,
	}

	r.mu.Lock()
	r.actors[a.ID] = a
	r.commands[job] = command
	r.mu.Unlock()

	// Nothing can release what a dead holder held, so its fate is watched here rather
	// than assumed later.
	go func() {
		err := cmd.Wait()
		r.mu.Lock()
		defer r.mu.Unlock()
		if err != nil {
			a.Note = err.Error()
			return
		}
		a.Note = "exited"
	}()

	return *a, nil
}

// running returns the live actor for a job, if there is one.
func (r *Registry) running(l workspace.Layout, job string) *Actor {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.actors {
		if a.Job == job && a.Alive() {
			return a
		}
	}
	return nil
}

// ActorEnv is the environment an actor is started with.
func ActorEnv(l workspace.Layout, job string) []string {
	return []string{
		"THRUM_ROOT=" + l.Root,
		"THRUM_JOB=" + job,
		"THRUM_JOBS=" + l.Jobs(),
		// Pointed at the workspace, so that nothing a job runs lands where nothing
		// collects or reviews it.
		"TMPDIR=" + l.JobSub(job, "tmp"),
		"XDG_CACHE_HOME=" + l.Cache(),
		"XDG_STATE_HOME=" + l.JobSub(job, "state"),
	}
}

// Stop ends the actor on a job and clears the note it held.
func (r *Registry) Stop(l workspace.Layout, job string) error {
	r.mu.Lock()
	var targets []*Actor
	for _, a := range r.actors {
		if a.Job == job && a.Alive() {
			targets = append(targets, a)
		}
	}
	r.mu.Unlock()

	for _, a := range targets {
		if proc, err := os.FindProcess(a.PID); err == nil {
			_ = proc.Signal(sigTerm)
		}
	}
	if len(targets) > 0 {
		if _, err := occupancy.Vacate(l, job); err != nil {
			return err
		}
	}
	return nil
}

// AnyAlive reports whether anything this site started is still going.
//
// The ledger keeps a record of everything it ever started; this is the question that
// decides whether the daemon still has a reason to exist.
func (r *Registry) AnyAlive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.actors {
		if a.Alive() {
			return true
		}
	}
	return false
}

// Actors lists what is running, in a stable order.
func (r *Registry) Actors() []Actor {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Actor, 0, len(r.actors))
	for _, a := range r.actors {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Job < out[j].Job })
	return out
}

// Wake starts again every actor of this site whose latest handoff says it is waiting on
// the job that just finished.
//
// No list of waiters is kept anywhere: the waiting is already written down, in the
// handoffs, and this reads it. The actor it wakes as is the one the job already names,
// because the claim belongs to whoever is working it rather than to whoever announced
// the news.
func (r *Registry) Wake(l workspace.Layout, finished string, announcedBy string) ([]string, []string, error) {
	jobs, err := liveJobs(l)
	if err != nil {
		return nil, nil, err
	}

	var woken, failures []string
	for _, job := range jobs {
		if job == finished {
			continue
		}
		waiting, err := waitingOn(l, job, finished)
		if err != nil || !waiting {
			continue
		}
		// Somebody already on it does not need waking, and starting a second actor on
		// one job is the thing the workspace exists to prevent.
		if r.running(l, job) != nil {
			continue
		}
		r.mu.Lock()
		command := r.commands[job]
		r.mu.Unlock()
		if len(command) == 0 {
			// Nothing was ever started here for it, so there is nothing to wake. The
			// job stays blocked, which is the truth.
			continue
		}
		// A refusal here is reported rather than swallowed: a job that stays blocked
		// with nobody told why is worse than a wake that fails loudly.
		if _, err := r.Start(l, holderOf(l, job, announcedBy), job, command); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", job, err))
			continue
		}
		woken = append(woken, job)
	}
	return woken, failures, nil
}

// holderOf names whoever the workspace already says is on a job: the note if there is
// one, else the agent of the last handoff, else whoever is asking.
func holderOf(l workspace.Layout, job, fallback string) string {
	if note, ok, err := occupancy.Get(l, job); err == nil && ok && note.Occupant != "" {
		return note.Occupant
	}
	paths, _ := filepath.Glob(filepath.Join(l.HandoffDir(job), "*.md"))
	if len(paths) > 0 {
		sort.Strings(paths)
		if front, _, err := format.LoadDocument(paths[len(paths)-1]); err == nil {
			var h format.Handoff
			if err := format.Decode(front, &h); err == nil && h.Agent != "" {
				return h.Agent
			}
		}
	}
	return fallback
}

// liveJobs lists the jobs in doc/task/, which is where a job still meant to happen is.
func liveJobs(l workspace.Layout) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(l.Task(), "*", "brief.md"))
	if err != nil {
		return nil, err
	}
	var jobs []string
	for _, p := range paths {
		jobs = append(jobs, filepath.Base(filepath.Dir(p)))
	}
	sort.Strings(jobs)
	return jobs, nil
}

// waitingOn reports whether a job's latest handoff is blocked on another one.
func waitingOn(l workspace.Layout, job, finished string) (bool, error) {
	paths, err := filepath.Glob(filepath.Join(l.HandoffDir(job), "*.md"))
	if err != nil || len(paths) == 0 {
		return false, err
	}
	sort.Strings(paths)
	front, _, err := format.LoadDocument(paths[len(paths)-1])
	if err != nil {
		return false, err
	}
	var h format.Handoff
	if err := format.Decode(front, &h); err != nil {
		return false, err
	}
	if h.Status != format.HandoffBlocked {
		return false, nil
	}
	for _, dep := range h.BlockedBy {
		if dep == finished {
			return true, nil
		}
	}
	return false, nil
}

// Site is the name this machine's workspace goes by, which a site that cannot say
// cannot find its own half of anything.
func Site(l workspace.Layout) string {
	b, err := os.ReadFile(l.SiteFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
