package roster_test

import (
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/roster"
)

// A site learns the names its peers have seen, and keeps the older of two facts about one
// name: where it is, is where it was heard from most recently.
func TestLearningARosterKeepsTheNewestFact(t *testing.T) {
	l := fixture(t, "build-01")
	early := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)

	if err := roster.Seen(l, "alice", "j1", early); err != nil {
		t.Fatal(err)
	}

	learned, err := roster.Learn(l, "lab-02", []roster.Entry{{
		Name: "alice", Site: "lab-02", FirstSeen: late.Format(time.RFC3339),
		LastSeen: late.Format(time.RFC3339), Job: "j2",
	}, {
		Name: "bob", Site: "lab-02", FirstSeen: late.Format(time.RFC3339),
		LastSeen: late.Format(time.RFC3339),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if learned != 2 {
		t.Errorf("both the newer alice and the new bob are changes, and it counted %d", learned)
	}

	entries, err := roster.Local(l)
	if err != nil {
		t.Fatal(err)
	}
	alice, ok := roster.Find("alice", entries)
	if !ok {
		t.Fatal("alice should still be here")
	}
	if alice.Site != "lab-02" || alice.Job != "j2" {
		t.Errorf("the newest fact about alice wins, and this site kept %+v", alice)
	}
	if _, ok := roster.Find("bob", entries); !ok {
		t.Error("bob was learned and should be here")
	}

	// Learning it again changes nothing, which is what keeps an idle pair of daemons quiet.
	again, err := roster.Learn(l, "lab-02", entries)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Errorf("learning what is already known is not a change, and it counted %d", again)
	}
}

// The digest is what lets a site tell whether it has anything to say without sending anything:
// it is the same for the same roster, whatever order it arrived in.
func TestTheDigestIgnoresOrder(t *testing.T) {
	one := []roster.Entry{{Name: "alice", LastSeen: "b"}, {Name: "bob", LastSeen: "a"}}
	two := []roster.Entry{{Name: "bob", LastSeen: "a"}, {Name: "alice", LastSeen: "b"}}
	if roster.Digest(one) != roster.Digest(two) {
		t.Error("the same roster in another order is the same roster")
	}
	if roster.Digest(one) == roster.Digest([]roster.Entry{{Name: "alice", LastSeen: "c"}}) {
		t.Error("a changed fact is a changed digest")
	}
}
