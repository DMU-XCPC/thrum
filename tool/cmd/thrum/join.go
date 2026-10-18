package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/discovery"
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
			if token.Reach == "" {
				reach, err := discovery.Query(token.Workspace, token.Peer, 3*time.Second)
				if err != nil {
					return fmt.Errorf("the inviter did not answer on the LAN: %w", err)
				}
				token.Reach = reach
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
		Short: "Ask this daemon to exchange the line, the ledger and the store with the peers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.LocalDo(workspaceRoot(), daemon.Request{Verb: "history.exchange"})
			if err != nil {
				var refusal *daemon.Refusal
				if errors.As(err, &refusal) {
					return err
				}
				return fmt.Errorf("the daemon is not running here (%v); start it with `thrum daemon run`", err)
			}
			var out struct {
				Settled     []exchange.Resolution `json:"settled"`
				Unreachable []string              `json:"unreachable"`
			}
			if err := json.Unmarshal(resp.Data, &out); err != nil {
				return err
			}
			for _, r := range out.Settled {
				fmt.Fprintf(cmd.OutOrStdout(),
					"settled %s: the earlier claim (%s) keeps it; the other (%s, %s) is void\n",
					r.Path, r.Winner, r.Loser, r.LoserSide)
			}
			for _, name := range out.Unreachable {
				fmt.Fprintf(cmd.OutOrStdout(), "unreachable: %s (left for the next sync)\n", name)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "exchanged")
			return nil
		},
	}
}
