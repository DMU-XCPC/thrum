package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "daemon", Short: "This site's endpoint: run it, ask it, stop it"}

	var listen string
	var routeEvery time.Duration
	var discover bool
	var discoverEvery time.Duration
	run := &cobra.Command{
		Use:   "run",
		Short: "Run this site's daemon in the foreground",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemon.Run(workspaceRoot(), listen, routeEvery, discover, discoverEvery)
		},
	}
	run.Flags().StringVar(&listen, "listen", "", "listen here instead of the site's declared reach")
	run.Flags().DurationVar(&routeEvery, "route-interval", 5*time.Second, "how often to learn routes from the reachable peers")
	run.Flags().BoolVar(&discover, "discover", false, "announce this site on the LAN and learn peers' addresses; off unless asked for")
	run.Flags().DurationVar(&discoverEvery, "discover-interval", 5*time.Second, "how often to announce on the LAN")

	status := &cobra.Command{
		Use:   "status",
		Short: "Whether a daemon answers here, and what it says",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.Status(workspaceRoot())
			if err != nil {
				return fmt.Errorf("no daemon is running here: %w", err)
			}
			if jsonFlag {
				fmt.Fprintln(cmd.OutOrStdout(), string(resp.Data))
				return nil
			}
			var data map[string]any
			if err := json.Unmarshal(resp.Data, &data); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "site      %v\nworkspace %v\nversion   %v\nheld      %v\n",
				data["site"], data["workspace"], data["version"], data["held"])
			return nil
		},
	}

	stop := &cobra.Command{
		Use:   "stop",
		Short: "Stop this site's daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Stop(workspaceRoot()); err != nil {
				return fmt.Errorf("no daemon is running here: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "stopped")
			return nil
		},
	}

	cmd.AddCommand(run, status, stop)
	return cmd
}

func newEventsCmd() *cobra.Command {
	var watch bool
	cmd := &cobra.Command{
		Use:   "events",
		Short: "A stream of what arrived, for a plugin to watch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			seen := map[string]bool{}
			for {
				msgs, err := messages.List(l, actorName())
				if err != nil {
					return err
				}
				for _, m := range msgs {
					if seen[m.ID] {
						continue
					}
					seen[m.ID] = true
					if err := printJSONLine(cmd, map[string]any{
						"kind": "message.delivered", "message": m,
					}); err != nil {
						return err
					}
				}
				if !watch {
					return nil
				}
				time.Sleep(500 * time.Millisecond)
			}
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "keep reporting what arrives")
	return cmd
}
