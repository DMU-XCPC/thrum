package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/bundles"
)

func newBundleCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bundle", Short: "Carry one job's payload to another site"}

	var out string
	export := &cobra.Command{
		Use:   "export <job-id> --out <file>",
		Short: "Write the payload and the handoff it belongs to",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bundles.Export(workspaceRoot(), args[0], out, time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exported %s to %s\n", args[0], out)
			return nil
		},
	}
	export.Flags().StringVar(&out, "out", "", "where to write the bundle, outside the workspace")

	var replace bool
	importCmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Unpack a payload into job/<job-id>/",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bundles.Import(workspaceRoot(), args[0], replace); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported %s\n", args[0])
			return nil
		},
	}
	importCmd.Flags().BoolVar(&replace, "replace", false, "discard the payload that is already there")

	cmd.AddCommand(export, importCmd)
	return cmd
}
