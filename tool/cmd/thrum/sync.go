package main

import (
	"fmt"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/spf13/cobra"
)

// newSyncCmd pushes the records, and at a job move the branch too.
//
// It is a floor rather than a ceiling: pushing more often only makes the backup
// fresher, and nothing requires a fetch in order to start work. A site with no remote
// is told plainly rather than silently doing nothing.
func newSyncCmd() *cobra.Command {
	var (
		jobID string
		fetch bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Push the records at the moment they have to travel, and fetch when they must arrive",
		Long: "The coordination repository goes first: the records are what another site needs in\n" +
			"order to do anything at all. At a job move the branch goes too, because the receiving\n" +
			"site has to be able to fetch it. A site with no remote configured is told so.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()

			branch, err := currentBranch(l.Root)
			if err != nil {
				return err
			}

			if fetch {
				out, err := gitx.Run(l.Root, "pull", "--ff-only", "origin", branch)
				if err != nil {
					return fmt.Errorf("fetching the records: %w", err)
				}
				report("records", out)
			}

			if !hasRemote(l.Root) {
				return fmt.Errorf("the coordination repository has no remote, so there is nowhere to push the records")
			}
			// The refspec is spelled out rather than left to the upstream: a site that
			// has just been created has no upstream configured, and a push that needs
			// one is a push that fails on the first day.
			out, err := gitx.Run(l.Root, "push", "origin", "HEAD:refs/heads/"+branch)
			if err != nil {
				return fmt.Errorf("pushing the records: %w", err)
			}
			report("records", out)

			if jobID == "" {
				return nil
			}
			// The branch lives in the code repository, and that is where it is pushed
			// from: the receiving site fetches it from there.
			if !hasRemote(l.CodeRepo()) {
				return fmt.Errorf("the code repository has no remote, so the branch for %s has nowhere to go", jobID)
			}
			jobBranch := "job/" + jobID
			out, err = gitx.Run(l.CodeRepo(), "push", "origin", jobBranch)
			if err != nil {
				return fmt.Errorf("pushing %s: %w", jobBranch, err)
			}
			report(jobBranch, out)
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "also push this job's branch, for a site that has to fetch it")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch the records first, when they have to arrive")
	return cmd
}

// currentBranch is the branch this repository is on, which is the one a refspec has to
// name.
func currentBranch(dir string) (string, error) {
	out, err := gitx.Run(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("this repository is not on a branch, so there is nothing with a name to push: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func hasRemote(dir string) bool {
	out, err := gitx.Run(dir, "remote")
	return err == nil && strings.TrimSpace(out) != ""
}

// report prints what git said, and nothing when it said nothing: a line per operation
// only when there is something to say.
func report(what, out string) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			fmt.Printf("%s: %s\n", what, line)
		}
	}
}
