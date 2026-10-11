package main

import (
	"fmt"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// target says where a resource lives: here, or at the one peer that says it holds it.
//
// A resource is leased where it lives and never where it is used, so this is what
// decides whether a lease is written here or asked for there — and it is why exclusion
// stays on the machine the thing is physically on, with nothing central involved.
//
// Which site holds what is asked rather than declared: a declaration travels by git and
// so is present everywhere, while what a machine actually has is the machine's own
// business.
type target struct {
	local  bool
	remote string
	ref    daemon.PeerRef
}

func leaseTarget(l workspace.Layout, resource string) (target, error) {
	holders, err := daemon.Held(l, resource)
	if err != nil {
		return target{}, err
	}

	var t target
	// Sites that were asked and did not answer. The design is explicit that this is not the
	// same as their not holding it, and a lease that cannot be established is refused rather
	// than assumed, so they are kept to say so in the refusal.
	var unanswered []string
	for _, h := range holders {
		if h.Unknown {
			unanswered = append(unanswered, h.Site)
			continue
		}
		if h.Local {
			t.local = true
			continue
		}
		if t.remote != "" {
			return target{}, fmt.Errorf("%s says it lives at %s and at %s: a resource has one home, and two sites claiming one is not something this workspace settles",
				resource, t.remote, h.Site)
		}
		t.remote = h.Site
	}
	if t.remote != "" {
		ref, err := daemon.NewPeers(l).Lookup(t.remote)
		if err != nil {
			return target{}, err
		}
		t.ref = ref
		return t, nil
	}
	if !t.local {
		if len(unanswered) > 0 {
			return target{}, fmt.Errorf("no site that answered holds %s, and %s did not answer: where it lives cannot be established, and this is not the same as nobody holding it",
				resource, strings.Join(unanswered, ", "))
		}
		return target{}, fmt.Errorf("no site says it holds %s: a resource is leased where it lives, and where that is has to be said by the machine that has it", resource)
	}
	return t, nil
}
