package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// placedSites is two sites with both daemons up, and a sender at each end.
func newPlacedSites(t *testing.T) *placedSites {
	t.Helper()
	ts := setupTwoSites(t)
	p1 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	p2 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ts.w1.r().run("site", "reach", "s2", "--at", p2).ok(t, "w1 reaches s2")
	ts.w2.r().run("site", "reach", "s1", "--at", p1).ok(t, "w2 reaches s1")
	startDaemon(t, ts.w1, p1)
	startDaemon(t, ts.w2, p2)
	ts.w1.r().as("a3").run("actor", "declare", "a3", "--kind", "agent").ok(t, "declare a3")
	ts.w1.r().run("sync").ok(t, "sync a3")
	ts.w2.r().run("sync").ok(t, "w2 sees a3")
	return &placedSites{ts: ts, p1: p1, p2: p2}
}

type placedSites struct {
	ts     *twoSites
	p1, p2 string
}

func waitForPlace(t *testing.T, w *workspace, actor, site string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		list := w.r().run("actor", "list").ok(t, "actor list").stdout
		for _, ln := range strings.Split(list, "\n") {
			fields := strings.Fields(ln)
			if len(fields) >= 3 && fields[0] == actor && fields[2] == site {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never saw %s at %s:\n%s", w.root, actor, site, list)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestHelloMovesMail: a hello at the new site is broadcast, and the old site then
// sends the next message to the new one.
func TestHelloMovesMail(t *testing.T) {
	ps := newPlacedSites(t)
	ts := ps.ts
	ts.w1.r().as("a1").run("actor", "hello").ok(t, "a1 hello at s1")
	// The heard stamp has second granularity; a later hello must be in a later second
	// for the late first broadcast not to tie with it.
	time.Sleep(1100 * time.Millisecond)
	ts.w2.r().as("a1").run("actor", "hello").ok(t, "a1 hello at s2")
	waitForPlace(t, ts.w1, "a1", "s2")

	ts.w1.r().as("a3").run("message", "send", "a1", "--subject", "you moved").ok(t, "send")
	if mail := ts.w2.r().as("a1").run("message", "list").ok(t, "s2 mail"); !strings.Contains(mail.stdout, "you moved") {
		t.Fatalf("the message did not follow the hello: %s", mail.stdout)
	}
	if mail := ts.w1.r().as("a1").run("message", "list").ok(t, "s1 mail"); strings.Contains(mail.stdout, "you moved") {
		t.Fatalf("the message stayed at the old site: %s", mail.stdout)
	}
}

// TestSendAsksPeers: with no local answer, the sender's daemon asks the peers.
func TestSendAsksPeers(t *testing.T) {
	ps := newPlacedSites(t)
	ts := ps.ts
	ts.w2.r().as("a1").run("actor", "hello").ok(t, "a1 hello at s2")
	waitForPlace(t, ts.w1, "a1", "s2")
	// Forget the answer locally: only the daemon's question can place the name.
	ts.w1.write("var/location.yaml", "actors: {}\n")

	ts.w1.r().as("a3").run("message", "send", "a1", "--subject", "asked").ok(t, "send")
	if mail := ts.w2.r().as("a1").run("message", "list").ok(t, "s2 mail"); !strings.Contains(mail.stdout, "asked") {
		t.Fatalf("the daemon's question did not find the actor: %s", mail.stdout)
	}
}

// TestNotHereRetries: a third site still points at the old one; the old one says
// not here, and the send retries once at the site it names.
func TestNotHereRetries(t *testing.T) {
	ts := setupThreeSites(t)
	p1 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	p2 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	p3 := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ts.w1.r().run("site", "reach", "s2", "--at", p2).ok(t, "w1 reaches s2")
	ts.w1.r().run("site", "reach", "s3", "--at", p3).ok(t, "w1 reaches s3")
	ts.w2.r().run("site", "reach", "s1", "--at", p1).ok(t, "w2 reaches s1")
	ts.w2.r().run("site", "reach", "s3", "--at", p3).ok(t, "w2 reaches s3")
	ts.w3.r().run("site", "reach", "s1", "--at", p1).ok(t, "w3 reaches s1")
	ts.w3.r().run("site", "reach", "s2", "--at", p2).ok(t, "w3 reaches s2")
	startDaemon(t, ts.w1, p1)
	startDaemon(t, ts.w2, p2)
	startDaemon(t, ts.w3, p3)

	ts.w1.r().run("actor", "declare", "a1", "--kind", "agent").ok(t, "declare a1")
	ts.w3.r().as("a3").run("actor", "declare", "a3", "--kind", "agent").ok(t, "declare a3")
	ts.w1.r().run("sync").ok(t, "sync a1")
	ts.w2.r().run("sync").ok(t, "sync a1 to s2")
	ts.w3.r().run("sync").ok(t, "sync a1 to s3")

	ts.w2.r().as("a1").run("actor", "hello").ok(t, "a1 hello at s2")
	waitForPlace(t, ts.w1, "a1", "s2")
	waitForPlace(t, ts.w3, "a1", "s2")

	// Site three now has the stale address: it still points at s1. Site one knows
	// the actor moved, so it answers not here and names s2.
	ts.w3.write("var/location.yaml", "actors:\n  a1:\n    site: s1\n    heard: 2026-01-01T00:00:00Z\n")
	ts.w3.r().as("a3").run("message", "send", "a1", "--subject", "stale address").ok(t, "send through the stale address")
	if mail := ts.w2.r().as("a1").run("message", "list").ok(t, "s2 mail"); !strings.Contains(mail.stdout, "stale address") {
		t.Fatalf("the retry did not reach the actor: %s", mail.stdout)
	}
}
