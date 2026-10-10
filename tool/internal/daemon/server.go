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
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Service is the daemon's state: the workspace it serves, the site it is, and when it
// started. Almost nothing is held in memory, because almost everything is a file.
type Service struct {
	L       workspace.Layout
	Peers   *Peers
	Started time.Time
}

// NewService builds a service over a workspace.
func NewService(l workspace.Layout) *Service {
	return &Service{L: l, Peers: NewPeers(l), Started: time.Now().UTC()}
}

// Site is the name this machine's workspace goes by.
func Site(l workspace.Layout) string { return roster.Site(l) }

// Handle is the whole surface. Each verb is one operation from the design, and the
// daemon answers it the same way whoever asks.
func (s *Service) Handle(req Request) Response {
	switch req.Verb {
	case VerbStatus:
		return s.status()

	case VerbRoster:
		entries, err := roster.Local(s.L)
		if err != nil {
			return Fail(err)
		}
		resp, err := Payload(map[string]any{"site": Site(s.L), "actors": entries})
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbJob:
		var args struct {
			Job string `json:"job"`
		}
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		if args.Job == "" {
			return Fail(fmt.Errorf("which job"))
		}
		note, ok, err := occupancy.Get(s.L, args.Job)
		if err != nil {
			return Fail(err)
		}
		body := map[string]any{"site": Site(s.L), "holds": HoldsJob(s.L, args.Job)}
		if ok {
			body["occupant"] = note.Occupant
			body["since"] = note.Since
		}
		resp, err := Payload(body)
		if err != nil {
			return Fail(err)
		}
		return resp

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

	case VerbLeaseTake:
		var args leaseArgs
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		lease, err := leases.Take(s.L, args.request(), time.Now().UTC())
		if err != nil {
			return Fail(err)
		}
		resp, err := Payload(lease)
		if err != nil {
			return Fail(err)
		}
		return resp

	case VerbLeaseRenew:
		var args leaseArgs
		if err := req.DecodeArgs(&args); err != nil {
			return Fail(err)
		}
		lease, err := leases.Renew(s.L, args.request(), time.Now().UTC())
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

	case VerbDeliver:
		return s.deliver(req)
	}
	return Fail(fmt.Errorf("unknown verb %q", req.Verb))
}

// leaseArgs is one shape for the three lease verbs, because they differ only in what
// they do with it.
type leaseArgs struct {
	Resource string `json:"resource"`
	Unit     string `json:"unit"`
	Occupant string `json:"occupant"`
	Job      string `json:"job"`
	TTL      int    `json:"ttl_seconds"`
}

func (a leaseArgs) request() leases.Request {
	return leases.Request{
		Resource: a.Resource,
		Unit:     a.Unit,
		Occupant: a.Occupant,
		Job:      a.Job,
		TTL:      time.Duration(a.TTL) * time.Second,
	}
}

// deliver takes a message for an actor here, with whatever travelled with it.
//
// It is the one thing a daemon is needed for that a single machine does not need: a
// mailbox is a directory, so only a message from somewhere else needs something
// listening.
func (s *Service) deliver(req Request) Response {
	var args struct {
		Message     json.RawMessage `json:"message"`
		Body        string          `json:"body"`
		Attachments []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"attachments"`
	}
	if err := req.DecodeArgs(&args); err != nil {
		return Fail(err)
	}
	var m messages.Message
	if err := json.Unmarshal(args.Message, &m); err != nil {
		return Fail(fmt.Errorf("the message does not decode: %w", err))
	}

	// The bytes arrive in the order the message names them, and the sizes are what the
	// sender said, so the split is exact.
	files := map[string][]byte{}
	offset := 0
	for _, a := range args.Attachments {
		end := offset + int(a.Size)
		if end > len(req.Files) {
			return Fail(fmt.Errorf("%s was announced as %d bytes and fewer arrived", a.Name, a.Size))
		}
		files[a.Name] = req.Files[offset:end]
		offset = end
	}
	if offset != len(req.Files) {
		return Fail(fmt.Errorf("%d bytes arrived that no attachment named", len(req.Files)-offset))
	}

	if err := messages.Deliver(s.L, m, args.Body, files); err != nil {
		return Fail(err)
	}
	// An actor that has been written to exists: being sent something is being named.
	if err := roster.Seen(s.L, m.To, m.Job, time.Now().UTC()); err != nil {
		return Fail(err)
	}
	resp, err := Payload(map[string]string{"delivered": m.ID, "to": m.To})
	if err != nil {
		return Fail(err)
	}
	return resp
}

// StatusData is what this site is and what it holds. Nothing in it is a question about
// another machine: asking a peer for its status must not make that peer ask back.
type StatusData struct {
	Site   string         `json:"site"`
	Root   string         `json:"root"`
	Uptime string         `json:"uptime"`
	Held   []format.Lease `json:"held"`
	Occupy []OccupantNote `json:"occupancy"`
	Actors []roster.Entry `json:"actors"`
	Peers  []PeerReport   `json:"peers,omitempty"`
}

// OccupantNote is one site-local claim, as the status verb reports it.
type OccupantNote struct {
	Job      string `json:"job"`
	Occupant string `json:"occupant"`
	Since    string `json:"since"`
}

func (s *Service) status() Response {
	data := StatusData{
		Site:   Site(s.L),
		Root:   s.L.Root,
		Uptime: time.Since(s.Started).Round(time.Second).String(),
	}
	if held, err := leases.Held(s.L); err == nil {
		data.Held = held
	}
	if entries, err := roster.Local(s.L); err == nil {
		data.Actors = entries
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

// Serve answers connections on a listener until it is closed.
func Serve(h Handler, ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			req, err := ReadRequest(conn)
			if err != nil {
				_ = Reply(conn, Fail(err))
				return
			}
			_ = Reply(conn, h(req))
		}()
	}
}
