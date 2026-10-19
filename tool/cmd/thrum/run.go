package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
)

// newRunCmd runs the actor's own command on another site. The default directory is
// that job's source snapshot or payload; there is no sandbox, the site is trusted.
func newRunCmd() *cobra.Command {
	var site, job string
	cmd := &cobra.Command{
		Use:   "run --site <helper> [--job <id>] -- <command>",
		Short: "Run a command on another site",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if site == "" {
				return fmt.Errorf("--site is required")
			}
			if len(args) == 0 {
				return fmt.Errorf("give a command after --")
			}
			l := workspaceRoot()
			client, err := daemon.Dial(l, site)
			if err != nil {
				return err
			}
			defer client.Close()
			payload, err := json.Marshal(map[string]any{
				"job": job, "actor": actorName(), "argv": args,
			})
			if err != nil {
				return err
			}
			resp, err := client.Do(daemon.Request{Verb: "run", Args: payload})
			if err != nil {
				return err
			}
			var out struct {
				Stdout string `json:"stdout"`
				Exit   int    `json:"exit"`
				Log    string `json:"log"`
			}
			if err := json.Unmarshal(resp.Data, &out); err != nil {
				return err
			}
			data, err := base64.StdEncoding.DecodeString(out.Stdout)
			if err != nil {
				return err
			}
			_, _ = os.Stdout.Write(data)
			if out.Exit != 0 {
				os.Exit(out.Exit)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&site, "site", "", "the helper site")
	cmd.Flags().StringVar(&job, "job", "", "the job whose source snapshot and payload are used")
	return cmd
}
