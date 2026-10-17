package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/clean"
	"github.com/multiagentworkspace/thrum/tool/internal/update"
)

func newCleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Remove what the shared histories do not track and do not ignore",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			removed, err := clean.Run(workspaceRoot())
			if err != nil {
				return err
			}
			for _, p := range removed {
				fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", p)
			}
			if len(removed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing to remove")
			}
			return nil
		},
	}
}

func newUpdateCmd() *cobra.Command {
	var force, dryRun bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Bring the skeleton, the ignore rules and the empty directories up to date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := update.Run(workspaceRoot(), force, dryRun)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, p := range r.Created {
				fmt.Fprintf(out, "created  %s\n", p)
			}
			for _, p := range r.Updated {
				fmt.Fprintf(out, "updated  %s\n", p)
			}
			for _, p := range r.AddedRules {
				fmt.Fprintf(out, "ignore   %s\n", p)
			}
			for _, p := range r.CreatedDirs {
				fmt.Fprintf(out, "dir      %s\n", p)
			}
			for _, p := range r.Templates {
				fmt.Fprintf(out, "template %s differs; --force writes it\n", p)
			}
			if dryRun {
				fmt.Fprintln(out, "(dry run: nothing written)")
			}
			if len(r.Created)+len(r.Updated)+len(r.AddedRules)+len(r.CreatedDirs)+len(r.Templates) == 0 {
				fmt.Fprintln(out, "nothing to do")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "write copy-me files that differ")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report only")
	return cmd
}
