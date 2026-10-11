package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// A site learns the names its peers have seen, both ways, without either being told directly.
func TestASiteLearnsTheRosterFromItsPeers(t *testing.T) {
	build := site(t, "build-01")
	lab := site(t, "lab-02")
	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	shareKeys(t, build, lab)
	writeRoster(t, build, lab)
	writeReach(t, build, "lab-02", serveSSH(t, daemon.NewService(lab)))
	writeReach(t, lab, "build-01", serveSSH(t, daemon.NewService(build)))

	now := time.Now().UTC()
	if err := roster.Seen(build, "alice", "j1", now); err != nil {
		t.Fatal(err)
	}
	if err := roster.Seen(lab, "bob", "", now); err != nil {
		t.Fatal(err)
	}

	daemon.SyncRoster(build)
	daemon.SyncRoster(lab)

	for _, want := range []struct {
		l    workspace.Layout
		name string
	}{{build, "bob"}, {lab, "alice"}} {
		entries, err := roster.Local(want.l)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := roster.Find(want.name, entries); !ok {
			t.Errorf("a site that exchanged with its peer should know %s: %+v", want.name, entries)
		}
	}

	// A name once learned is known without asking anybody: that is what makes it answerable
	// while the site that has it is quiet. With no address for the peer left at all, the
	// name is still here.
	if err := os.Remove(filepath.Join(build.Config(), "lab-02.yaml")); err != nil {
		t.Fatal(err)
	}
	entries, err := roster.Local(build)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := roster.Find("bob", entries); !ok {
		t.Error("the name should not depend on being able to ask the site that has it")
	}
}

// A site tells its peers what it learned from the others, so knowledge reaches a site it never
// exchanged with.
func TestALearnedNameTravelsOnToAThirdSite(t *testing.T) {
	build := site(t, "build-01")
	lab := site(t, "lab-02")
	cache := site(t, "cache-03")

	writeKeys(t, build, "A")
	writeKeys(t, lab, "B")
	writeKeys(t, cache, "C")
	shareKeys(t, build, lab)
	writeRoster(t, build, lab)
	writeReach(t, build, "lab-02", serveSSH(t, daemon.NewService(lab)))
	writeReach(t, lab, "build-01", serveSSH(t, daemon.NewService(build)))

	if err := roster.Seen(build, "alice", "j1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// The two exchange, so lab-02 now knows alice.
	daemon.SyncRoster(build)
	daemon.SyncRoster(lab)
	if entries, err := roster.Local(lab); err != nil {
		t.Fatal(err)
	} else if _, ok := roster.Find("alice", entries); !ok {
		t.Fatalf("lab-02 should have learned alice before passing it on: %+v", entries)
	}

	// A third site that only ever talks to lab-02 declares itself and that peer, and gets
	// alice anyway: a site tells its peers what it learned, not only what it saw.
	declared := "site:\n  - name: cache-03\n    purpose: The cache machine\n  - name: lab-02\n    purpose: The lab machine\n"
	if err := os.WriteFile(cache.ResourceKind("site"), []byte(declared), 0o644); err != nil {
		t.Fatal(err)
	}
	shareKeys(t, cache, lab)
	writeReach(t, cache, "lab-02", serveSSH(t, daemon.NewService(lab)))

	daemon.SyncRoster(cache)
	entries, err := roster.Local(cache)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := roster.Find("alice", entries)
	if !ok {
		t.Fatalf("a name learned by a peer should reach the next site as well: %+v", entries)
	}
	if entry.Site != "build-01" {
		t.Errorf("the name should still say where it was heard from, and it says %q", entry.Site)
	}
}
