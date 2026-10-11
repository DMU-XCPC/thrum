package main

import (
	"fmt"

	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
	"github.com/spf13/cobra"
)

// newWhereCmd prints the working directory a job is materialised in.
//
// It is the one question an actor has to answer before it can do anything, and it used to be
// answered by running git's own worktree list and reading the label out of its output. The
// answer is derived, not stored: which directory holds a branch is what git already knows,
// and this only asks.
func newWhereCmd() *cobra.Command {
	var jobID string

	cmd := &cobra.Command{
		Use:   "where [--job <id>]",
		Short: "Print the working directory a job is in, or the workspace root",
		Long: "With --job, the directory whose branch is that job's — which is where its work is done.\n" +
			"Without it, the workspace root, for a command that needs to be somewhere.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			if jobID == "" {
				fmt.Println(l.Root)
				return nil
			}
			_, dir, err := worktreeOf(l, jobID)
			if err != nil {
				return err
			}
			fmt.Println(dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job whose working directory to print")
	return cmd
}

// worktreeOf is the label and the directory holding a job's branch.
func worktreeOf(l workspace.Layout, job string) (string, string, error) {
	entries, err := worktrees.List(l)
	if err != nil {
		return "", "", err
	}
	for _, e := range entries {
		if e.Job() == job {
			return e.Label, e.Path, nil
		}
	}
	return "", "", fmt.Errorf("no working directory here holds job %s: materialise one with `thrum work --job %s --label <label>`, or work where it already is", job, job)
}
