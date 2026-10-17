// Package exchange publishes what a site recorded and fetches what was published
// elsewhere. A name claimed twice is settled, not merged: the earlier claim keeps
// it, and the loser is told which claim was void.
package exchange

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/assets"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

// Resolution is one name claimed twice, and how it was settled.
type Resolution struct {
	Path      string `json:"path"`
	Winner    string `json:"winner"`
	Loser     string `json:"loser"`
	LoserSide string `json:"loser_side"`
}

// Link is one peer's transport for one repository: where a fetch goes and where a
// push goes. The daemon builds both; no repository stores a remote.
type Link struct {
	Name  string   `json:"name"`
	Fetch string   `json:"fetch"`
	Push  string   `json:"push"`
	Tags  []string `json:"tags,omitempty"`
}

// Links carries the three repositories' transports for the peers.
type Links struct {
	Line   []Link `json:"line"`
	Ledger []Link `json:"ledger"`
	Store  []Link `json:"store"`
}

// RunVia exchanges the line, the ledger and the store over the daemon's links. A peer
// that cannot be reached is skipped and reported; the others still exchange.
func RunVia(l layout.Layout, links Links) ([]Resolution, []string, error) {
	var settled []Resolution
	var unreachable []string
	for _, h := range []struct {
		name  string
		dir   string
		links []Link
	}{
		{"the line", l.Root, links.Line},
		{"the ledger", l.Ledger(), links.Ledger},
	} {
		got, lost, err := OneVia(h.dir, h.links)
		if err != nil {
			return settled, unique(unreachable), fmt.Errorf("%s: %w", h.name, err)
		}
		settled = append(settled, got...)
		unreachable = append(unreachable, lost...)
	}
	lost, err := StoreVia(l, links.Store)
	if err != nil {
		return settled, unique(unreachable), err
	}
	unreachable = append(unreachable, lost...)
	return settled, unique(unreachable), nil
}

// OneVia fetches each reachable link, merges it, settles any name claimed twice, and
// publishes. A link whose fetch fails is left for the next sync.
func OneVia(dir string, links []Link) ([]Resolution, []string, error) {
	if len(links) == 0 {
		return nil, nil, nil
	}
	branch, err := gitx.Run(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return nil, nil, err
	}
	branch = strings.TrimSpace(branch)

	var settled []Resolution
	var unreachable []string
	var pushErr error
	// A push can lose a race with another site. Git's answer is one rejected push
	// and one retry: fetch, merge again, and publish.
	for attempt := 0; attempt < 4; attempt++ {
		var reachable []Link
		unreachable = nil
		for _, link := range links {
			refspec := "+refs/heads/*:refs/remotes/" + link.Name + "/*"
			if err := gitx.FetchURL(dir, link.Fetch, refspec); err != nil {
				unreachable = append(unreachable, link.Name)
				continue
			}
			reachable = append(reachable, link)
		}
		for _, link := range reachable {
			ref := "refs/remotes/" + link.Name + "/" + branch
			if _, err := gitx.Run(dir, "rev-parse", "--verify", "--quiet", ref); err != nil {
				continue
			}
			identity := gitx.IdentityArgs(dir, "thrum")
			args := append(append([]string{}, identity...), "merge", "--no-edit", link.Name+"/"+branch)
			if err := gitx.OK(dir, args...); err != nil {
				got, settleErr := settle(dir)
				if settleErr != nil {
					conflicts, _ := gitx.Run(dir, "diff", "--name-only", "--diff-filter=U")
					_ = gitx.OK(dir, "merge", "--abort")
					if names := strings.TrimSpace(conflicts); names != "" {
						return settled, unreachable, fmt.Errorf("the records do not go together: %s needs a person", names)
					}
					return settled, unreachable, fmt.Errorf("the records do not go together: %v", err)
				}
				settled = append(settled, got...)
			}
		}
		pushErr = nil
		for _, link := range reachable {
			if err := gitx.PushURL(dir, link.Push, branch); err != nil {
				pushErr = err
				break
			}
		}
		if pushErr == nil {
			return settled, unreachable, nil
		}
		jitter := time.Duration(time.Now().UnixNano()%50) * time.Millisecond
		time.Sleep(time.Duration(attempt+1)*50*time.Millisecond + jitter)
	}
	return settled, unreachable, fmt.Errorf("the records could not be published after a retry: %w", pushErr)
}

// StoreVia fetches the peers' live tags and publishes this site's. A tag is never
// moved, so a rejected push is a real conflict.
func StoreVia(l layout.Layout, links []Link) ([]string, error) {
	store := l.Store()
	if store == "" {
		return nil, nil
	}
	if _, err := os.Stat(store); err != nil {
		return nil, nil
	}
	// Only tags the local line still calls live: a withdrawn version must not come
	// back from a peer that has not seen the withdrawal yet.
	live, err := assets.Live(l)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(live))
	for _, tag := range live {
		allowed[tag] = true
	}
	var unreachable []string
	var reachable []Link
	for _, link := range links {
		ok := true
		for _, tag := range link.Tags {
			if !allowed[tag] {
				continue
			}
			if err := gitx.FetchURL(store, link.Fetch, "refs/tags/"+tag+":refs/tags/"+tag); err != nil {
				unreachable = append(unreachable, link.Name)
				ok = false
				break
			}
		}
		if ok {
			reachable = append(reachable, link)
		}
	}
	published, err := assets.Published(l)
	if err != nil {
		return unique(unreachable), err
	}
	for _, tag := range published {
		if !gitx.Resolves(store, "refs/tags/"+tag) {
			continue
		}
		for _, link := range reachable {
			if err := gitx.PushURL(store, link.Push, "refs/tags/"+tag); err != nil {
				return unique(unreachable), fmt.Errorf("the store: %w", err)
			}
		}
	}
	return unique(unreachable), nil
}

func unique(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

type claim struct {
	clock int64
	site  string
	id    string
	hash  string
}

func (c claim) earlierThan(o claim) bool {
	if c.clock != o.clock {
		return c.clock < o.clock
	}
	if c.site != o.site {
		return c.site < o.site
	}
	if c.id != o.id {
		return c.id < o.id
	}
	return c.hash < o.hash
}

// settle resolves a merge that stopped on add/add conflicts. A conflict with a base
// is a genuine disagreement, and is refused.
func settle(dir string) ([]Resolution, error) {
	conflicted, err := gitx.Run(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var resolved []Resolution
	for _, path := range strings.Split(strings.TrimSpace(conflicted), "\n") {
		if path == "" {
			continue
		}
		stages, err := gitx.Run(dir, "ls-files", "-u", "--", path)
		if err != nil {
			return nil, err
		}
		if hasBase(stages) {
			return nil, fmt.Errorf("%s changed on both sides; it is not a name claimed twice", path)
		}
		ours, err := claimOf(dir, "HEAD", path)
		if err != nil {
			return nil, err
		}
		theirs, err := claimOf(dir, "MERGE_HEAD", path)
		if err != nil {
			return nil, err
		}
		winner, loser, side := ours, theirs, "--ours"
		loserSide := "theirs"
		if theirs.earlierThan(ours) {
			winner, loser, side = theirs, ours, "--theirs"
			loserSide = "ours"
		}
		if err := gitx.OK(dir, "checkout", side, "--", path); err != nil {
			return nil, err
		}
		if err := gitx.OK(dir, "add", "--", path); err != nil {
			return nil, err
		}
		resolved = append(resolved, Resolution{Path: path, Winner: winner.hash, Loser: loser.hash, LoserSide: loserSide})
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("a merge stopped without a conflict it can settle")
	}
	identity := gitx.IdentityArgs(dir, "thrum")
	args := append(identity, "commit", "--no-edit")
	if err := gitx.OK(dir, args...); err != nil {
		return nil, err
	}
	return resolved, nil
}

func claimOf(dir, rev, path string) (claim, error) {
	out, err := gitx.Run(dir, "log", "-1", "--diff-filter=A", "--format=%ct %H", rev, "--", path)
	if err != nil {
		return claim{}, err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return claim{}, fmt.Errorf("%s: cannot tell when it was claimed", path)
	}
	t, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return claim{}, err
	}
	hash := fields[1]
	if msg, err := gitx.Run(dir, "log", "-1", "--format=%B", hash); err == nil {
		if clock, site, id, ok := parseEvent(msg); ok {
			return claim{clock: clock, site: site, id: id, hash: hash}, nil
		}
	}
	return claim{clock: t * int64(time.Second), hash: hash}, nil
}

// parseEvent reads the trailer the line puts on every event.
func parseEvent(message string) (int64, string, string, bool) {
	for _, ln := range strings.Split(message, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "Thrum-Event: ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(ln, "Thrum-Event: "))
		if len(fields) < 3 {
			return 0, "", "", false
		}
		clock, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, "", "", false
		}
		return clock, fields[1], fields[2], true
	}
	return 0, "", "", false
}

func hasBase(stages string) bool {
	for _, ln := range strings.Split(stages, "\n") {
		fields := strings.Fields(ln)
		if len(fields) >= 3 && fields[2] == "1" {
			return true
		}
	}
	return false
}
