// Package exchange publishes what a site recorded and fetches what was published
// elsewhere: the one step every recording site runs, and sync runs on demand.
package exchange

import (
	"fmt"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

// Run exchanges both shared histories.
func Run(l layout.Layout) error {
	for _, h := range []struct{ name, dir string }{
		{"the line", l.Root},
		{"the ledger", l.Ledger()},
	} {
		if err := One(h.dir); err != nil {
			return fmt.Errorf("%s: %w", h.name, err)
		}
	}
	return nil
}

// One fetches a history's remotes, merges each into the local branch, and publishes.
//
// A conflict is refused rather than merged: the records in question are somebody's
// work, and the loser records again.
func One(dir string) error {
	out, err := gitx.Run(dir, "remote")
	if err != nil {
		return err
	}
	remotes := strings.Fields(out)
	if len(remotes) == 0 {
		return nil
	}
	if err := gitx.OK(dir, "fetch", "--all", "--prune"); err != nil {
		return err
	}
	branch, err := gitx.Run(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return err
	}
	branch = strings.TrimSpace(branch)

	for _, remote := range remotes {
		ref := "refs/remotes/" + remote + "/" + branch
		if _, err := gitx.Run(dir, "rev-parse", "--verify", "--quiet", ref); err != nil {
			continue
		}
		identity := gitx.IdentityArgs(dir, "thrum")
		args := append(append([]string{}, identity...), "merge", "--no-edit", remote+"/"+branch)
		if err := gitx.OK(dir, args...); err != nil {
			_ = gitx.OK(dir, "merge", "--abort")
			return fmt.Errorf("the records do not go together: %v", err)
		}
	}
	for _, remote := range remotes {
		if err := gitx.OK(dir, "push", remote, branch); err != nil {
			return err
		}
	}
	return nil
}
