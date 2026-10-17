package e2e

import (
	"strings"
	"sync"
	"testing"
)

// TestConcurrentSync: two sites publish at the same time. One push loses the race;
// the retry fetches, merges and publishes, and both sites converge.
func TestConcurrentSync(t *testing.T) {
	ts := setupTwoSites(t)
	ts.w1.r().run("actor", "declare", "a4", "--kind", "agent").ok(t, "a4")
	ts.w2.r().run("actor", "declare", "a5", "--kind", "agent").ok(t, "a5")

	var wg sync.WaitGroup
	results := make([]result, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results[0] = ts.w1.r().run("sync") }()
	go func() { defer wg.Done(); results[1] = ts.w2.r().run("sync") }()
	wg.Wait()
	results[0].ok(t, "w1 sync")
	results[1].ok(t, "w2 sync")
	ts.w1.r().run("sync").ok(t, "w1 settles")
	ts.w2.r().run("sync").ok(t, "w2 settles")

	for _, w := range []*workspace{ts.w1, ts.w2} {
		list := w.r().run("actor", "list").ok(t, "list").stdout
		if !strings.Contains(list, "a4") || !strings.Contains(list, "a5") {
			t.Fatalf("%s does not see both actors:\n%s", w.root, list)
		}
	}
}
