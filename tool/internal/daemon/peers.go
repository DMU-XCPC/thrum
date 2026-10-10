package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multiagentworkspace/ws/tool/internal/format"
	"github.com/multiagentworkspace/ws/tool/internal/workspace"
)

// PeerRef is one site in the roster: the half that travels, and the half that does not.
type PeerRef struct {
	// Name is what the workspace calls the site, spelled exactly as the resource name
	// so that the roster and the declarations match by plain string comparison.
	Name    string `json:"name"`
	Purpose string `json:"purpose,omitempty"`

	// The site-local half: how this site reaches that one. It does not travel,
	// because a collective's internal addresses are not a collective's business to
	// publish, and each site enters its own.
	Reach   string `json:"reach,omitempty"`
	SSHUser string `json:"ssh_user,omitempty"`
	Key     string `json:"key,omitempty"`

	// Stale marks a local half whose site the roster no longer declares.
	Stale bool `json:"stale,omitempty"`
}

// PeerReport is a peer and whether it answered just now.
type PeerReport struct {
	Name      string `json:"name"`
	Reach     string `json:"reach,omitempty"`
	Reachable bool   `json:"reachable"`
	Stale     bool   `json:"stale,omitempty"`
	Note      string `json:"note,omitempty"`
}

// Peers is the roster and the site-local halves put together.
//
// A site that uses nothing remote needs no reach for anybody. The roster is still read
// for names, because the rule about a site's own name is keyed on it.
type Peers struct {
	l workspace.Layout
}

func NewPeers(l workspace.Layout) *Peers { return &Peers{l: l} }

// Site is this site's own name, which a site that cannot say cannot find its own half
// of anything.
func (p *Peers) Site() string { return Site(p.l) }

// localHalf is the site-local half of one roster entry. Its keys are this site's own
// business, so they are read loosely: a field here the tooling does not read is a field
// for whoever maintains the machine.
type localHalf struct {
	Reach   string `yaml:"reach"`
	SSHUser string `yaml:"ssh_user"`
	Key     string `yaml:"key"`
	// Holds is how a resource with nothing on disk says where it is. Absent means
	// "this site has it", because a machine does not write a file about a thing it
	// has not got.
	Holds *bool `yaml:"holds"`
}

func (p *Peers) locals() (map[string]localHalf, error) {
	out := map[string]localHalf{}
	paths, err := filepath.Glob(filepath.Join(p.l.Config(), "*.yaml"))
	if err != nil {
		return out, nil
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var half localHalf
		if err := format.DecodeLoose(raw, &half); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out[name] = half
	}
	return out, nil
}

// Refs returns every roster entry with its local half, and every local half whose site
// the roster no longer declares.
func (p *Peers) Refs() ([]PeerRef, error) {
	declared, err := workspace.ReadResources(p.l, "site")
	if err != nil {
		return nil, err
	}
	local, err := p.locals()
	if err != nil {
		return nil, err
	}

	var refs []PeerRef
	for _, name := range sortedResources(declared) {
		ref := PeerRef{Name: name, Purpose: declared[name].Purpose}
		if half, ok := local[name]; ok {
			ref.Reach, ref.SSHUser, ref.Key = half.Reach, half.SSHUser, half.Key
		}
		refs = append(refs, ref)
	}
	// Removing a site from the roster ends the duty, so a local half left behind says
	// so rather than going on being used.
	//
	// Not every file under var/config/ is a site's: the directory holds the local half
	// of any resource, and most resources are not sites. A half that carries what only
	// a machine needs — a user to connect as, a key to connect with — is a site's, and
	// one that says instead what this site has is a resource's.
	for _, name := range sortedHalves(local) {
		if _, ok := declared[name]; ok {
			continue
		}
		half := local[name]
		if half.SSHUser == "" && half.Key == "" {
			continue
		}
		refs = append(refs, PeerRef{Name: name, Reach: half.Reach, Stale: true})
	}
	return refs, nil
}

// Lookup finds one peer by name.
func (p *Peers) Lookup(name string) (PeerRef, error) {
	refs, err := p.Refs()
	if err != nil {
		return PeerRef{}, err
	}
	for _, ref := range refs {
		if ref.Name == name {
			return ref, nil
		}
	}
	return PeerRef{}, fmt.Errorf("%q is not in the roster", name)
}

// Report says which peers are reachable, right now. It is a question about the present
// and is never cached.
func (p *Peers) Report() []PeerReport {
	refs, err := p.Refs()
	if err != nil {
		return []PeerReport{{Name: "roster", Note: err.Error()}}
	}

	out := make([]PeerReport, len(refs))
	done := make(chan int, len(refs))
	for i, ref := range refs {
		go func(i int, ref PeerRef) {
			out[i] = PeerReport{Name: ref.Name, Reach: ref.Reach, Stale: ref.Stale}
			defer func() { done <- i }()

			switch {
			case ref.Stale:
				out[i].Note = "the roster no longer declares it"
			case ref.Name == p.Site():
				out[i].Reachable = true
				out[i].Note = "this site"
			case ref.Reach == "":
				out[i].Note = "no reach is entered for it here"
			default:
				resp, err := p.Ask(ref, Request{Verb: VerbStatus})
				switch {
				case err != nil:
					out[i].Note = err.Error()
				case !resp.OK:
					out[i].Note = resp.Error
				default:
					out[i].Reachable = true
				}
			}
		}(i, ref)
	}
	for range refs {
		<-done
	}
	return out
}

// Ask sends one request to one peer and reads its answer.
func (p *Peers) Ask(ref PeerRef, req Request) (Response, error) {
	if ref.Reach == "" {
		return Response{}, fmt.Errorf("%s has no reach entered for it here", ref.Name)
	}
	client, err := DialSSH(ref, p.l, p.Site())
	if err != nil {
		return Response{}, err
	}
	defer client.Close()
	return client.Do(req)
}

// Broadcast sends one request to every reachable peer and reports who heard it.
//
// This is how "job A is done" gets around without a hub: the site where it happened
// tells everyone it can reach, and each of them wakes its own waiters.
func (p *Peers) Broadcast(req Request) ([]string, error) {
	refs, err := p.Refs()
	if err != nil {
		return nil, err
	}
	var told []string
	var lastErr error
	for _, ref := range refs {
		if ref.Reach == "" || ref.Stale || ref.Name == p.Site() {
			continue
		}
		if _, err := p.Ask(ref, req); err != nil {
			lastErr = err
			continue
		}
		told = append(told, ref.Name)
	}
	sort.Strings(told)
	return told, lastErr
}

// Holds reports whether this site holds a resource.
//
// Both facts that settle it are site-local, which is the point: a declaration travels
// by git and so is present everywhere, while what a machine actually has is the
// machine's own business. A site holds a resource when it is materialised here, or when
// this site's own half says so — which is how a borrowed machine or a shared dependency
// says where it is.
func (p *Peers) Holds(resource string) bool {
	if _, err := os.Stat(localHalfPath(p.l, resource)); err == nil {
		if halves, err := p.locals(); err == nil {
			if half, ok := halves[resource]; ok && half.Holds != nil {
				return *half.Holds
			}
		}
		return true
	}
	return materialisedHere(p.l, resource)
}

// materialisedHere reports whether a resource has anything on disk at this site.
func materialisedHere(l workspace.Layout, resource string) bool {
	if _, err := os.Stat(l.Worktree(resource)); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(l.State(), resource)); err == nil {
		return true
	}
	return false
}

func localHalfPath(l workspace.Layout, name string) string {
	return filepath.Join(l.Config(), name+".yaml")
}

// Home reports which sites hold a resource, this one included.
//
// A resource is leased where it lives and nowhere else, which keeps exclusion on the
// machine the thing is physically on. That a resource has exactly one home is assumed
// rather than enforced, so two sites that both claim one are reported instead of being
// picked between.
func (p *Peers) Home(resource string) ([]string, error) {
	var holders []string
	if p.Holds(resource) {
		holders = append(holders, p.Site())
	}

	refs, err := p.Refs()
	if err != nil {
		return holders, err
	}
	type answer struct {
		name  string
		holds bool
	}
	answers := make(chan answer, len(refs))
	asked := 0
	for _, ref := range refs {
		if ref.Reach == "" || ref.Stale || ref.Name == p.Site() {
			continue
		}
		asked++
		go func(ref PeerRef) {
			resp, err := p.Ask(ref, Request{
				Verb: VerbHolds,
				Args: Args(map[string]string{"resource": resource}),
			})
			if err != nil || !resp.OK {
				answers <- answer{name: ref.Name}
				return
			}
			var body struct {
				Holds bool `json:"holds"`
			}
			if err := json.Unmarshal(resp.Data, &body); err != nil {
				answers <- answer{name: ref.Name}
				return
			}
			answers <- answer{name: ref.Name, holds: body.Holds}
		}(ref)
	}
	for i := 0; i < asked; i++ {
		a := <-answers
		if a.holds {
			holders = append(holders, a.name)
		}
	}
	sort.Strings(holders)
	return holders, nil
}

func sortedResources(m map[string]format.Resource) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedHalves(m map[string]localHalf) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
