package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
)

func newWhereCmd() *cobra.Command {
	var job string
	cmd := &cobra.Command{
		Use:   "where [--job <id>]",
		Short: "Print the working directory a job is in, or the workspace root",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			if job == "" {
				fmt.Fprintln(cmd.OutOrStdout(), l.Root)
				return nil
			}
			path, ok, err := jobs.Where(l, job)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no working directory holds job %q", job)
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
	cmd.Flags().StringVar(&job, "job", "", "the job")
	return cmd
}

func newEnvCmd() *cobra.Command {
	var job string
	cmd := &cobra.Command{
		Use:   "env [--job <id>]",
		Short: "Print the environment variables that keep a job's tools inside the workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			out := cmd.OutOrStdout()
			if job != "" {
				if _, _, ok, err := jobs.ReadBrief(l, job); err != nil {
					return err
				} else if !ok {
					return fmt.Errorf("no job named %q", job)
				}
			}
			fmt.Fprintf(out, "export THRUM_ROOT=%s\n", l.Root)
			fmt.Fprintf(out, "export THRUM_ACTOR=%s\n", actorName())
			if job != "" {
				fmt.Fprintf(out, "export THRUM_JOB=%s\n", job)
				fmt.Fprintf(out, "export TMPDIR=%s\n", l.PayloadSub(job, "tmp"))
				fmt.Fprintf(out, "export XDG_STATE_HOME=%s\n", l.PayloadSub(job, "state"))
			}
			fmt.Fprintf(out, "export XDG_CACHE_HOME=%s\n", l.CacheDir())
			return nil
		},
	}
	cmd.Flags().StringVar(&job, "job", "", "the job whose payload the variables point at")
	return cmd
}
