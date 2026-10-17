// Package exchange publishes what a site recorded and fetches what was published
// elsewhere. A name claimed twice is settled, not merged: the earlier claim keeps
// it, and the loser is told which claim was void.
package exchange

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
		if strings.TrimSpace(remotes) == "" {
			continue
		}
		if err := gitx.OK(dir, "fetch", "--all", "--prune"); err != nil {
			return settled, fmt.Errorf("the repository %s: %w", d.Repo, err)
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
	if err := gitx.OK(dir, "fetch", "--all", "--prune"); err != nil {
		return nil, err
	}
	branch, err := gitx.Run(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return nil, err
	}
	branch = strings.TrimSpace(branch)

	var settled []Resolution
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
				_ = gitx.OK(dir, "merge", "--abort")
				return settled, fmt.Errorf("the records do not go together: %v", err)
			}
			settled = append(settled, got...)
		}
	}
	for _, remote := range remotes {
		if err := gitx.OK(dir, "push", remote, branch); err != nil {
			return settled, err
		}
	}
	return settled, nil
}

type claim struct {
	time int64
	hash string
}

func (c claim) earlierThan(o claim) bool {
	if c.time != o.time {
		return c.time < o.time
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
	out, err := gitx.Run(dir, "log", "-1", "--format=%ct %H", rev, "--", path)
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
	return claim{time: t, hash: fields[1]}, nil
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
