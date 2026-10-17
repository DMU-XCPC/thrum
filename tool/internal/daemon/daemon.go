// Package daemon is a site's endpoint: its own listener and protocol, the last-known
// places, and the mailboxes. It is a cache and a channel, never an authority.
package daemon

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

const protocolVersion = "thrum/1"

// Request is one call.
type Request struct {
	Verb string          `json:"verb"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is one answer.
type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type greeting struct {
	Workspace string `json:"workspace"`
	Site      string `json:"site"`
	Version   string `json:"version"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature,omitempty"`
	Error     string `json:"error,omitempty"`
}

type server struct {
	l         layout.Layout
	site      string
	roster    map[string]records.Site
	workspace string
	stop      chan struct{}
	once      sync.Once
}

// Run listens until it is stopped. The TCP listener serves peers; the unix socket
// serves this machine's commands.
func Run(l layout.Layout, listen string) error {
	s, err := newServer(l)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.Var(), 0o755); err != nil {
		return err
	}
	if pid, err := readLock(l.DaemonLock()); err == nil {
		if processAlive(pid) {
			return fmt.Errorf("a daemon is already running here (pid %d)", pid)
		}
		_ = os.Remove(l.DaemonLock())
	} else if !os.IsNotExist(err) {
		// A lock nobody can read is a leftover, not a daemon.
		_ = os.Remove(l.DaemonLock())
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
		addr = reachFor(l, s.site, s.roster)
	}
	if addr == "" {
		return fmt.Errorf("this site declares no reach: set one with `thrum site reach %s --at <host:port>`", s.site)
	}

	_ = os.Remove(l.DaemonSocket())
	unix, err := net.Listen("unix", l.DaemonSocket())
	if err != nil {
		return err
	}
	_ = os.Chmod(l.DaemonSocket(), 0o600)
	defer os.Remove(l.DaemonSocket())

	tcp, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer tcp.Close()

	fmt.Printf("listening on %s\n", tcp.Addr())
	go s.accept(unix, false)
	go s.accept(tcp, true)

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
	return &server{l: l, site: site, roster: roster, workspace: ws, stop: make(chan struct{})}, nil
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
			if remote {
				if err := s.auth(conn, br); err != nil {
					return
				}
			}
			s.serve(conn, br, remote)
		}()
	}
}

func (s *server) auth(conn net.Conn, br *bufio.Reader) error {
	dec := json.NewDecoder(br)
	enc := json.NewEncoder(conn)
	refuse := func(format string, args ...any) error {
		msg := fmt.Sprintf(format, args...)
		_ = enc.Encode(greeting{Error: msg})
		return fmt.Errorf("%s", msg)
	}

	var hello greeting
	if err := dec.Decode(&hello); err != nil {
		return err
	}
	if hello.Workspace != s.workspace {
		return refuse("that site is in a different workspace")
	}
	if hello.Version != protocolVersion {
		return refuse("that site speaks protocol %q, this site speaks %q", hello.Version, protocolVersion)
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
	key, err := loadKey(s.l)
	if err != nil {
		return refuse("this site has no usable key")
	}

	serverNonce := nonce()
	if err := enc.Encode(greeting{
		Workspace: s.workspace, Site: s.site, Version: protocolVersion,
		Nonce: serverNonce, Signature: sign(key, hello.Nonce),
	}); err != nil {
		return err
	}
	var proof greeting
	if err := dec.Decode(&proof); err != nil {
		return err
	}
	if !verify(pub, serverNonce, proof.Signature) {
		return refuse("site %q did not prove its key", hello.Site)
	}
	return enc.Encode(greeting{})
}

func (s *server) serve(conn net.Conn, br *bufio.Reader, remote bool) {
	dec := json.NewDecoder(br)
	enc := json.NewEncoder(conn)
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}
		_ = enc.Encode(s.dispatch(req, remote))
	}
}

type actorArgs struct {
	Actor string `json:"actor"`
}

type pushArgs struct {
	Message records.Message   `json:"message"`
	Body    string            `json:"body,omitempty"`
	Files   map[string]string `json:"files,omitempty"`
}

func (s *server) dispatch(req Request, remote bool) Response {
	switch req.Verb {
	case "site.hello":
		return payload(map[string]any{
			"workspace": s.workspace, "site": s.site, "version": protocolVersion,
		})
	case "actor.where":
		var args actorArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		place, ok := actors.Places(s.l).Actors[args.Actor]
		if !ok {
			return payload(map[string]any{"here": false})
		}
		return payload(map[string]any{"here": true, "site": place.Site, "heard": place.Heard})
	case "actor.here":
		var args actorArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
		}
		if err := actors.Hello(s.l, args.Actor, time.Now()); err != nil {
			return fail(err)
		}
		return payload(map[string]bool{"ok": true})
	case "msg.push":
		var args pushArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return fail(err)
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

func payload(v any) Response {
	data, err := json.Marshal(v)
	if err != nil {
		return fail(err)
	}
	return Response{OK: true, Data: data}
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
			Reach string `yaml:"reach"`
		}
		if err := format.DecodeWhole(b, &cfg); err == nil && cfg.Reach != "" {
			return cfg.Reach
		}
	}
	if s, ok := roster[site]; ok {
		return s.Reach
	}
	return ""
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
