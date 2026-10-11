package daemon

import (
	"encoding/json"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// rosterSyncEvery is how often a daemon looks at its own roster. A minute is short enough that
// a name is known to the peers of the site it is at, and long enough that an idle workspace
// sends nothing.
const rosterSyncEvery = time.Minute

// rosterSync keeps this site's roster current with its peers.
//
// The roster is written by whichever process uses a name, and that is usually a one-shot
// command rather than the daemon, so a change has to be noticed rather than announced. It is
// also why this is a timer: a tick with nothing to say reads one small file and sends nothing
// at all.
//
// Pulling before pushing matters. What a peer knows may be newer than what we have, and what we
// then push should carry it: that is how a name learned from a third site reaches us.
type rosterSync struct {
	l      workspace.Layout
	digest string          // the roster we last exchanged with every peer
	behind map[string]bool // peers we could not reach, to try again
}

func newRosterSync(l workspace.Layout) *rosterSync {
	return &rosterSync{l: l, behind: map[string]bool{}}
}

// once is one attempt at converging, and it does nothing when there is nothing new to say and
// nobody left to say it to.
func (r *rosterSync) once() {
	local, err := roster.Local(r.l)
	if err != nil {
		return
	}
	if roster.Digest(local) == r.digest && len(r.behind) == 0 {
		return
	}

	peers := NewPeers(r.l)
	refs, err := peers.Refs()
	if err != nil {
		return
	}
	failed := map[string]bool{}
	for _, ref := range r.peers(peers, refs) {
		if err := r.pull(peers, ref); err != nil {
			failed[ref.Name] = true
		}
	}

	// Re-read: pulling may have changed what we have to say.
	local, err = roster.Local(r.l)
	if err != nil {
		return
	}
	for _, ref := range r.peers(peers, refs) {
		if err := r.push(peers, ref, local); err != nil {
			failed[ref.Name] = true
		}
	}

	r.behind = failed
	if len(failed) == 0 {
		r.digest = roster.Digest(local)
	}
}

// peers are the ones worth talking to: not this site, not one the roster no longer declares,
// and not one with no address entered here.
func (r *rosterSync) peers(p *Peers, refs []PeerRef) []PeerRef {
	out := make([]PeerRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Name == p.Site() || ref.Reach == "" || ref.Stale {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// pull takes what a peer has seen and merges it here.
func (r *rosterSync) pull(peers *Peers, ref PeerRef) error {
	resp, err := peers.Ask(ref, Request{Verb: VerbRoster})
	if err != nil {
		return err
	}
	if !resp.OK {
		return errPeerRefused{ref.Name, resp.Error}
	}
	var body struct {
		Site   string         `json:"site"`
		Actors []roster.Entry `json:"actors"`
	}
	if err := json.Unmarshal(resp.Data, &body); err != nil {
		return err
	}
	_, err = roster.Learn(r.l, ref.Name, body.Actors)
	return err
}

// push tells a peer what this site has seen, learned names included.
func (r *rosterSync) push(peers *Peers, ref PeerRef, local []roster.Entry) error {
	if len(local) == 0 {
		return nil
	}
	resp, err := peers.Ask(ref, Request{
		Verb: VerbRosterLearn,
		Args: Args(map[string]any{"site": roster.Site(r.l), "actors": local}),
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return errPeerRefused{ref.Name, resp.Error}
	}
	return nil
}

// errPeerRefused is a peer answering with a refusal rather than failing to answer, which for
// this purpose is the same thing: it has not learned what we told it.
type errPeerRefused struct {
	site string
	why  string
}

func (e errPeerRefused) Error() string {
	if e.why == "" {
		return e.site + " refused the roster"
	}
	return e.site + " refused the roster: " + e.why
}
