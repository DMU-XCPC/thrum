package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

type wireGreeting struct {
	Workspace string `json:"workspace"`
	Site      string `json:"site"`
	Version   string `json:"version"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
	Error     string `json:"error"`
}

func rawHello(t *testing.T, addr string, hello wireGreeting) wireGreeting {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", addr, err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		t.Fatal(err)
	}
	var reply wireGreeting
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		t.Fatalf("reading the reply: %v", err)
	}
	return reply
}

func rawProof(t *testing.T, addr string, hello, proof wireGreeting) wireGreeting {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", addr, err)
	}
	defer conn.Close()
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(hello); err != nil {
		t.Fatal(err)
	}
	var reply wireGreeting
	if err := dec.Decode(&reply); err != nil {
		t.Fatalf("reading the greeting: %v", err)
	}
	if reply.Error != "" {
		return reply
	}
	if err := enc.Encode(proof); err != nil {
		t.Fatal(err)
	}
	var ack wireGreeting
	if err := dec.Decode(&ack); err != nil {
		t.Fatalf("reading the proof's answer: %v", err)
	}
	return ack
}

func TestDaemonRefusesBadHandshakes(t *testing.T) {
	w := newWorkspace(t)
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	startDaemon(t, w, addr)
	ws := strings.TrimSpace(runGit(t, w.root, "rev-list", "--max-parents=0", "HEAD"))

	cases := []struct {
		name  string
		hello wireGreeting
		want  string
	}{
		{"another workspace", wireGreeting{Workspace: "not-this-one", Site: "s1", Version: "thrum/1", Nonce: "a"}, "workspace"},
		{"another protocol", wireGreeting{Workspace: ws, Site: "s1", Version: "thrum/2", Nonce: "a"}, "protocol"},
		{"an undeclared site", wireGreeting{Workspace: ws, Site: "stranger", Version: "thrum/1", Nonce: "a"}, "not declared"},
	}
	for _, c := range cases {
		reply := rawHello(t, addr, c.hello)
		if !strings.Contains(reply.Error, c.want) {
			t.Fatalf("%s: want %q in the refusal, got %+v", c.name, c.want, reply)
		}
	}

	bad := rawProof(t, addr,
		wireGreeting{Workspace: ws, Site: "s1", Version: "thrum/1", Nonce: "abc"},
		wireGreeting{Signature: "00"})
	if !strings.Contains(bad.Error, "prove") {
		t.Fatalf("a bad proof was not refused: %+v", bad)
	}

	w.r().run("daemon", "status").ok(t, "the daemon survived the refusals")
}

func TestPeerCannotStopTheDaemon(t *testing.T) {
	ts := setupTwoSites(t)
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ts.w2.r().run("site", "reach", "s1", "--at", addr).ok(t, "reach")
	startDaemon(t, ts.w1, addr)

	client, err := daemon.Dial(layout.New(ts.w2.root), "s1")
	if err != nil {
		t.Fatalf("dialing the peer: %v", err)
	}
	defer client.Close()
	if _, err := client.Do(daemon.Request{Verb: "stop"}); err == nil || !strings.Contains(err.Error(), "local only") {
		t.Fatalf("a peer's stop: %v", err)
	}
	ts.w1.r().run("daemon", "status").ok(t, "the daemon survived the peer's stop")
}

func TestUnknownVerbAndGarbage(t *testing.T) {
	ts := setupTwoSites(t)
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ts.w2.r().run("site", "reach", "s1", "--at", addr).ok(t, "reach")
	startDaemon(t, ts.w1, addr)

	client, err := daemon.Dial(layout.New(ts.w2.root), "s1")
	if err != nil {
		t.Fatalf("dialing the peer: %v", err)
	}
	if _, err := client.Do(daemon.Request{Verb: "does.not.exist"}); err == nil || !strings.Contains(err.Error(), "unknown verb") {
		t.Fatalf("an unknown verb: %v", err)
	}
	client.Close()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("this is not json\n"))
	_ = conn.Close()
	ts.w1.r().run("daemon", "status").ok(t, "the daemon survived garbage")
}

func TestKeyRotationTakesEffect(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "before").ok(t, "before the rotation")

	ts.w1.r().run("site", "key", "rotate").ok(t, "rotate")
	ts.w1.r().run("sync").ok(t, "publish the rotation")
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "with the old key").
		fails(t, "the old key", "did not prove")

	ts.w2.r().run("sync").ok(t, "learn the new key")
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "after the rotation").ok(t, "after")
}

func TestRevokedKeyIsRefused(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "before").ok(t, "before the revocation")

	ts.w1.r().run("site", "key", "revoke", "s2").ok(t, "revoke")
	ts.w1.r().run("sync").ok(t, "publish the revocation")
	ts.w2.r().as("a2").run("message", "send", "a1", "--subject", "with a revoked key").
		fails(t, "the revoked key", "no key")

	ts.w1.r().run("daemon", "status").ok(t, "the daemon survived the revoked key")
}
