package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
)

// repositories gives a site its own copies of the code and asset repositories.
//
// They are bare on every site, so that no checkout duplicates them, and each site holds its
// own: the code repository is reached the way the project reaches it, and the asset
// repository carries the versions a site has fetched.
//
// A URL means the project keeps that repository somewhere and this site takes a copy of it.
// No URL means this site starts one, which is what a project that has not pushed anything
// anywhere yet does — and it is why a workspace with no code in it is a normal state rather
// than a broken one: it is a project that has not made its first commit.
func repositories(l Layout, code, store string) error {
	if err := os.MkdirAll(filepath.Dir(l.CodeRepo()), 0o755); err != nil {
		return err
	}
	for _, repo := range []struct{ path, remote string }{
		{l.CodeRepo(), code},
		{l.Store(), store},
	} {
		if repo.remote == "" {
			args := []string{"init", "--bare", "--quiet", repo.path}
			if repo.path == l.CodeRepo() {
				// The code repository's default branch is the one a job branches from.
				args = []string{"init", "--bare", "-b", "main", "--quiet", repo.path}
			}
			if _, err := gitx.Run(l.Root, args...); err != nil {
				return fmt.Errorf("initialising %s: %w", repo.path, err)
			}
			continue
		}
		if _, err := gitx.Run(l.Root, "clone", "--bare", "--quiet", repo.remote, repo.path); err != nil {
			return fmt.Errorf("cloning %s into %s: %w", repo.remote, repo.path, err)
		}
	}
	return nil
}
