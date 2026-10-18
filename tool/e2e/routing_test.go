package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// TestForwardLoopAndHopLimit drives msg.forward directly: a relay that finds its
// own site in the path must refuse, and a walk past the hop limit must stop.
func TestForwardLoopAndHopLimit(t *testing.T) {
	ts := setupTwoSites(t)
	twoSiteDaemons(t, ts)
	c, err := daemon.Dial(layout.New(ts.w2.root), "s1")
	if err != nil {
		t.Fatalf("dialling the peer: %v", err)
	}
	defer c.Close()

	msg := records.Message{ID: "loop-1", From: "a2", To: "a1", Site: "s2"}
	args, _ := json.Marshal(map[string]any{
		"to": "s2", "message": msg, "seen": []string{"s1"}, "hops": 1,
	})
	if _, err := c.Do(daemon.Request{Verb: "msg.forward", Args: args}); err == nil || !strings.Contains(err.Error(), "looped") {
		t.Fatalf("a loop was not reported: %v", err)
	}
	args, _ = json.Marshal(map[string]any{
		"to": "s2", "message": msg, "hops": 99,
	})
	if _, err := c.Do(daemon.Request{Verb: "msg.forward", Args: args}); err == nil || !strings.Contains(err.Error(), "hop limit") {
		t.Fatalf("the hop limit was not reported: %v", err)
	}
}
