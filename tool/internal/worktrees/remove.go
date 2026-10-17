package worktrees

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// Remove retires a label. A label whose directory is materialised is refused: the
// directory is somebody's work.
func Remove(l layout.Layout, label string, now time.Time) error {
	w, ok, err := Find(l, label)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no label named %q", label)
	}
	if w.Retired != "" {
		return fmt.Errorf("label %q is already retired", label)
	}
	if _, err := os.Stat(l.Worktree(label)); err == nil {
		return fmt.Errorf("label %q is materialised at %s; release it first", label, l.Worktree(label))
	}
	w.Retired = now.UTC().Format(format.DayForm)
	archived := l.ArchivedWorktree(label)
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		return err
	}
	if err := records.Write(archived, w); err != nil {
		return err
	}
	if err := os.Remove(l.WorktreeDecl(label)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return line.Commit(l.Root, []string{l.WorktreeDecl(label), archived},
		fmt.Sprintf("retire worktree/%s: retired", label))
}
