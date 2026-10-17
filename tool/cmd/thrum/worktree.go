package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "worktree", Short: "Manage the labelled working directories"}

	var repo, job, purpose string
	var ephemeral bool
	add := &cobra.Command{
		Use:   "add <label> --repo <name> [--job <id>]",
		Short: "Declare a label and materialise it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			if job != "" {
				b, _, ok, err := jobs.ReadBrief(l, job)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("no job named %q", job)
				}
				if repo != "" && repo != b.Repo {
					return fmt.Errorf("job %q is in repository %q, not %q", job, b.Repo, repo)
				}
				repo = b.Repo
			}
			if repo == "" {
				return fmt.Errorf("--repo is required for a detached label")
			}
			if _, err := worktrees.Declare(l, args[0], repo, purpose, ephemeral); err != nil {
				return err
			}
			path, err := worktrees.Add(l, args[0], repo, job)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
	add.Flags().StringVar(&repo, "repo", "", "the repository the label belongs to")
	add.Flags().StringVar(&job, "job", "", "check a job's branch out, instead of a detached head")
	add.Flags().StringVar(&purpose, "purpose", "", "what the area is for")
	add.Flags().BoolVar(&ephemeral, "ephemeral", false, "destroy the directory on release")

	list := &cobra.Command{
		Use:   "list",
		Short: "What is declared and what is materialised",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := worktrees.List(workspaceRoot())
			if err != nil {
				return err
			}
			if jsonFlag {
				return printJSON(cmd, entries)
			}
			for _, e := range entries {
				state := "declared"
				if e.Path != "" {
					state = "materialised"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-8s %-12s %-20s %s\n",
					e.Label, e.Repo, state, e.Branch, e.Path)
			}
			return nil
		},
	}

	release := &cobra.Command{
		Use:   "release <label>",
		Short: "Leave a label clean and detached",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := worktrees.Release(workspaceRoot(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "released %s\n", args[0])
			return nil
		},
	}

	remove := &cobra.Command{
		Use:   "remove <label>",
		Short: "Retire a label that is not materialised",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := worktrees.Remove(workspaceRoot(), args[0], time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(add, list, release, remove)
	return cmd
}
