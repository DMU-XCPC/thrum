package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/exchange"
	"github.com/multiagentworkspace/thrum/tool/internal/join"
)

func newJoinCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "join <token>",
		Short: "Become another site of an existing workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := join.Decode(args[0])
			if err != nil {
				return err
			}
			root := dir
			if root == "" {
				root = "."
			}
			if err := join.Join(root, token); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "joined %s as %s\n", root, token.Site)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "where the workspace goes (default the current directory)")
	return cmd
}

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Fetch the peers, merge both shared histories, and publish",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := exchange.Run(workspaceRoot()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "exchanged")
			return nil
		},
	}
}
