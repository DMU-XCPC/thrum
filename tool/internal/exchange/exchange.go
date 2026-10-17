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
	"github.com/multiagentworkspace/thrum/tool/internal/repos"
)

// Resolution is one name claimed twice, and how it was settled.
type Resolution struct {
	Path      string `json:"path"`
	Winner    string `json:"winner"`
	Loser     string `json:"loser"`
	LoserSide string `json:"loser_side"`
}

// Run exchanges both shared histories, then fetches the code repositories. The code
// is fetched, never pushed: publishing a branch is the project's business.
func Run(l layout.Layout) ([]Resolution, error) {
	var settled []Resolution
	for _, h := range []struct{ name, dir string }{
		{"the line", l.Root},
		{"the ledger", l.Ledger()},
	} {
		got, err := One(h.dir)
		if err != nil {
			return settled, fmt.Errorf("%s: %w", h.name, err)
		}
		settled = append(settled, got...)
	}
	decls, err := repos.List(l)
	if err != nil {
		return settled, err
	}
	for _, d := range decls {
		dir := l.CodeRepo(d.Repo)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		remotes, err := gitx.Run(dir, "remote")
		if err != nil {
			return settled, err
		}
		names := strings.Fields(remotes)
		if len(names) == 0 {
			continue
		}
		// Fetch every branch explicitly: a bare clone does not always carry a fetch
		// refspec, and a handoff may name a commit on a branch that is not HEAD.
		for _, remote := range names {
			refspec := "+refs/heads/*:refs/remotes/" + remote + "/*"
			// A declared repository this peer has not materialised has nothing to
			// give; that is not a failure of the histories' exchange.
			_ = gitx.OK(dir, "fetch", "--prune", remote, refspec)
		}
	}
	// The store is a repository of tags: fetch what peers published, and publish
	// what this site did. A tag is never moved, so a rejected push is a real conflict.
	if store := l.Store(); store != "" {
		if _, err := os.Stat(store); err == nil {
			remotes, err := gitx.Run(store, "remote")
			if err != nil {
				return settled, err
			}
			names := strings.Fields(remotes)
			for _, remote := range names {
				if err := gitx.OK(store, "fetch", remote, "+refs/tags/*:refs/tags/*"); err != nil {
					return settled, fmt.Errorf("the store: %w", err)
				}
			}
			published, err := assets.Published(l)
			if err != nil {
				return settled, err
			}
			for _, tag := range published {
				if _, err := gitx.Run(store, "rev-parse", "--verify", "--quiet", "refs/tags/"+tag); err != nil {
					continue
				}
				for _, remote := range names {
					if err := gitx.OK(store, "push", remote, "refs/tags/"+tag); err != nil {
						return settled, fmt.Errorf("the store: %w", err)
					}
				}
			}
		}
	}
	return settled, nil
}

// One fetches a history's remotes, merges each into the local branch, settles any
// name claimed twice, and publishes.
func One(dir string) ([]Resolution, error) {
	out, err := gitx.Run(dir, "remote")
	if err != nil {
		return nil, err
	}
	remotes := strings.Fields(out)
	if len(remotes) == 0 {
		return nil, nil
	}
	branch, err := gitx.Run(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return nil, err
	}
	branch = strings.TrimSpace(branch)

	var settled []Resolution
	var pushErr error
	// A push can lose a race with another site. Git's answer is one rejected push
	// and one retry: fetch, merge again, and publish.
	for attempt := 0; attempt < 2; attempt++ {
		if err := gitx.OK(dir, "fetch", "--all", "--prune"); err != nil {
			return settled, err
		}
		for _, remote := range remotes {
			ref := "refs/remotes/" + remote + "/" + branch
			if _, err := gitx.Run(dir, "rev-parse", "--verify", "--quiet", ref); err != nil {
				continue
			}
			identity := gitx.IdentityArgs(dir, "thrum")
			args := append(append([]string{}, identity...), "merge", "--no-edit", remote+"/"+branch)
			if err := gitx.OK(dir, args...); err != nil {
				got, settleErr := settle(dir)
				if settleErr != nil {
					conflicts, _ := gitx.Run(dir, "diff", "--name-only", "--diff-filter=U")
					_ = gitx.OK(dir, "merge", "--abort")
					if names := strings.TrimSpace(conflicts); names != "" {
						return settled, fmt.Errorf("the records do not go together: %s needs a person", names)
					}
					return settled, fmt.Errorf("the records do not go together: %v", err)
				}
				settled = append(settled, got...)
			}
		}
		pushErr = nil
		for _, remote := range remotes {
			if err := gitx.OK(dir, "push", remote, branch); err != nil {
				pushErr = err
				break
			}
		}
		if pushErr == nil {
			return settled, nil
		}
	}
	return settled, fmt.Errorf("the records could not be published after a retry: %w", pushErr)
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
