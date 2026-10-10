package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/leases"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Handler answers one request. The socket and the forced command share it, so that a
// peer and a local command do exactly the same thing.
type Handler func(Request) Response

// Service is the daemon's state: the registry it holds, the roster it read, and the
// workspace it serves.
type Service struct {
	L        workspace.Layout
	Registry *Registry
	Peers    *Peers
	Started  time.Time
}

// NewService builds a service over a workspace.
func NewService(l workspace.Layout) *Service {
	return &Service{
		L:        l,
		Registry: NewRegistry(),
		Peers:    NewPeers(l),
		Started:  time.Now().UTC(),
	}
}

// Handle is the whole surface. Each verb is one operation from the design, and the
// daemon answers it the same way whoever asks.
func (s *Service) Handle(req Request) Response {
	switch req.Verb {
	case VerbStatus:
		return s.status()
	case VerbOccupy:
		var args struct {
			Job      string `json:"job"`
			Actor    string `json:"actor"`
			Takeover bool   `json:"takeover"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		if args.Actor == "" {
			return Fail(fmt.Errorf("occupancy needs to know which actor"))
		}
		return Fail(occupancy.Occupy(s.L, args.Job, args.Actor, args.Takeover, time.Now().UTC()))

	case VerbVacate:
		var args struct {
			Job string `json:"job"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		was, err := occupancy.Vacate(s.L, args.Job)
		if err != nil {
			return Fail(err)
		}
		resp, err := Payload(was)
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbStart:
		var args struct {
			Job     string   `json:"job"`
			Actor   string   `json:"actor"`
			Command []string `json:"command"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		actor, err := s.Registry.Start(s.L, args.Actor, args.Job, args.Command)
		if err != nil {
			return Fail(err)
		}
		resp, err := Payload(actor)
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbStop:
		var args struct {
			Job string `json:"job"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		return Fail(s.Registry.Stop(s.L, args.Job))

	case VerbNotify, VerbAnnounce:
		var args struct {
			Job   string `json:"job"`
			Actor string `json:"actor"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		woken, failures, err := s.Registry.Wake(s.L, args.Job, args.Actor)
		if err != nil {
			return Fail(err)
		}
		// Every other site wakes its own waiters, and exactly one site does the
		// telling: the one where the job finished. A peer that hears it wakes its own
		// and stops, because a broadcast that re-broadcasts never ends.
		var told []string
		if req.Verb == VerbAnnounce {
			forwarded, _ := s.Peers.Broadcast(Request{
				Verb: VerbNotify,
				Args: Args(map[string]string{"job": args.Job, "actor": args.Actor}),
			})
			told = forwarded
		}
		resp, err := Payload(map[string]any{"woken": woken, "told": told, "failed": failures})
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbAsk:
		var args struct {
			Job string `json:"job"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		note, ok, err := occupancy.Get(s.L, args.Job)
		if err != nil {
			return Fail(err)
		}
		if !ok {
			return Response{OK: true}
		}
		resp, err := Payload(map[string]string{
			"site":     Site(s.L),
			"occupant": note.Occupant,
			"since":    note.Since,
		})
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbLeaseTake:
		var args struct {
			Resource string `json:"resource"`
			Unit     string `json:"unit"`
			Occupant string `json:"occupant"`
			Job      string `json:"job"`
			TTL      int    `json:"ttl_seconds"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		lease, err := leases.Take(s.L, leases.Request{
			Resource: args.Resource,
			Unit:     args.Unit,
			Occupant: args.Occupant,
			Job:      args.Job,
			TTL:      time.Duration(args.TTL) * time.Second,
		}, time.Now().UTC())
		if err != nil {
			return Fail(err)
		}
		resp, err := Payload(lease)
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbLeaseRenew:
		var args struct {
			Resource string `json:"resource"`
			Unit     string `json:"unit"`
			Occupant string `json:"occupant"`
			Job      string `json:"job"`
			TTL      int    `json:"ttl_seconds"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		lease, err := leases.Renew(s.L, leases.Request{
			Resource: args.Resource,
			Unit:     args.Unit,
			Occupant: args.Occupant,
			Job:      args.Job,
			TTL:      time.Duration(args.TTL) * time.Second,
		}, time.Now().UTC())
		if err != nil {
			return Fail(err)
		}
		resp, err := Payload(lease)
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbLeaseRelease:
		var args struct {
			Resource string `json:"resource"`
			Unit     string `json:"unit"`
			Occupant string `json:"occupant"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		return Fail(leases.Release(s.L, args.Resource, args.Unit, args.Occupant))

	case VerbHolds:
		var args struct {
			Resource string `json:"resource"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		resp, err := Payload(map[string]bool{"holds": s.Peers.Holds(args.Resource)})
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbPeers:
		resp, err := Payload(map[string]any{"peers": s.Peers.Report()})
		if err != nil {
			return Fail(err)
		}
		return resp
	}
	return Fail(fmt.Errorf("unknown verb %q", req.Verb))
}

// OccupantNote is one site-local note, as the status verb reports it.
type OccupantNote struct {
	Job      string `json:"job"`
	Occupant string `json:"occupant"`
	Since    string `json:"since"`
}

// StatusData is what the daemon knows without asking anybody.
//
// Nothing here is a question about another machine. Asking a peer for its status must
// not make that peer ask back, or two sites that answer each other's questions ring
// forever; who is reachable is the peers verb's business, and it is asked once.
type StatusData struct {
	Site   string         `json:"site"`
	Root   string         `json:"root"`
	Uptime string         `json:"uptime"`
	Actors []Actor        `json:"actors"`
	Held   []format.Lease `json:"held"`
	Occupy []OccupantNote `json:"occupancy"`
}

func (s *Service) status() Response {
	data := StatusData{
		Site:   Site(s.L),
		Root:   s.L.Root,
		Uptime: time.Since(s.Started).Round(time.Second).String(),
		Actors: s.Registry.Actors(),
	}

	if held, err := leases.Held(s.L); err == nil {
		data.Held = held
	}
	paths, _ := filepath.Glob(filepath.Join(s.L.Occupy(), "*.yaml"))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var note format.Occupy
		if err := format.DecodeLoose(raw, &note); err != nil {
			continue
		}
		data.Occupy = append(data.Occupy, OccupantNote{
			Job:      strings.TrimSuffix(filepath.Base(path), ".yaml"),
			Occupant: note.Occupant,
			Since:    note.Since,
		})
	}
	sort.Slice(data.Occupy, func(i, j int) bool { return data.Occupy[i].Job < data.Occupy[j].Job })

	resp, err := Payload(data)
	if err != nil {
		return Fail(err)
	}
	return resp
}

// Idle reports whether this site has nothing left that needs something alive.
//
// A daemon exists for two reasons: to supervise an actor, and to act later on behalf of
// a job that is waiting. When it has neither, it has no reason to exist, and a daemon
// nobody asked to run is worse than no daemon at all.
//
// The grace period covers the one race that matters: a daemon started in order to serve
// a request that has not arrived yet must not decide it is idle and leave.
func (s *Service) Idle() bool {
	if time.Since(s.Started) < 10*time.Second {
		return false
	}
	if s.Registry.AnyAlive() {
		return false
	}
	jobs, err := liveJobs(s.L)
	if err != nil {
		return false // unsure is not idle
	}
	for _, job := range jobs {
		if blocked(s.L, job) {
			return false // something is waiting, and only this can wake it
		}
	}
	return true
}

// blocked reports whether a job's latest handoff says it is waiting on another.
func blocked(l workspace.Layout, job string) bool {
	paths, _ := filepath.Glob(filepath.Join(l.HandoffDir(job), "*.md"))
	if len(paths) == 0 {
		return false
	}
	sort.Strings(paths)
	front, _, err := format.LoadDocument(paths[len(paths)-1])
	if err != nil {
		return false
	}
	var h format.Handoff
	if err := format.Decode(front, &h); err != nil {
		return false
	}
	return h.Status == format.HandoffBlocked && len(h.BlockedBy) > 0
}

// Serve answers connections on a listener until it is closed. The socket and the SSH
// server both come through here, so both speak the same protocol.
func Serve(h Handler, ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			var req Request
			if err := json.NewDecoder(conn).Decode(&req); err != nil {
				_ = json.NewEncoder(conn).Encode(Fail(err))
				return
			}
			_ = json.NewEncoder(conn).Encode(h(req))
		}()
	}
}
