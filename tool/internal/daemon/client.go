package daemon

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Client is a connection to one peer's daemon.
type Client struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
}

// Do sends one request and reads its answer.
func (c *Client) Do(req Request) (Response, error) {
	if err := c.enc.Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Dial opens an authenticated connection to a peer's daemon.
func Dial(l layout.Layout, peer string) (*Client, error) {
	site := actors.Site(l)
	roster := rosterOf(l)
	target, ok := roster[peer]
	if !ok {
		return nil, fmt.Errorf("site %q is not declared", peer)
	}
	addr := reachFor(l, peer, roster)
	if addr == "" {
		return nil, fmt.Errorf("no reach is entered for %q, so where it is cannot be established", peer)
	}
	key, err := loadKey(l)
	if err != nil {
		return nil, err
	}
	ws, err := workspaceID(l)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%s did not answer: %w", peer, err)
	}
	br := bufio.NewReader(conn)
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(br)

	clientNonce := nonce()
	if err := enc.Encode(greeting{Workspace: ws, Site: site, Version: protocolVersion, Nonce: clientNonce}); err != nil {
		conn.Close()
		return nil, err
	}
	var reply greeting
	if err := dec.Decode(&reply); err != nil {
		conn.Close()
		return nil, err
	}
	if reply.Workspace != ws {
		conn.Close()
		return nil, fmt.Errorf("that site is in a different workspace")
	}
	pub, err := workspace.ParseSiteKey(target.Key)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !verify(pub, clientNonce, reply.Signature) {
		conn.Close()
		return nil, fmt.Errorf("site %q did not prove its key", peer)
	}
	if err := enc.Encode(greeting{Signature: sign(key, reply.Nonce)}); err != nil {
		conn.Close()
		return nil, err
	}
	return &Client{conn: conn, enc: enc, dec: dec}, nil
}

// Push hands a message to the site that takes it.
func Push(l layout.Layout, site string, msg records.Message, body []byte, files map[string][]byte) error {
	c, err := Dial(l, site)
	if err != nil {
		return err
	}
	defer c.Close()

	encoded := map[string]string{}
	for name, data := range files {
		encoded[filepath.Base(name)] = base64.StdEncoding.EncodeToString(data)
	}
	args, err := json.Marshal(pushArgs{
		Message: msg,
		Body:    base64.StdEncoding.EncodeToString(body),
		Files:   encoded,
	})
	if err != nil {
		return err
	}
	_, err = c.Do(Request{Verb: "msg.push", Args: args})
	return err
}

// LocalDo asks this machine's daemon. A missing socket is not an error the caller has
// to care about: it means no daemon is running, which most operations do not need.
func LocalDo(l layout.Layout, req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", l.DaemonSocket(), time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

func rosterOf(l layout.Layout) map[string]records.Site {
	out := map[string]records.Site{}
	entries, err := os.ReadDir(l.Sites())
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		var s records.Site
		ok, err := records.Read(filepath.Join(l.Sites(), e.Name()), &s)
		if err == nil && ok {
			out[s.Site] = s
		}
	}
	return out
}

func nonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func sign(key ed25519.PrivateKey, value string) string {
	return hex.EncodeToString(ed25519.Sign(key, []byte(value)))
}

func verify(pub ed25519.PublicKey, value, signature string) bool {
	sig, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, []byte(value), sig)
}
