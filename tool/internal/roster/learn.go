package roster

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/atomic"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"gopkg.in/yaml.v3"
)

// Learn merges what a peer says it has seen into this site's roster.
//
// This is the daemon-to-daemon half of the roster being a service: a site knows the names it
// has seen itself and the names its peers have told it about, and it tells them what it learns
// in turn. The merge rule is the one queries already use — the site that heard from a name
// most recently is where it is — so two sites that tell each other converge without either
// being authoritative, and a name learned here keeps the site that actually has it.
//
// Nothing is refused and nothing is forwarded. A merge is not a request anybody is waiting on,
// so it does not answer one, and it does not pass what it heard on: a site that learned
// something tells the others when it next speaks to them, which is the same thing that keeps
// two daemons from talking to each other forever.
//
// It returns how many entries changed, so a caller can stay quiet when it has nothing to say.
func Learn(l workspace.Layout, from string, entries []Entry) (int, error) {
	changed := 0
	err := atomic.Replace(l.ActorRoster(), func(current []byte) ([]byte, error) {
		local, err := decode(current)
		if err != nil {
			return nil, err
		}
		merged := Merge(Site(l), local, entries)
		changed = differences(local, merged)
		if changed == 0 {
			return current, nil
		}
		return yaml.Marshal(map[string][]Entry{"actors": merged})
	})
	if err != nil {
		return 0, fmt.Errorf("learning the roster from %s: %w", from, err)
	}
	return changed, nil
}

// differences counts the entries that a merge added or moved.
func differences(before, after []Entry) int {
	had := make(map[string]Entry, len(before))
	for _, e := range before {
		had[e.Name] = e
	}
	n := 0
	for _, e := range after {
		old, ok := had[e.Name]
		if !ok || old.LastSeen != e.LastSeen || old.Site != e.Site || old.Job != e.Job {
			n++
		}
	}
	return n
}

// Digest is a fingerprint of a roster, so that a site can tell whether it still has something
// to say to its peers without sending anything.
func Digest(entries []Entry) string {
	sorted := append([]Entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	sum := sha256.Sum256([]byte(fmt.Sprintf("%v", sorted)))
	return hex.EncodeToString(sum[:8])
}

// Touch records that a name is working here now, without inventing anything about it.
//
// It is what a daemon calls when it hears from a name: the roster is written by whatever
// process uses the name, and this is the same fact arriving another way.
func Touch(l workspace.Layout, name, job string, now time.Time) error {
	return Seen(l, name, job, now)
}

var _ = os.IsNotExist // the roster may legitimately not exist yet
