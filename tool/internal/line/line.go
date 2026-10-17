// Package line records one coordination event: the paths an event changed, staged and
// committed as one commit, and never anything else the index happens to hold.
package line

import (
	"errors"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
)

// ErrNoChanges means the event's paths are already as they were.
var ErrNoChanges = errors.New("nothing to record")

// Commit stages paths — including deletions — and commits exactly them.
func Commit(dir string, paths []string, message string) error {
	if len(paths) == 0 {
		return errors.New("an event records at least one path")
	}
	add := append([]string{"add", "-A", "--"}, paths...)
	if err := gitx.OK(dir, add...); err != nil {
		return err
	}
	identity := gitx.IdentityArgs(dir, "thrum")
	args := append(append([]string{}, identity...), "commit", "-m", message, "--")
	args = append(args, paths...)
	if err := gitx.OK(dir, args...); err != nil {
		if gitx.IsErr(err, "nothing to commit", "no changes added", "nothing added", "did not match") {
			return ErrNoChanges
		}
		return err
	}
	return nil
}
