package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// jobHome is the one site whose answer about a job counts.
//
// A job has a home the way a resource does, but it is not declared: it is the site where
// the job is materialised — the site whose working directory has `job/<id>` checked out.
// That is the only site whose tooling can answer for the job, and asking it is what makes
// a claim consistent across sites without anything being agreed on, which is the same
// reason a lease is taken at the resource's home site.
type jobHome struct {
	local  bool
	remote string
	ref    daemon.PeerRef
	claim  format.Occupy
}

// resolveJob finds the site that holds a job, and what its claim says.
//
// Every refusal here is a refusal to guess. A job no site holds cannot be claimed,
// because there is nothing to claim. Two sites holding one job is refused rather than
// picked between, which is the same answer the workspace gives about two homes for a
// resource. And a peer that does not answer means *cannot establish*, never *not held*:
// treating silence as absence is how two actors end up on one job.
func resolveJob(l workspace.Layout, job string) (jobHome, error) {
	peers := daemon.NewPeers(l)

	var holders []string
	var silent []string
	home := jobHome{}
	claims := map[string]format.Occupy{}

	if daemon.HoldsJob(l, job) {
		holders = append(holders, peers.Site())
		home.local = true
		if note, ok, err := occupancy.Get(l, job); err != nil {
			return home, err
		} else if ok {
			claims[peers.Site()] = note
			home.claim = note
		}
	}

	for _, report := range peers.Report() {
		if report.Name == peers.Site() {
			continue
		}
		if !report.Reachable {
			silent = append(silent, report.Name)
			continue
		}
		ref, err := peers.Lookup(report.Name)
		if err != nil {
			continue
		}
		resp, err := peers.Ask(ref, daemon.Request{
			Verb: daemon.VerbJob,
			Args: daemon.Args(map[string]string{"job": job}),
		})
		if err != nil || !resp.OK {
			silent = append(silent, report.Name)
			continue
		}
		var body struct {
			Site     string `json:"site"`
			Holds    bool   `json:"holds"`
			Occupant string `json:"occupant"`
			Since    string `json:"since"`
		}
		if err := decode(resp, &body); err != nil {
			silent = append(silent, report.Name)
			continue
		}
		if !body.Holds {
			continue
		}
		holders = append(holders, report.Name)
		note := format.Occupy{Occupant: body.Occupant, Since: body.Since}
		claims[report.Name] = note
		if report.Name != peers.Site() {
			home.remote = report.Name
			home.ref = ref
			home.claim = note
		}
	}

	sort.Strings(holders)
	sort.Strings(silent)
	switch {
	case len(holders) == 0 && len(silent) > 0:
		return home, homeUnknown{job: job, silent: silent}
	case len(holders) == 0:
		return home, noHome{job: job}
	case len(holders) > 1:
		return home, fmt.Errorf("%s say they hold %s: a job has one home, and two sites materialising one is not something this workspace settles",
			strings.Join(holders, " and "), job)
	}
	return home, nil
}

// noHome is a job no site holds: a fact about the world rather than a question that failed.
type noHome struct{ job string }

func (e noHome) Error() string {
	return fmt.Sprintf("no site holds %s: a job is materialised at a site before anybody is on it, and this one is materialised nowhere", e.job)
}

// homeUnknown is a job whose home could not be established, because a site did not answer. It
// is not the same as nobody holding it, and a caller must be able to tell the two apart without
// reading the sentence — which is what these types are for.
type homeUnknown struct {
	job    string
	silent []string
}

func (e homeUnknown) Error() string {
	return fmt.Sprintf("no site that answered holds %s, and %s did not answer: where it lives cannot be established, and this is not the same as nobody holding it",
		e.job, strings.Join(e.silent, ", "))
}

// claimAt writes or clears a job's claim at the site that holds it, which is the only
// place it can be written or read authoritatively.
func claimAt(l workspace.Layout, home jobHome, req daemon.Request) (daemon.Response, error) {
	if home.remote == "" {
		return daemon.Response{OK: true}, nil
	}
	return daemon.NewPeers(l).Ask(home.ref, req)
}

// whoIsOn answers the question rather than refusing it.
//
// Asking who is on a job is a query, so every one of these is an answer: nobody is on it,
// or nobody can say, or two sites disagree about where it is. Refusing to answer would
// make a view into a gate, and the gate is `occupy`, which resolves the same thing and
// does refuse.
func whoIsOn(l workspace.Layout, job string) (string, error) {
	home, err := resolveJob(l, job)
	if err != nil {
		var nowhere noHome
		var unknown homeUnknown
		switch {
		case errors.As(err, &nowhere):
			return fmt.Sprintf("nobody is on %s, and no site holds it: a job is materialised at a site before anybody is on it", job), nil
		case errors.As(err, &unknown):
			return fmt.Sprintf("cannot say who is on %s: %s did not answer, so where it lives cannot be established", job, strings.Join(unknown.silent, ", ")), nil
		default:
			return fmt.Sprintf("cannot say who is on %s: %v", job, err), nil
		}
	}
	where := daemon.NewPeers(l).Site()
	if home.remote != "" {
		where = home.remote
	}
	if home.claim.Occupant == "" {
		return fmt.Sprintf("%s holds %s, and nobody says they are on it", where, job), nil
	}
	return fmt.Sprintf("%-20s %-14s since %s", where, home.claim.Occupant, home.claim.Since), nil
}
