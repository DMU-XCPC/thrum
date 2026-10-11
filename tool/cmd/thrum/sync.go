package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/commit"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/spf13/cobra"
)

// newSyncCmd publishes what this site has recorded, and fetches what has to arrive.
//
// It is the command a site runs after it has been alone: the records it made while the
// remote was unreachable are a backlog, and publishing a backlog means putting it on top
// of what was published meanwhile. That is the same step every record write takes, so
// sync takes it too, rather than being the one command that cannot publish its own work.
func newSyncCmd() *cobra.Command {
	var jobID string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Publish the records this site has made, and fetch what has arrived",
		Long: "The coordination repository goes first: the records are what another site needs in\n" +
			"order to do anything at all. This brings the site up to date from the shared line,\n" +
			"puts its own unpublished records on top, and publishes them, which is how a site that\n" +
			"recorded while it was alone gets its backlog into the line. At a job move the branch\n" +
			"goes too, because the receiving site has to be able to fetch it.\n\n" +
			"Nothing is merged. Records that do not go together with what was published elsewhere\n" +
			"are refused, and so is a branch that moved at the other site, because merging a job's\n" +
			"branch is the project's business.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()

			branch, err := currentBranch(l.Root)
			if err != nil {
				return err
			}
			if !hasOrigin(l.Root) {
				return fmt.Errorf("the coordination repository has no remote, so there is nowhere to publish the records")
			}

			before, _ := head(l.Root)
			// The same step every record write takes, and the reason a backlog can be
			// published at all: fetch, and put this site's unpublished records on top of
			// what the others published.
			if err := commit.Current(l); err != nil {
				return err
			}
			if after, _ := head(l.Root); after != before {
				fmt.Println("integrated what was published elsewhere, and put this site's records on top")
			}

			// The line may not have this branch on it at all yet — a site's first
			// publish is exactly that — so the count of what is going is asked for in
			// the one form that means "everything here" rather than naming a ref git
			// has never seen.
			from := "origin/" + branch
			if !gitx.Resolves(l.Root, from) {
				from = ""
			}
			ahead, err := countBetween(l.Root, from, "HEAD")
			if err != nil {
				return err
			}
			switch {
			case ahead == 0:
				fmt.Printf("nothing to publish: origin/%s already has everything this site recorded\n", branch)
			default:
				if _, err := gitx.Run(l.Root, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch); err != nil {
					if unreachable(l.Root) {
						return fmt.Errorf("origin cannot be reached, so the %d record(s) this site has made are still only here", ahead)
					}
					return fmt.Errorf("the line moved again while this was publishing: run sync once more, and nothing was merged")
				}
				fmt.Printf("published %d record(s) to origin/%s\n", ahead, branch)
			}

			if jobID == "" {
				return nil
			}
			// The branch lives in the code repository, and that is where it is published
			// from: the receiving site fetches it from there.
			if !hasOrigin(l.CodeRepo()) {
				return fmt.Errorf("the code repository has no origin remote, so the branch for %s has nowhere to go", jobID)
			}
			jobBranch := "job/" + jobID
			if _, err := gitx.Run(l.CodeRepo(), "push", "--quiet", "origin", jobBranch); err != nil {
				if unreachable(l.CodeRepo()) {
					return fmt.Errorf("%s could not be published because the code repository's origin cannot be reached", jobBranch)
				}
				return fmt.Errorf("%s has moved at the other site, and this refused rather than resolving it: merging a job's branch is the project's business", jobBranch)
			}
			fmt.Printf("published %s\n", jobBranch)
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "also publish this job's branch, for a site that has to fetch it")
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

// hasOrigin reports whether a repository has somewhere to publish to.
func hasOrigin(dir string) bool {
	_, err := gitx.Run(dir, "remote", "get-url", "origin")
	return err == nil
}

// unreachable reports whether the remote is the reason a push failed, rather than the
// line having moved.
func unreachable(dir string) bool {
	_, err := gitx.Run(dir, "ls-remote", "--quiet", "origin")
	return err != nil
}

func head(dir string) (string, error) {
	out, err := gitx.Run(dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

func countBetween(dir, from, to string) (int, error) {
	rng := to
	if from != "" {
		rng = from + ".." + to
	}
	out, err := gitx.Run(dir, "rev-list", "--count", rng)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, err
	}
	return n, nil
}
