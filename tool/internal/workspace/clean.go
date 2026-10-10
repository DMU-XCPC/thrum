package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/multiagentworkspace/ws/tool/internal/gitx"
)

// Clean removes the untracked files the coordination repository neither tracks nor
// ignores.
//
// Two guards, and both matter. It runs git clean without -x, so every ignored plane —
// .bare/, store/, var/, job/ and worktree/ — is not even a candidate; a bare
// `git clean -xdf` at the workspace root would delete the code repository and, with it,
// the entire source history. And it runs without -d, so no directory is ever removed:
// the workspace's own empty directories are not tracked, and a clean that removed them
// would quietly demolish the shape of the place.
func Clean(l Layout, dryRun bool) ([]string, error) {
	if _, err := os.Stat(filepath.Join(l.Root, ".git")); err != nil {
		return nil, fmt.Errorf("%s is not a workspace root", l.Root)
	}

	out, err := gitx.Run(l.Root, "clean", "-n")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "Would remove ")
		if line != "" {
			paths = append(paths, line)
		}
	}
	if dryRun || len(paths) == 0 {
		return paths, nil
	}
	if _, err := gitx.Run(l.Root, "clean", "-f"); err != nil {
		return paths, err
	}
	return paths, nil
}
