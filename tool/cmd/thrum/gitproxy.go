package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/join"
)

// newGitProxyCmd is the local end of a git ext:: remote: it carries one service
// over the daemon's authenticated channel. It is hidden because it is a transport,
// not an operation a person runs.
func newGitProxyCmd() *cobra.Command {
	var peer, repo, service, joinToken string
	cmd := &cobra.Command{
		Use:    "git-proxy",
		Hidden: true,
		Short:  "Carry one git service over the daemon's channel",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if joinToken != "" {
				if repo == "" || service == "" {
					return fmt.Errorf("--repo and --service are required")
				}
				token, err := join.Decode(joinToken)
				if err != nil {
					return err
				}
				return daemon.JoinProxy(token, repo, service)
			}
			if peer == "" || repo == "" || service == "" {
				return fmt.Errorf("--peer, --repo and --service are required")
			}
			// git sets GIT_DIR for a clone's transport child; it must not leak into
			// this process's own repository reads.
			for _, k := range []string{
				"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
				"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR",
			} {
				_ = os.Unsetenv(k)
			}
			return daemon.Proxy(workspaceRoot(), peer, repo, service)
		},
	}
	cmd.Flags().StringVar(&peer, "peer", "", "the site whose daemon serves the repository")
	cmd.Flags().StringVar(&repo, "repo", "", "line, ledger, store, or a code repository")
	cmd.Flags().StringVar(&service, "service", "", "upload-pack or receive-pack")
	cmd.Flags().StringVar(&joinToken, "join-token", "", "the invitation whose daemon to use")
	return cmd
}
