package daemon

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ForcedCommand is the one command a peer may ask for. A peer runs one command per
// request, over the same channel it would use to use the resource, and there is no
// shell on the other end of it.
const ForcedCommand = "thrum-daemon"

// DialSSH opens a connection to a peer.
func DialSSH(ref PeerRef, l workspace.Layout, site string) (*sshClient, error) {
	network, address, user, err := reachTarget(ref)
	if err != nil {
		return nil, err
	}

	signer, err := clientKey(l, ref)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		// The transport is SSH, and its authentication is SSH's: keys that the other
		// end already lists. What this adds is only a check that the host is the one
		// this site has pinned, when it has pinned one.
		HostKeyCallback: hostKeyCallback(l),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial(network, address, config)
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	if err := session.Start(ForcedCommand); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("%s did not accept the forced command: %w", ref.Name, err)
	}
	return &sshClient{client: client, session: session, stdin: stdin, stdout: stdout}, nil
}

// sshClient is one request to one peer, on one channel.
type sshClient struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func (c *sshClient) Do(req Request) (Response, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if _, err := c.stdin.Write(append(payload, '\n')); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(c.stdout).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

func (c *sshClient) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	err := c.session.Wait()
	c.client.Close()
	if err != nil {
		// The remote side closing after answering is the normal ending.
		if _, ok := err.(*ssh.ExitError); ok {
			return nil
		}
	}
	return nil
}

// ServeSSH answers peers on a listener, with the same handler the socket uses.
func ServeSSH(l workspace.Layout, h Handler, ln net.Listener) error {
	config, err := serverConfig(l)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thrum daemon: cannot accept peers: %v\n", err)
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handleSSH(conn, config, h, l)
	}
}

func handleSSH(conn net.Conn, config *ssh.ServerConfig, h Handler, l workspace.Layout) {
	defer conn.Close()

	server, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		// A peer that cannot get in is worth saying out loud: the alternative is a
		// connection that fails somewhere else, for a reason nobody can see.
		fmt.Fprintf(os.Stderr, "thrum daemon: refused a peer: %v\n", err)
		return
	}
	defer server.Close()
	go ssh.DiscardRequests(reqs)

	for channel := range chans {
		if channel.ChannelType() != "session" {
			_ = channel.Reject(ssh.UnknownChannelType, "only a session, and only for one command")
			continue
		}
		ch, requests, err := channel.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range requests {
				if req.Type != "exec" {
					// No shell, no terminal, no forwarding: a peer gets one command.
					_ = req.Reply(false, nil)
					continue
				}
				command := commandOf(req.Payload)
				var status uint32
				switch {
				case command == ForcedCommand:
					_ = req.Reply(true, nil)
					if err := Exec(h, ch, ch); err != nil {
						status = 1
					}
				default:
					// Serving this site's repositories is the other half of what a
					// peer needs: the records travel by git, and git needs a
					// repository to fetch from. Only this workspace's own
					// repositories are served, whatever path is asked for.
					verb, path, ok := gitVerb(command)
					if !ok || !servedRepo(l, path) {
						_ = req.Reply(false, nil)
						return
					}
					_ = req.Reply(true, nil)
					status = runGit(ch, verb, path)
				}
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				return
			}
		}()
	}
}

// gitVerb reads the git service a peer asked for, if that is what it asked for. The
// path comes from git itself, and is only ever used after being checked.
func gitVerb(command string) (verb, path string, ok bool) {
	verb, rest, found := strings.Cut(strings.TrimSpace(command), " ")
	if !found {
		return "", "", false
	}
	verb = strings.TrimPrefix(verb, "git-")
	switch verb {
	case "upload-pack", "receive-pack":
	default:
		return "", "", false
	}
	path = strings.Trim(strings.TrimSpace(rest), "'\"")
	return verb, path, path != ""
}

// servedRepo reports whether a path is one of this workspace's repositories. These are
// the only things a peer may ask for, and the check is what stops a peer asking for
// anything else on the machine.
func servedRepo(l workspace.Layout, path string) bool {
	asked, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, candidate := range []string{l.Root, l.CodeRepo(), l.Store()} {
		allowed, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if asked == allowed {
			return true
		}
	}
	return false
}

// runGit runs one git service on the channel. Its standard error is kept here rather
// than sent down the channel: the channel carries the protocol, and anything else on it
// is corruption.
func runGit(ch io.ReadWriter, verb, path string) uint32 {
	cmd := exec.Command("git", verb, path)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")
	cmd.Stdin = ch
	cmd.Stdout = ch
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "thrum daemon: git %s %s: %v: %s\n", verb, path, err, strings.TrimSpace(stderr.String()))
		return 1
	}
	return 0
}

// commandOf reads the command out of an exec request, which is a length-prefixed
// string.
func commandOf(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := binary.BigEndian.Uint32(payload[:4])
	if uint32(len(payload)-4) < n {
		return ""
	}
	return string(payload[4 : 4+n])
}

// serverConfig builds the SSH side: a host key, and the keys that may talk to it.
func serverConfig(l workspace.Layout) (*ssh.ServerConfig, error) {
	signer, err := hostKey(l)
	if err != nil {
		return nil, err
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			allowed, err := authorizedKeys(l)
			if err != nil {
				return nil, err
			}
			for _, k := range allowed {
				if bytes.Equal(k.Marshal(), key.Marshal()) {
					return nil, nil
				}
			}
			return nil, fmt.Errorf("this key is not listed in authorized_keys")
		},
	}
	config.AddHostKey(signer)
	return config, nil
}

// hostKey loads the daemon's host key, making one the first time. A host key is a
// secret, so it lives with the other secrets rather than in the workspace's records.
func hostKey(l workspace.Layout) (ssh.Signer, error) {
	path := filepath.Join(l.Secrets(), "daemon_host_key")
	if raw, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(raw)
	}
	if err := os.MkdirAll(l.Secrets(), 0o700); err != nil {
		return nil, err
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(der), 0o600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(private)
}

// authorizedKeys reads the keys that may talk to this daemon.
func authorizedKeys(l workspace.Layout) ([]ssh.PublicKey, error) {
	raw, err := os.ReadFile(filepath.Join(l.Secrets(), "authorized_keys"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []ssh.PublicKey
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("authorized_keys: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// clientKey finds the private key this site uses to talk to peers.
func clientKey(l workspace.Layout, ref PeerRef) (ssh.Signer, error) {
	candidates := []string{}
	if ref.Key != "" {
		candidates = append(candidates, ref.Key)
	}
	candidates = append(candidates, filepath.Join(l.Secrets(), "id_ed25519"))
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".ssh", "id_ed25519"))
	}
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return signer, nil
	}
	return nil, fmt.Errorf("no private key for %s: put one in %s, or name one with key:",
		ref.Name, filepath.Join(l.Secrets(), "id_ed25519"))
}

// hostKeyCallback pins a peer's host key when this site has pinned one.
func hostKeyCallback(l workspace.Layout) ssh.HostKeyCallback {
	path := filepath.Join(l.Secrets(), "known_hosts")
	if _, err := os.Stat(path); err == nil {
		if cb, err := knownhosts.New(path); err == nil {
			return cb
		}
	}
	// With nothing pinned, the transport is trusted the way the operator already
	// trusts it: this is a collective's own machines on their own network, and the
	// roster that names them is versioned.
	return ssh.InsecureIgnoreHostKey()
}

// reachTarget reads the site-local half: host, host:port or user@host:port, or a unix
// socket for a peer reached through a tunnel rather than over the network.
//
// How to reach a site is the site's own business and travels nowhere, so the form is
// whatever the machine that wrote it needs.
func reachTarget(ref PeerRef) (network, address, user string, err error) {
	reach := strings.TrimSpace(ref.Reach)
	if reach == "" {
		return "", "", "", fmt.Errorf("%s has no reach entered for it here", ref.Name)
	}
	if before, after, ok := strings.Cut(reach, "@"); ok {
		user, reach = before, after
	}
	if user == "" {
		user = ref.SSHUser
	}
	if user == "" {
		// A THRUM_SSH_USER is the one piece of a collective's local environment that
		// the site-local half cannot supply, because it is about the other machine.
		user = os.Getenv("THRUM_SSH_USER")
	}
	if user == "" {
		user = "root"
	}

	if path, ok := strings.CutPrefix(reach, "unix:"); ok {
		return "unix", path, user, nil
	}
	if strings.HasPrefix(reach, "/") {
		return "unix", reach, user, nil
	}

	host, port, splitErr := net.SplitHostPort(reach)
	if splitErr != nil {
		host, port = reach, "22"
	}
	if host == "" {
		return "", "", "", fmt.Errorf("%q names no host", ref.Reach)
	}
	return "tcp", net.JoinHostPort(host, port), user, nil
}
