package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/assets"
)

func newAssetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "asset", Short: "Durable bytes in the store, named by asset and version"}

	ingest := &cobra.Command{
		Use:   "ingest <path>",
		Short: "Write bytes into the store and compute their raw hash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, commit, err := assets.Ingest(workspaceRoot(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "commit %s\n", commit)
			if a.SHA256 != "" {
				fmt.Fprintf(out, "sha256 %s\nbytes  %d\n", a.SHA256, a.Bytes)
			}
			return nil
		},
	}

	var from, description string
	publish := &cobra.Command{
		Use:   "publish <name> <version>",
		Short: "Tag the ingested bytes and write the manifest",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assets.Publish(workspaceRoot(), args[0], args[1], from, description, time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "published %s@%s\n", args[0], args[1])
			return nil
		},
	}
	publish.Flags().StringVar(&from, "from", "", "the commit to tag (default the last ingest)")
	publish.Flags().StringVar(&description, "description", "", "what the version is")

	var job string
	materialise := &cobra.Command{
		Use:   "materialise <name> <version> --job <id>",
		Short: "Extract a version into a job's transient data directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if job == "" {
				return fmt.Errorf("--job is required")
			}
			if err := assets.Materialise(workspaceRoot(), args[0], args[1], job); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "materialised %s@%s into job/%s/data\n", args[0], args[1], job)
			return nil
		},
	}
	materialise.Flags().StringVar(&job, "job", "", "the job whose payload receives the bytes")

	var why string
	withdraw := &cobra.Command{
		Use:   "withdraw <name> <version> --why <text>",
		Short: "Retire a version and delete its tag",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if why == "" {
				return fmt.Errorf("--why is required: a withdrawal states its reason")
			}
			if err := assets.Withdraw(workspaceRoot(), args[0], args[1], why, time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "withdrew %s@%s\n", args[0], args[1])
			return nil
		},
	}
	withdraw.Flags().StringVar(&why, "why", "", "why it is withdrawn")

	store := &cobra.Command{
		Use:   "store",
		Short: "Objects no tag reaches",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lines, err := assets.Unreachable(workspaceRoot())
			if err != nil {
				return err
			}
			if jsonFlag {
				return printJSON(cmd, lines)
			}
			for _, ln := range lines {
				fmt.Fprintln(cmd.OutOrStdout(), ln)
			}
			return nil
		},
	}

	prune := &cobra.Command{
		Use:   "prune",
		Short: "Collect the objects no tag reaches",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assets.Prune(workspaceRoot()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "pruned")
			return nil
		},
	}

	cmd.AddCommand(ingest, publish, materialise, withdraw, store, prune)
	return cmd
}
