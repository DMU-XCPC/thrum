package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

func newWorkCmd() *cobra.Command {
	var repo, label, purpose, goal, owner, touches, depends string
	var keep, ephemeral bool

	cmd := &cobra.Command{
		Use:   "work <job-id> [--repo <name> --label <label>]",
		Short: "Open or take up a job, and enter its working directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			id := args[0]
			out := cmd.OutOrStdout()

			b, _, ok, err := jobs.ReadBrief(l, id)
			if err != nil {
				return err
			}
			if !ok {
				if repo == "" || label == "" {
					return fmt.Errorf("--repo and --label are required to open a job")
				}
				o := jobs.OpenOptions{
					Job: id, Repo: repo, Label: label, Purpose: purpose, Goal: goal,
					Owner: owner, Touches: splitList(touches), DependsOn: splitList(depends),
					Keep: keep, Ephemeral: ephemeral,
				}
				if o.Owner == "" {
					o.Owner = actorName()
				}
				path, err := jobs.Open(l, o, time.Now())
				if err != nil {
					return err
				}
				fmt.Fprintln(out, path)
				fmt.Fprintf(out, "eval \"$(thrum env --job %s)\"\n", id)
				return nil
			}

			if b.Owner != "" && b.Owner != actorName() {
				return fmt.Errorf("job %q is owned by %s; reassign it before working here", id, b.Owner)
			}
			if b.Owner == "" {
				if err := jobs.Claim(l, id, actorName()); err != nil {
					return err
				}
			}
			path, ok, err := jobs.Where(l, id)
			if err != nil {
				return err
			}
			if !ok {
				if label == "" {
					return fmt.Errorf("this site holds no directory for %q; --label names one", id)
				}
				if _, err := worktrees.Declare(l, label, b.Repo, purpose, ephemeral); err != nil {
					return err
				}
				path, err = worktrees.Add(l, label, b.Repo, id)
				if err != nil {
					return err
				}
			}
			fmt.Fprintln(out, path)
			fmt.Fprintf(out, "eval \"$(thrum env --job %s)\"\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "the repository, when opening a job")
	cmd.Flags().StringVar(&label, "label", "", "the area of work")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what the area is for")
	cmd.Flags().StringVar(&goal, "goal", "", "what the job is for, when opening it")
	cmd.Flags().StringVar(&owner, "owner", "", "who the work is promised to")
	cmd.Flags().StringVar(&touches, "touches", "", "paths the job expects to touch, comma-separated")
	cmd.Flags().StringVar(&depends, "depends-on", "", "job ids that must be done first, comma-separated")
	cmd.Flags().BoolVar(&keep, "keep", false, "keep this job's payload after it closes")
	cmd.Flags().BoolVar(&ephemeral, "ephemeral", false, "destroy the working directory on release")
	return cmd
}
