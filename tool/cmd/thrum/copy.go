package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
)

// newCopyCmd moves ordinary files between sites: bytes only, no git, no store,
// no records, no queue.
func newCopyCmd() *cobra.Command {
	var to, from, job string
	cmd := &cobra.Command{
		Use:   "copy [--to <site>|--from <site>] [--job <id>] <src>... <dst>",
		Short: "Move ordinary files between sites",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (to == "") == (from == "") {
				return fmt.Errorf("give exactly one of --to or --from")
			}
			l := workspaceRoot()
			site := to
			outbound := true
			if from != "" {
				site, outbound = from, false
			}
			client, err := daemon.Dial(l, site)
			if err != nil {
				return err
			}
			defer client.Close()
			if outbound {
				src, dst := args[0], ""
				if len(args) > 1 {
					dst = args[len(args)-1]
				}
				data, err := os.ReadFile(src)
				if err != nil {
					return err
				}
				if dst == "" {
					dst = filepath.Base(src)
				}
				args, err := json.Marshal(map[string]string{
					"job": job, "name": filepath.Base(dst), "data": base64.StdEncoding.EncodeToString(data),
				})
				if err != nil {
					return err
				}
				_, err = client.Do(daemon.Request{Verb: "file.receive", Args: args})
				return err
			}
			// --from: fetch a file into the local payload or scratch directory.
			src, dst := args[0], ""
			if len(args) > 1 {
				dst = args[len(args)-1]
			}
			argsJSON, err := json.Marshal(map[string]string{"job": job, "name": filepath.Base(src)})
			if err != nil {
				return err
			}
			resp, err := client.Do(daemon.Request{Verb: "file.fetch", Args: argsJSON})
			if err != nil {
				return err
			}
			var out struct {
				Data string `json:"data"`
			}
			if err := json.Unmarshal(resp.Data, &out); err != nil {
				return err
			}
			data, err := base64.StdEncoding.DecodeString(out.Data)
			if err != nil {
				return err
			}
			if dst == "" {
				dst = filepath.Base(src)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", dst)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "the helper site to send to")
	cmd.Flags().StringVar(&from, "from", "", "the helper site to fetch from")
	cmd.Flags().StringVar(&job, "job", "", "the job whose payload data/ is used")
	return cmd
}
