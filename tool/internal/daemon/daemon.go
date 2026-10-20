// Package daemon is a site's endpoint: its own listener and protocol, the last-known
// places, and the mailboxes. It is a cache and a channel, never an authority.
package daemon

import (
	"archive/tar"
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
	"github.com/multiagentworkspace/thrum/tool/internal/assets"
	"github.com/multiagentworkspace/thrum/tool/internal/discovery"
	"github.com/multiagentworkspace/thrum/tool/internal/exchange"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/join"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

const protocolVersion = "thrum/1"

// Request is one call.
type Request struct {
	Verb    string          `json:"verb"`
	Args    json.RawMessage `json:"args,omitempty"`
	NoReply bool            `json:"no_reply,omitempty"`
}

// Response is one answer.
type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Code  string          `json:"code,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type greeting struct {
	Workspace string `json:"workspace"`
	Site      string `json:"site"`
	Version   string `json:"version"`
	Nonce     string `json:"nonce"`
	Join      bool   `json:"join,omitempty"`
	Key       string `json:"key,omitempty"`
	Previous  string `json:"previous,omitempty"`
	Proof     string `json:"proof,omitempty"`
	Signature string `json:"signature,omitempty"`
	Error     string `json:"error,omitempty"`
}

type server struct {
	l             layout.Layout
	site          string
	roster        map[string]records.Site
	workspace     string
	stop          chan struct{}
	once          sync.Once
	routes        map[string]route
	routesMu      sync.RWMutex
	routeEvery    time.Duration
	discoverEvery time.Duration
	listen        string
	code          bool
	run           bool
	peers         map[string]*peerLink
	peersMu       sync.Mutex

	discoverMu   sync.Mutex
	discoverConn *discovery.Socket
	discoverStop chan struct{}

	listenerMu sync.Mutex
	tcp        net.Listener
}

// Run listens until it is stopped. The TCP listener serves peers; the unix socket
// serves this machine's commands.
func Run(l layout.Layout, listen string, routeEvery time.Duration, discover bool, discoverEvery time.Duration, code, run bool) error {
	s, err := newServer(l)
	if err != nil {
		return err
	}
	s.listen = listen
	s.code = code
	s.run = run
	s.peers = map[string]*peerLink{}
	s.routeEvery = routeEvery
	if s.routeEvery <= 0 {
		s.routeEvery = 5 * time.Second
	}
	s.discoverEvery = discoverEvery
	if s.discoverEvery <= 0 {
		s.discoverEvery = 5 * time.Second
	}
	if err := s.setDiscover(discover); err != nil {
		return err
	}
	defer s.stopDiscover()
	if err := os.MkdirAll(l.Var(), 0o755); err != nil {
		return err
	}
	if pid, err := readLock(l.DaemonLock()); err == nil {
		// A pid can be alive and still not be a daemon: the number may have been
		// reused, or an old daemon may have died leaving its socket behind. Only an
		// answer on the socket is a daemon; anything else is a leftover.
		if processAlive(pid) {
			if _, err := Status(l); err == nil {
				return fmt.Errorf("a daemon is already running here (pid %d)", pid)
			}
		}
		_ = os.Remove(l.DaemonLock())
		_ = os.Remove(l.DaemonSocket())
	} else if !os.IsNotExist(err) {
		// A lock nobody can read is a leftover, not a daemon.
		_ = os.Remove(l.DaemonLock())
		_ = os.Remove(l.DaemonSocket())
	}
	lock, err := os.OpenFile(l.DaemonLock(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("a daemon is already running here")
	}
	if _, err := fmt.Fprintf(lock, "%d\n", os.Getpid()); err != nil {
		_ = lock.Close()
		_ = os.Remove(l.DaemonLock())
		return err
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(l.DaemonLock())
	}()

	addr := listen
	if addr == "" {
		addr = ":7700"
	}

	_ = os.Remove(l.DaemonSocket())
	unix, err := net.Listen("unix", l.DaemonSocket())
	if err != nil {
		return err
	}
	_ = os.Chmod(l.DaemonSocket(), 0o600)
	defer os.Remove(l.DaemonSocket())

	if err := s.setListen(addr); err != nil {
		return err
	}
	defer s.closeListeners()

	fmt.Printf("listening on %s\n", s.tcp.Addr())
	go s.accept(unix, false)
	go s.router()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	select {
	case <-sig:
	case <-s.stop:
	}
	return nil
}

// Status reports whether a daemon answers here, and what it says.
func Status(l layout.Layout) (Response, error) {
	return LocalDo(l, Request{Verb: "site.hello"})
}

// Reconfigure applies a setting to the running daemon without restarting it.
func Reconfigure(l layout.Layout, args map[string]any) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	_, err = LocalDo(l, Request{Verb: "site.reconfigure", Args: b})
	return err
}

// Stop asks the local daemon to stop.
func Stop(l layout.Layout) error {
	_, err := LocalDo(l, Request{Verb: "stop"})
	return err
}

func newServer(l layout.Layout) (*server, error) {
	site := actors.Site(l)
	if site == "" {
		return nil, fmt.Errorf("this machine does not name its site")
	}
	if _, err := loadKey(l); err != nil {
		return nil, err
	}
	roster := map[string]records.Site{}
	entries, err := os.ReadDir(l.Sites())
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		var s records.Site
		ok, err := records.Read(filepath.Join(l.Sites(), e.Name()), &s)
		if err != nil {
			return nil, err
		}
		if ok {
			roster[s.Site] = s
		}
	}
	ws, err := workspaceID(l)
	if err != nil {
		return nil, err
	}
	// The learned routes are a cache, but a restart is ordinary (an address change,
	// an idle stop), and losing them means the next send fails until the poll runs
	// again. `var/routes.yaml` is that cache.
	routes := map[string]route{}
	if _, err := format.ReadWhole(filepath.Join(l.Var(), "routes.yaml"), &struct {
		Sites map[string]route `yaml:"sites"`
	}{Sites: routes}); err != nil {
		routes = map[string]route{}
	}
	return &server{l: l, site: site, roster: roster, workspace: ws, stop: make(chan struct{}), routes: routes}, nil
}

func (s *server) accept(ln net.Listener, remote bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
			br := bufio.NewReader(conn)
			caller := s.site
			joining := false
			if remote {
				site, join, err := s.auth(conn, br)
				if err != nil {
					return
				}
				caller = site
				joining = join
			}
			s.serve(conn, br, remote, caller, joining)
		}()
	}
}

func (s *server) auth(conn net.Conn, br *bufio.Reader) (string, bool, error) {
	dec := json.NewDecoder(br)
	enc := json.NewEncoder(conn)
	refuse := func(format string, args ...any) (string, bool, error) {
		msg := fmt.Sprintf(format, args...)
		_ = enc.Encode(greeting{Error: msg})
		return "", false, fmt.Errorf("%s", msg)
	}

	var hello greeting
	if err := dec.Decode(&hello); err != nil {
		return "", false, err
	}
	if hello.Workspace != s.workspace {
		return refuse("that site is in a different workspace")
	}
	if hello.Version != protocolVersion {
		return refuse("that site speaks protocol %q, this site speaks %q", hello.Version, protocolVersion)
	}
	if hello.Join {
		// The joiner has no roster entry with a key yet; the one-time nonce is what
		// authorizes the first pull and the key push.
		peer, ok := rosterOf(s.l)[hello.Site]
		if !ok {
			return refuse("site %q is not declared", hello.Site)
		}
		if peer.Invite == "" || peer.Invite != join.InviteHash(hello.Nonce) {
			return refuse("that invitation has been used, or it names another site")
		}
		key, err := loadKey(s.l)
		if err != nil {
			return refuse("this site has no usable key")
		}
		serverNonce := nonce()
		reply := greeting{
			Workspace: s.workspace, Site: s.site, Version: protocolVersion,
			Nonce: serverNonce, Signature: sign(key, hello.Nonce),
			Key: workspace.PublicKey(key.Public().(ed25519.PublicKey)),
		}
		if err := enc.Encode(reply); err != nil {
			return "", false, err
		}
		var ack greeting
		if err := dec.Decode(&ack); err != nil {
			return "", false, err
		}
		if ack.Error != "" {
			return "", false, fmt.Errorf("%s", ack.Error)
		}
		if err := enc.Encode(greeting{}); err != nil {
			return "", false, err
		}
		return hello.Site, true, nil
	}
	// Read the roster for each handshake: a rotation, a revocation or a forget must
	// take effect without restarting the daemon.
	peer, ok := rosterOf(s.l)[hello.Site]
	if !ok {
		return refuse("site %q is not declared", hello.Site)
	}
	if peer.Key == "" {
		return refuse("site %q has no key", hello.Site)
	}
	pub, err := workspace.ParseSiteKey(peer.Key)
	if err != nil {
		return refuse("site %q has a key this site cannot read", hello.Site)
	}
	// A rotated key is proved by the key the roster still carries: the peers have
	// not seen the line that publishes the new one yet.
	callerKey := pub
	if hello.Key != "" && hello.Proof != "" && hello.Previous == peer.Key {
		if next, err := workspace.ParseSiteKey(hello.Key); err == nil &&
			verify(pub, hello.Key, hello.Proof) {
			callerKey = next
		}
	}
	key, err := loadKey(s.l)
	if err != nil {
		return refuse("this site has no usable key")
	}

	serverNonce := nonce()
	reply := greeting{
		Workspace: s.workspace, Site: s.site, Version: protocolVersion,
		Nonce: serverNonce, Signature: sign(key, hello.Nonce),
	}
	if prev, ok := loadPrevKey(s.l); ok {
		cur := workspace.PublicKey(key.Public().(ed25519.PublicKey))
		old := workspace.PublicKey(prev.Public().(ed25519.PublicKey))
		reply.Key, reply.Previous, reply.Proof = cur, old, sign(prev, cur)
	}
	if err := enc.Encode(reply); err != nil {
		return "", false, err
	}
	var proof greeting
	if err := dec.Decode(&proof); err != nil {
		return "", false, err
	}
	if !verify(callerKey, serverNonce, proof.Signature) {
		return refuse("site %q did not prove its key", hello.Site)
	}
	if err := enc.Encode(greeting{}); err != nil {
		return "", false, err
	}
	return hello.Site, false, nil
}

func (s *server) serve(conn net.Conn, br *bufio.Reader, remote bool, caller string, joining bool) {
	dec := json.NewDecoder(br)
	enc := json.NewEncoder(conn)
	var link *peerLink
	if remote && caller != "" && !joining {
		link = newPeerLink(conn, enc, caller, false)
		s.registerPeer(caller, link)
		defer s.unregisterPeer(caller, link)
	}
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}
		if link != nil {
			link.seen()
		}
		if req.Verb == "git.serve" {
			s.gitServe(conn, br, enc, req, joining)
			return
		}
		if req.Verb == "code.receive" {
			s.codeReceive(conn, enc, req, remote)
			return
		}
		if joining {
			if !req.NoReply {
				_ = enc.Encode(fail(errors.New("a join connection carries repositories only")))
			}
			continue
		}
		resp := s.dispatch(req, remote, caller)
		if !req.NoReply {
			if link != nil {
				_ = link.send(resp)
			} else {
				_ = enc.Encode(resp)
			}
		}
	}
}

// registerPeer records a live link. The incoming side replaces any older incoming
// link; an outgoing link is owned by the daemon's sender and is not overwritten here.
func (s *server) registerPeer(site string, p *peerLink) {
	s.peersMu.Lock()
	defer s.peersMu.Unlock()
	if s.peers == nil {
		s.peers = map[string]*peerLink{}
	}
	if old := s.peers[site]; old != nil && old.outgoing && p.outgoing {
		_ = old.close()
	}
	s.peers[site] = p
}

func (s *server) unregisterPeer(site string, p *peerLink) {
	s.peersMu.Lock()
	defer s.peersMu.Unlock()
	if cur := s.peers[site]; cur == p {
		delete(s.peers, site)
	}
}

// sendTo writes a request to a live link. It is how the reverse direction works:
// a site that was reached can answer on the same connection without an address.
func (s *server) sendTo(site string, req Request) error {
	s.peersMu.Lock()
	p := s.peers[site]
	s.peersMu.Unlock()
	if p == nil {
		return fmt.Errorf("no live link to %s", site)
	}
	return p.send(req)
}

func (s *server) hasPeer(site string) bool {
	s.peersMu.Lock()
	defer s.peersMu.Unlock()
	_, ok := s.peers[site]
	return ok
}

type actorArgs struct {
	Actor string `json:"actor"`
	Heard string `json:"heard,omitempty"`
}

type pushArgs struct {
	Message records.Message   `json:"message"`
	Body    string            `json:"body,omitempty"`
	Files   map[string]string `json:"files,omitempty"`
}

// forwardArgs carries a message one hop. Seen is the sites it has already passed
// through, Hops counts them; the id in Message is what makes a retry idempotent.
type forwardArgs struct {
	To      string            `json:"to"`
	Message records.Message   `json:"message"`
	Body    string            `json:"body,omitempty"`
	Files   map[string]string `json:"files,omitempty"`
	From    string            `json:"from,omitempty"`
	Seen    []string          `json:"seen,omitempty"`
	Hops    int               `json:"hops,omitempty"`
}

func (s *server) dispatch(req Request, remote bool, caller string) Response {
	switch req.Verb {
	case "store.published":
		tags, err := assets.Live(s.l)
		if err != nil {
			return fail(err)
		}
		return payload(map[string]any{"tags": tags})
	case "history.exchange":
		if remote {
			return fail(errors.New("history.exchange is this machine's"))
		}
		if strings.ContainsAny(s.l.Root, " \t\n") {
			return fail(errors.New("the workspace path contains a space; the daemon transport cannot carry it"))
		}
		if exe, err := os.Executable(); err != nil || strings.ContainsAny(exe, " \t\n") {
			return fail(errors.New("the thrum binary path contains a space; the daemon transport cannot carry it"))
		}
		settled, unreachable, err := exchange.RunVia(s.l, s.historyLinks())
		if err != nil {
			return fail(err)
		}
		return payload(map[string]any{"settled": settled, "unreachable": unreachable})
	case "site.hello":
		return payload(map[string]any{
			"workspace": s.workspace, "site": s.site, "version": protocolVersion,
			"held": held(s.l),
		})
	case "actor.where":
		var args actorArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		if place, ok := actors.Places(s.l).Actors[args.Actor]; ok && place.Site != "" {
			return payload(map[string]any{"here": true, "site": place.Site, "heard": place.Heard})
		}
		if remote {
			return payload(map[string]any{"here": false})
		}
		// Ask the peers, and keep the latest answer. A peer answers from its own
		// table; it does not ask further, so the question cannot loop.
		var bestSite, bestHeard string
		for peer := range rosterOf(s.l) {
			if peer == s.site {
				continue
			}
			c, err := Dial(s.l, peer)
			if err != nil {
				continue
			}
			resp, err := c.Do(Request{Verb: "actor.where", Args: req.Args})
			_ = c.Close()
			if err != nil {
				continue
			}
			var d struct {
				Here  bool   `json:"here"`
				Site  string `json:"site"`
				Heard string `json:"heard"`
			}
			if err := json.Unmarshal(resp.Data, &d); err != nil {
				continue
			}
			if d.Here && d.Site != "" && d.Heard > bestHeard {
				bestSite, bestHeard = d.Site, d.Heard
			}
		}
		if bestSite != "" {
			_ = actors.SetPlaceAt(s.l, args.Actor, bestSite, bestHeard)
			return payload(map[string]any{"here": true, "site": bestSite, "heard": bestHeard})
		}
		return payload(map[string]any{"here": false})
	case "actor.hello":
		var args actorArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		site := s.site
		if remote {
			site = caller
		}
		heard := args.Heard
		if heard == "" {
			heard = time.Now().UTC().Format(format.RFC3339UTC)
		}
		if err := actors.SetPlaceAt(s.l, args.Actor, site, heard); err != nil {
			return fail(err)
		}
		if !remote {
			go s.broadcastPlace(args)
		}
		return payload(map[string]bool{"ok": true})
	case "msg.push":
		var args pushArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		return s.pushMessage(args)
	case "msg.forward":
		var args forwardArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		if args.To == "" || args.Message.ID == "" {
			return fail(errors.New("a forward needs a destination and a message id"))
		}
		// A relay that sees itself in the path is a loop; the destination's own
		// delivery is not.
		if remote && args.To != s.site {
			for _, seen := range args.Seen {
				if seen == s.site {
					return fail(fmt.Errorf("the message for %s looped back to %s", args.Message.To, s.site))
				}
			}
		}
		if args.Hops > len(rosterOf(s.l))+1 {
			return fail(fmt.Errorf("the message for %s exceeded the hop limit", args.Message.To))
		}
		if args.To == s.site {
			resp := s.pushMessage(pushArgs{Message: args.Message, Body: args.Body, Files: args.Files})
			return resp
		}
		r, ok := s.routeFor(args.To)
		if !ok {
			return fail(fmt.Errorf("no route to %s", args.To))
		}
		if r.NextHop == args.To {
			body, err := base64.StdEncoding.DecodeString(args.Body)
			if err != nil {
				return fail(err)
			}
			files := map[string][]byte{}
			for name, encoded := range args.Files {
				data, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					return fail(fmt.Errorf("attachment %s: %w", name, err))
				}
				files[name] = data
			}
			err = Push(s.l, args.To, args.Message, body, files)
			if err == nil {
				return payload(map[string]string{"id": args.Message.ID})
			}
			var nh *NotHere
			if errors.As(err, &nh) && nh.Site != "" {
				data, _ := json.Marshal(map[string]string{"site": nh.Site})
				return Response{Code: "not_here", Error: err.Error(), Data: data}
			}
			return fail(err)
		}
		if !hasSite(args.Seen, s.site) {
			args.Seen = append(args.Seen, s.site)
		}
		args.Hops++
		c, err := Dial(s.l, r.NextHop)
		if err != nil {
			return fail(err)
		}
		defer c.Close()
		data, err := json.Marshal(args)
		if err != nil {
			return fail(err)
		}
		resp, err := c.Do(Request{Verb: "msg.forward", Args: data})
		if err != nil {
			if resp.Code != "" {
				return resp
			}
			return fail(err)
		}
		return resp
	case "site.routes":
		s.routesMu.RLock()
		out := make(map[string]route, len(s.routes))
		for k, v := range s.routes {
			out[k] = v
		}
		s.routesMu.RUnlock()
		if remote {
			sites := map[string]int{}
			for k, v := range out {
				sites[k] = v.Hops
			}
			return payload(map[string]any{"sites": sites})
		}
		return payload(map[string]any{"routes": out})
	case "site.reconfigure":
		var args struct {
			Discover *bool  `json:"discover"`
			Listen   string `json:"listen"`
		}
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		if args.Discover != nil {
			if err := s.setDiscover(*args.Discover); err != nil {
				return fail(err)
			}
		}
		if args.Listen != "" {
			if err := s.setListen(args.Listen); err != nil {
				return fail(err)
			}
		}
		return payload(map[string]any{"site": s.site})

	case "site.ping":
		return payload(map[string]any{"site": s.site})
	case "file.receive":
		if !remote {
			return fail(errors.New("file.receive is for another site"))
		}
		var args struct {
			Job  string `json:"job"`
			Name string `json:"name"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		data, err := base64.StdEncoding.DecodeString(args.Data)
		if err != nil {
			return fail(err)
		}
		dir, err := fileDir(s, args.Job)
		if err != nil {
			return fail(err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail(err)
		}
		name := filepath.Base(args.Name)
		if name == "" || name == "." || name == ".." {
			return fail(fmt.Errorf("%q is not a file name", args.Name))
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return fail(err)
		}
		return payload(map[string]string{"name": name})
	case "file.fetch":
		if !remote {
			return fail(errors.New("file.fetch is for another site"))
		}
		var args struct {
			Job  string `json:"job"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		dir, err := fileDir(s, args.Job)
		if err != nil {
			return fail(err)
		}
		name := filepath.Base(args.Name)
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fail(err)
		}
		return payload(map[string]string{"data": base64.StdEncoding.EncodeToString(data)})
	case "run":
		if !remote {
			return fail(errors.New("run is for another site"))
		}
		var args runArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		return s.runCommand(args)
	case "stop":
		if remote {
			return fail(fmt.Errorf("stop is local only"))
		}
		s.once.Do(func() { close(s.stop) })
		return Response{OK: true}
	default:
		return fail(fmt.Errorf("unknown verb %q", req.Verb))
	}
}

// pushMessage hands a message to this site's mailbox, refusing it when the actor has
// moved. It is the destination end of both msg.push and a relayed msg.forward.
func (s *server) pushMessage(args pushArgs) Response {
	if place, ok := actors.Places(s.l).Actors[args.Message.To]; ok && place.Site != "" && place.Site != s.site {
		data, _ := json.Marshal(map[string]string{"site": place.Site})
		return Response{
			Code:  "not_here",
			Error: fmt.Sprintf("%s is at %s, not here", args.Message.To, place.Site),
			Data:  data,
		}
	}
	body, err := base64.StdEncoding.DecodeString(args.Body)
	if err != nil {
		return fail(err)
	}
	files := map[string][]byte{}
	for name, encoded := range args.Files {
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return fail(fmt.Errorf("attachment %s: %w", name, err))
		}
		files[name] = data
	}
	if err := messages.Send(s.l, args.Message, body, files); err != nil {
		if errors.Is(err, messages.ErrExists) {
			return payload(map[string]any{"id": args.Message.ID, "duplicate": true})
		}
		return fail(err)
	}
	return payload(map[string]string{"id": args.Message.ID})
}

type route struct {
	NextHop string `json:"next_hop" yaml:"next_hop"`
	Hops    int    `json:"hops" yaml:"hops"`
	Live    bool   `json:"live,omitempty" yaml:"live,omitempty"`
}

func (s *server) router() {
	t := time.NewTicker(s.routeEvery)
	defer t.Stop()
	alone := 0
	for {
		if s.pollRoutes() == 0 {
			alone++
		} else {
			alone = 0
		}
		// Six intervals with no neighbour answering: nobody is being served, and
		// nothing durable lives in the daemon, so it can stop. The next command
		// starts it again.
		if alone >= 6 {
			// A helper is meant to be used by others: it stays up even when no
			// peer currently answers, or a remote command could never reach it.
			if s.code || s.run {
				alone = 0
			} else {
				s.once.Do(func() { close(s.stop) })
				return
			}
		}
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
	}
}

// pollRoutes asks every directly reachable peer for its own reachable set, keeps
// the direct edges at one hop, and takes the shortest route through a neighbour.
func (s *server) pollRoutes() int {
	peers := rosterOf(s.l)
	best := map[string]route{}
	sets := map[string]map[string]int{}
	for name := range peers {
		if name == s.site || peers[name].Key == "" {
			continue
		}
		if s.hasPeer(name) {
			best[name] = route{NextHop: name, Hops: 1, Live: true}
			continue
		}
		if reachFor(s.l, name, peers) == "" {
			continue
		}
		c, err := dial(s.l, name, true)
		if err != nil {
			continue
		}
		resp, err := c.Do(Request{Verb: "site.routes"})
		_ = c.Close()
		if err != nil || !resp.OK {
			continue
		}
		var d struct {
			Sites map[string]int `json:"sites"`
		}
		if json.Unmarshal(resp.Data, &d) != nil {
			continue
		}
		best[name] = route{NextHop: name, Hops: 1}
		sets[name] = d.Sites
	}
	for peer, set := range sets {
		for dest, hops := range set {
			if dest == s.site || dest == peer {
				continue
			}
			consider(best, dest, route{NextHop: peer, Hops: hops + 1})
		}
	}
	s.setRoutes(best)
	s.peersMu.Lock()
	live := len(s.peers)
	s.peersMu.Unlock()
	return len(sets) + live
}

func consider(m map[string]route, site string, r route) {
	cur, ok := m[site]
	if !ok || r.Hops < cur.Hops || (r.Hops == cur.Hops && r.NextHop < cur.NextHop) {
		m[site] = r
	}
}

func (s *server) setDiscover(on bool) error {
	s.discoverMu.Lock()
	defer s.discoverMu.Unlock()
	if on && s.discoverConn == nil {
		conn, err := discovery.Listen()
		if err != nil {
			return err
		}
		s.discoverConn = conn
		s.discoverStop = make(chan struct{})
		go s.discoverLoop(conn, s.discoverStop)
	}
	if !on && s.discoverConn != nil {
		close(s.discoverStop)
		_ = s.discoverConn.Close()
		s.discoverConn = nil
		s.discoverStop = nil
	}
	return nil
}

func (s *server) stopDiscover() {
	s.discoverMu.Lock()
	defer s.discoverMu.Unlock()
	if s.discoverConn != nil {
		close(s.discoverStop)
		_ = s.discoverConn.Close()
		s.discoverConn = nil
		s.discoverStop = nil
	}
}

func (s *server) setListen(addr string) error {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()
	if s.tcp != nil {
		if s.tcp.Addr().String() == addr {
			return nil
		}
		_ = s.tcp.Close()
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.tcp = ln
	s.listen = addr
	go s.accept(ln, true)
	return nil
}

func (s *server) closeListeners() {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()
	if s.tcp != nil {
		_ = s.tcp.Close()
		s.tcp = nil
	}
}

func (s *server) setRoutes(routes map[string]route) {
	s.routesMu.Lock()
	s.routes = routes
	s.routesMu.Unlock()
	data, err := format.MarshalYAML(struct {
		Sites map[string]route `yaml:"sites"`
	}{Sites: routes})
	if err != nil {
		return
	}
	_ = format.WriteFileAtomic(filepath.Join(s.l.Var(), "routes.yaml"), data)
}

func (s *server) routeFor(site string) (route, bool) {
	s.routesMu.RLock()
	defer s.routesMu.RUnlock()
	r, ok := s.routes[site]
	return r, ok
}

func hasSite(list []string, site string) bool {
	for _, s := range list {
		if s == site {
			return true
		}
	}
	return false
}

// codeReceive takes over the connection: ack, then a tar of a working tree that
// lands in job/<id>/data/src/. No repository is touched.
func (s *server) codeReceive(conn net.Conn, enc *json.Encoder, req Request, remote bool) {
	if !remote {
		_ = enc.Encode(fail(errors.New("code.receive is for another site")))
		return
	}
	if !s.code {
		_ = enc.Encode(fail(errors.New("code receiving is off here (site code on)")))
		return
	}
	var args struct {
		Job string `json:"job"`
	}
	if err := json.Unmarshal(req.Args, &args); err != nil {
		_ = enc.Encode(fail(err))
		return
	}
	if !format.ValidSlug(args.Job) {
		_ = enc.Encode(fail(fmt.Errorf("%q is not a job id", args.Job)))
		return
	}
	dir := s.l.PayloadSub(args.Job, "data/src")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_ = enc.Encode(fail(err))
		return
	}
	if err := enc.Encode(payload(map[string]string{"state": "receiving"})); err != nil {
		return
	}
	tr := tar.NewReader(conn)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		name := filepath.Clean(h.Name)
		if name == "." || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || filepath.IsAbs(name) {
			return
		}
		target := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode)&0o777)
			if err != nil {
				return
			}
			_, _ = io.Copy(f, tr)
			_ = f.Close()
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Remove(target)
			_ = os.Symlink(h.Linkname, target)
		}
	}
}

type runArgs struct {
	Job   string   `json:"job,omitempty"`
	Actor string   `json:"actor,omitempty"`
	Argv  []string `json:"argv"`
}

// runCommand executes the actor's own command on this site. It is a transport with
// a default directory, not a sandbox.
func (s *server) runCommand(args runArgs) Response {
	if !s.run {
		return fail(errors.New("remote run is off here (site run on)"))
	}
	if len(args.Argv) == 0 {
		return fail(errors.New("run needs a command"))
	}
	cwd := s.l.Root
	var env []string
	logDir := filepath.Join(s.l.Var(), "run")
	if args.Job != "" {
		if !format.ValidSlug(args.Job) {
			return fail(fmt.Errorf("%q is not a job id", args.Job))
		}
		cwd = s.l.PayloadSub(args.Job, "data/src")
		if _, err := os.Stat(cwd); err != nil {
			cwd = s.l.Payload(args.Job)
		}
		// data/src is a source snapshot, not a repository. A .git here means someone
		// cloned into it; the next sync would delete it, and the branch is not the
		// helper's to own.
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
			return fail(fmt.Errorf("job/%s/data/src contains a .git: it is a source snapshot, not a repository; do not clone into it. Use `thrum sync --code` from the site that holds the job, or work in its worktree", args.Job))
		}
		logDir = s.l.PayloadSub(args.Job, "log")
		env = append(env,
			"THRUM_JOB="+args.Job,
			"TMPDIR="+s.l.PayloadSub(args.Job, "tmp"),
			"XDG_STATE_HOME="+s.l.PayloadSub(args.Job, "state"),
			"XDG_CACHE_HOME="+s.l.PayloadSub(args.Job, "cache"),
		)
	}
	if args.Actor != "" {
		env = append(env, "THRUM_ACTOR="+args.Actor)
	}
	cmd := exec.Command(args.Argv[0], args.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fail(err)
	}
	logPath := filepath.Join(logDir, "run-"+time.Now().UTC().Format("20060102T150405Z")+".log")
	_ = os.WriteFile(logPath, out, 0o644)
	return payload(map[string]any{"stdout": base64.StdEncoding.EncodeToString(out), "exit": code, "log": logPath})
}

// broadcastPlace tells the peers where a name just said hello. It is best effort:
// a peer that cannot be reached answers on the next hello or the next question.
func (s *server) broadcastPlace(actor actorArgs) {
	args, err := json.Marshal(actor)
	if err != nil {
		return
	}
	for peer := range rosterOf(s.l) {
		if peer == s.site {
			continue
		}
		c, err := Dial(s.l, peer)
		if err != nil {
			continue
		}
		_, _ = c.Do(Request{Verb: "actor.hello", Args: args})
		_ = c.Close()
	}
}

// gitServe hands the connection to a git service after one acknowledgement. The
// client waits for one byte before piping, so its JSON decoder cannot swallow the
// advertisement the service writes as soon as it starts.
func (s *server) gitServe(conn net.Conn, br *bufio.Reader, enc *json.Encoder, req Request, joining bool) {
	var args struct {
		Repo    string `json:"repo"`
		Service string `json:"service"`
	}
	if err := json.Unmarshal(req.Args, &args); err != nil {
		_ = enc.Encode(fail(err))
		return
	}
	if joining && args.Repo != "line" && args.Repo != "ledger" && args.Repo != "store" {
		_ = enc.Encode(fail(errors.New("a join carries the line, the ledger and the store only")))
		return
	}
	if joining && args.Service == "receive-pack" && args.Repo != "line" {
		_ = enc.Encode(fail(errors.New("only the line may be written during a join")))
		return
	}
	if args.Service != "upload-pack" && args.Service != "receive-pack" {
		_ = enc.Encode(fail(fmt.Errorf("%q is not a git service", args.Service)))
		return
	}
	gitdir, err := s.repoDir(args.Repo)
	if err != nil {
		_ = enc.Encode(fail(err))
		return
	}
	if err := enc.Encode(Response{OK: true}); err != nil {
		return
	}
	if _, err := br.ReadByte(); err != nil {
		return
	}
	cmd := exec.Command("git", args.Service, gitdir)
	cmd.Stdout = conn
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return
	}
	// The copy is ours, not os/exec's: Wait must not wait for a reader that is
	// still holding the connection open after the service has exited.
	go func() {
		_, _ = io.Copy(stdin, br)
		_ = stdin.Close()
	}()
	_ = cmd.Wait()
}

// historyLinks builds each peer's line, ledger and store transports. They are the
// daemon's, made per exchange and never written into a repository.
func (s *server) historyLinks() exchange.Links {
	var links exchange.Links
	exe, err := os.Executable()
	if err != nil {
		return links
	}
	if strings.ContainsAny(exe, " \t\n") {
		return links
	}
	peers := rosterOf(s.l)
	for name := range peers {
		reach := reachFor(s.l, name, peers)
		if name == s.site || reach == "" || strings.HasSuffix(reach, ":0") || peers[name].Key == "" {
			continue
		}
		mk := func(repo string) (string, string) {
			base := "ext::" + exe + " --root " + s.l.Root +
				" git-proxy --peer " + name + " --repo " + repo
			return base + " --service upload-pack", base + " --service receive-pack"
		}
		lf, lp := mk("line")
		links.Line = append(links.Line, exchange.Link{Name: name, Fetch: lf, Push: lp})
		df, dp := mk("ledger")
		links.Ledger = append(links.Ledger, exchange.Link{Name: name, Fetch: df, Push: dp})
		c, err := dial(s.l, name, true)
		if err != nil {
			continue
		}
		var tags []string
		if resp, err := c.Do(Request{Verb: "store.published"}); err == nil {
			var d struct {
				Tags []string `json:"tags"`
			}
			if json.Unmarshal(resp.Data, &d) == nil {
				tags = d.Tags
			}
		}
		_ = c.Close()
		sf, sp := mk("store")
		links.Store = append(links.Store, exchange.Link{Name: name, Fetch: sf, Push: sp, Tags: tags})
	}
	return links
}

// repoDir names the repository the way the fabric does, never as a path from the
// caller.
func (s *server) repoDir(name string) (string, error) {
	switch name {
	case "line":
		return filepath.Join(s.l.Root, ".git"), nil
	case "ledger":
		return filepath.Join(s.l.Ledger(), ".git"), nil
	case "store":
		return s.l.Store(), nil
	}
	return "", fmt.Errorf("no repository named %q", name)
}

// Reach is where a site's daemon listens, from this machine's view.
func Reach(l layout.Layout, site string) string {
	return reachFor(l, site, rosterOf(l))
}

// PublicKey is this site's current public key.
func PublicKey(l layout.Layout) (string, error) {
	key, err := loadKey(l)
	if err != nil {
		return "", err
	}
	return workspace.PublicKey(key.Public().(ed25519.PublicKey)), nil
}

func payload(v any) Response {
	data, err := json.Marshal(v)
	if err != nil {
		return fail(err)
	}
	return Response{OK: true, Data: data}
}

// fileDir is where `copy` lands: the job's payload data/, or a site scratch
// directory when no job is named.
func fileDir(s *server, job string) (string, error) {
	if job == "" {
		return filepath.Join(s.l.Var(), "scratch"), nil
	}
	if !format.ValidSlug(job) {
		return "", fmt.Errorf("%q is not a job id", job)
	}
	return s.l.PayloadSub(job, "data"), nil
}

func fail(err error) Response {
	return Response{Error: err.Error()}
}

func workspaceID(l layout.Layout) (string, error) {
	out, err := gitx.Run(l.Root, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", fmt.Errorf("the line has no first commit")
	}
	return fields[0], nil
}

func reachFor(l layout.Layout, site string, roster map[string]records.Site) string {
	if b, err := os.ReadFile(filepath.Join(l.ConfigDir(), site+".yaml")); err == nil {
		var cfg struct {
			Via   string `yaml:"via"`
			Reach string `yaml:"reach"`
		}
		if err := format.DecodeWhole(b, &cfg); err == nil {
			if cfg.Via != "" {
				return cfg.Via
			}
			if cfg.Reach != "" {
				return cfg.Reach
			}
		}
	}
	if s, ok := roster[site]; ok && s.Addr() != "" {
		return s.Addr()
	}
	return discoveredReach(l, site, discoveredFor)
}

func readLock(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("unreadable lock")
	}
	return pid, nil
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// loadPrevKey is the key a rotation retired; it is what proves the new one to a
// peer that still has the old public half.
func loadPrevKey(l layout.Layout) (ed25519.PrivateKey, bool) {
	b, err := os.ReadFile(l.PrivateKey() + ".prev")
	if err != nil {
		return nil, false
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, false
	}
	return ed25519.NewKeyFromSeed(seed), true
}

func loadKey(l layout.Layout) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(l.PrivateKey())
	if err != nil {
		return nil, fmt.Errorf("this site has no key: %w", err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is not a site key seed", l.PrivateKey())
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// held is how many messages this site is holding, which is what `daemon status`
// reports besides what it is.
func held(l layout.Layout) int {
	dirs, err := os.ReadDir(l.Mailboxes())
	if err != nil {
		return 0
	}
	n := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(l.Mailboxes(), d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
				n++
			}
		}
	}
	return n
}
