// Package clean removes what a shared history does not track and does not ignore.
// It is the guarded version of git clean: files only, never ignored, never a
// directory.
package clean

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/payload"
)

// Run cleans the line and the ledger, and returns what it removed. With payloads it
// also sweeps the job payloads whose retention window has passed.
func Run(l layout.Layout, payloads bool) ([]string, error) {
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
	if payloads {
		collected, err := payload.Sweep(l)
		if err != nil {
			return removed, err
		}
		for _, job := range collected {
			removed = append(removed, filepath.Join("job", job))
		}
	}
	return removed, nil
}
