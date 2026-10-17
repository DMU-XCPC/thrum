// Package clean removes what a shared history does not track and does not ignore.
// It is the guarded version of git clean: files only, never ignored, never a
// directory.
package clean

import (
	"os"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

// Run cleans the line and the ledger, and returns what it removed.
func Run(l layout.Layout) ([]string, error) {
	var removed []string
	for _, dir := range []string{l.Root, l.Ledger()} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		out, err := gitx.Run(dir, "clean", "-f")
		if err != nil {
			return removed, err
		}
		for _, ln := range strings.Split(out, "\n") {
			ln = strings.TrimSpace(ln)
			if strings.HasPrefix(ln, "Removing ") {
				removed = append(removed, strings.TrimPrefix(ln, "Removing "))
			}
		}
	}
	return removed, nil
}
