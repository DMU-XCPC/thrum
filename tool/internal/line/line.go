// Package line records one coordination event: the paths an event changed, staged and
// committed as one commit, and never anything else the index happens to hold.
package line

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
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
	message = withEvent(dir, message)
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

// withEvent adds the event's place in the derived order: a monotonic clock, the
// site, and an id. The settlement reads them, so every site computes the same
// order from the same events instead of comparing wall-clock commit times.
func withEvent(dir, message string) string {
	root, site := workspaceOf(dir)
	if site == "" {
		return message
	}
	clock, err := nextClock(root)
	if err != nil {
		return message
	}
	id, err := eventID()
	if err != nil {
		return message
	}
	return message + "\n\nThrum-Event: " + strconv.FormatInt(clock, 10) + " " + site + " " + id
}

func workspaceOf(dir string) (string, string) {
	for _, d := range []string{dir, filepath.Dir(dir)} {
		b, err := os.ReadFile(filepath.Join(d, "var", "site"))
		if err != nil {
			continue
		}
		if site := strings.TrimSpace(string(b)); site != "" {
			return d, site
		}
	}
	return "", ""
}

// nextClock is a hybrid logical clock: it never goes backwards, even across a
// restart, and it is persisted with the site's other runtime state.
func nextClock(root string) (int64, error) {
	state := filepath.Join(root, "var", "state", "clock")
	var last int64
	if b, err := os.ReadFile(state); err == nil {
		last, _ = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	}
	now := time.Now().UnixNano()
	if now <= last {
		now = last + 1
	}
	if err := os.MkdirAll(filepath.Dir(state), 0o755); err != nil {
		return 0, err
	}
	if err := format.WriteFileAtomic(state, []byte(strconv.FormatInt(now, 10)+"\n")); err != nil {
		return 0, err
	}
	return now, nil
}

func eventID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
