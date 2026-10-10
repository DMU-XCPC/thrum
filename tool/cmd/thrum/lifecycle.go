package main

import (
	"fmt"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	var (
		jobID      string
		label      string
		owner      string
		dependsOn  string
		ownedPaths string
		keep       bool
		base       string
		summary    string
	)

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open a job: the brief, the branch, and a labelled working directory",
		Long: "Opening touches the brief, the code repository and a working directory, and leaves\n" +
			"all three consistent or none. The label has to be declared already: declaring a\n" +
			"resource is a different event with a commit of its own.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobs.Open(layout(), jobs.OpenOptions{
				Job:        jobID,
				Owner:      owner,
				DependsOn:  list(dependsOn),
				OwnedPaths: list(ownedPaths),
				Keep:       keep,
				Label:      label,
				Base:       base,
				Summary:    summary,
			})
			if err != nil {
				return err
			}
			fmt.Printf("opened %s in %s (%s)\n", jobID, label, short(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job id (required)")
	cmd.Flags().StringVar(&label, "label", "", "the working directory to work in (required)")
	cmd.Flags().StringVar(&owner, "owner", "", "who is responsible; empty means unowned and claimable")
	cmd.Flags().StringVar(&dependsOn, "depends-on", "", "job ids that must reach done first")
	cmd.Flags().StringVar(&ownedPaths, "owned-paths", "", "paths this job touches, relative to the code repository root")
	cmd.Flags().BoolVar(&keep, "keep", false, "protect this job's payload from collection")
	cmd.Flags().StringVar(&base, "base", "", "the commit to branch from (default: the code repository's default branch)")
	cmd.Flags().StringVar(&summary, "summary", "", "the commit's summary")
	_ = cmd.MarkFlagRequired("job")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newHandoffCmd() *cobra.Command {
	var (
		jobID      string
		state      string
		next       string
		started    string
		blockedBy  string
		worktrees  string
		ownedPaths string
		assets     string
		summary    string
	)

	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Record a stretch of work",
		Long: "A handoff covers one stretch. Base and head are worked out from the branch and the\n" +
			"previous record rather than typed, because a hand gets them wrong. Write one when\n" +
			"you stop, and not before: work that runs for many rounds without stopping needs\n" +
			"nothing in between.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobs.WriteHandoff(layout(), jobs.HandoffOptions{
				Job:        jobID,
				Agent:      actor,
				Status:     state,
				Next:       next,
				Started:    started,
				Worktrees:  list(worktrees),
				BlockedBy:  list(blockedBy),
				OwnedPaths: list(ownedPaths),
				Assets:     list(assets),
				Summary:    summary,
			})
			if err != nil {
				return err
			}
			fmt.Printf("recorded %s (%s)\n", jobID, short(id))

			l := layout()
			switch state {
			case format.HandoffBlocked:
				// Something is waiting, so something has to be alive to wake it when
				// the wait ends. This is the only reason a daemon is started without
				// being asked for, and it leaves once nothing is waiting.
				if err := startDaemonIfNeeded(l); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: nothing here can wake %s when %s finishes: %v\n",
						jobID, strings.Join(list(blockedBy), ", "), err)
				}
			case format.HandoffDone:
				if told := announce(l, jobID, actor); len(told) > 0 {
					fmt.Printf("told %d site(s) that %s is done: %s\n", len(told), jobID, strings.Join(told, ", "))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job id (required)")
	cmd.Flags().StringVar(&state, "status", "", "done, partial, blocked or aborted (required)")
	cmd.Flags().StringVar(&next, "next", "", "what whoever continues does first; none when finished")
	cmd.Flags().StringVar(&started, "started", "", "when the stretch began, in either time form (default: now)")
	cmd.Flags().StringVar(&blockedBy, "blocked-by", "", "job ids this is waiting on")
	cmd.Flags().StringVar(&worktrees, "worktrees", "", "labels this stretch used")
	cmd.Flags().StringVar(&ownedPaths, "owned-paths", "", "paths actually touched")
	cmd.Flags().StringVar(&assets, "assets", "", "assets published by this stretch, as name@version")
	cmd.Flags().StringVar(&summary, "summary", "", "the commit's summary")
	_ = cmd.MarkFlagRequired("job")
	_ = cmd.MarkFlagRequired("status")
	return cmd
}

func newCloseCmd() *cobra.Command {
	var (
		jobID     string
		abandoned bool
		summary   string
	)

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close a job: set its final status, archive the brief, release the working directory",
		Long: "Closing merges nothing. The branch stays in the code repository and merging it is\n" +
			"the project's business: a closed job is a hand-off to whoever integrates, not an\n" +
			"integration itself.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			state := format.StatusDone
			if abandoned {
				state = format.StatusAbandoned
			}
			l := layout()
			id, err := jobs.Close(l, jobID, state, summary)
			if err != nil {
				return err
			}
			fmt.Printf("closed %s as %s (%s)\n", jobID, state, short(id))
			// Whatever was waiting on it can stop waiting.
			if told := announce(l, jobID, actor); len(told) > 0 {
				fmt.Printf("told %d site(s) that %s is done: %s\n", len(told), jobID, strings.Join(told, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job id (required)")
	cmd.Flags().BoolVar(&abandoned, "abandoned", false, "close it as abandoned rather than done")
	cmd.Flags().StringVar(&summary, "summary", "", "the commit's summary")
	_ = cmd.MarkFlagRequired("job")
	return cmd
}

func newClaimCmd() *cobra.Command {
	var jobID, owner string

	cmd := &cobra.Command{
		Use:   "claim",
		Short: "Take responsibility for a job, or hand it to somebody else",
		Long: "A job with no owner is one nobody is responsible for. Whoever takes it up writes\n" +
			"itself in without asking, because there is nobody left to ask; an owner may also\n" +
			"reassign a job it holds.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			who := owner
			if who == "" {
				who = actor
			}
			id, err := jobs.SetOwner(layout(), jobID, who, "")
			if err != nil {
				return err
			}
			fmt.Printf("%s is now owned by %s (%s)\n", jobID, who, short(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job id (required)")
	cmd.Flags().StringVar(&owner, "owner", "", "who takes it (default: this actor)")
	_ = cmd.MarkFlagRequired("job")
	return cmd
}

func newRelinquishCmd() *cobra.Command {
	var jobID string

	cmd := &cobra.Command{
		Use:   "relinquish",
		Short: "Give up ownership, leaving the job claimable",
		Long: "Giving ownership up is what makes a job claimable, and it is the owner's act. When\n" +
			"the owner is an actor who no longer exists, a person gives it up on their behalf,\n" +
			"since the job would otherwise be stuck forever.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobs.SetOwner(layout(), jobID, "", "")
			if err != nil {
				return err
			}
			fmt.Printf("%s is now unowned (%s)\n", jobID, short(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job id (required)")
	_ = cmd.MarkFlagRequired("job")
	return cmd
}

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage the labelled working directories",
		Long: "A label is free when git shows no job branch in it, after the cleaning the drift\n" +
			"rules require: an idle directory is clean and detached. An occupied label is never\n" +
			"taken from under the job holding it.",
	}

	var (
		label  string
		jobID  string
		detach string
	)

	add := &cobra.Command{
		Use:   "add",
		Short: "Materialise a label, on a job's branch or detached for comparison",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := worktrees.Add(layout(), label, jobID, detach); err != nil {
				return err
			}
			fmt.Printf("worktree %s is ready\n", label)
			return nil
		},
	}
	add.Flags().StringVar(&label, "label", "", "the label (required)")
	add.Flags().StringVar(&jobID, "job", "", "check out this job's branch")
	add.Flags().StringVar(&detach, "detach", "", "check out this revision, detached")
	_ = add.MarkFlagRequired("label")

	release := &cobra.Command{
		Use:   "release",
		Short: "Leave a working directory clean and detached, reporting what it discarded",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			discarded, err := worktrees.Release(layout(), label)
			if err != nil {
				return err
			}
			for _, p := range discarded {
				fmt.Printf("discarded %s\n", p)
			}
			fmt.Printf("worktree %s is idle\n", label)
			return nil
		},
	}
	release.Flags().StringVar(&label, "label", "", "the label (required)")
	_ = release.MarkFlagRequired("label")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Say what this site has materialised, and what each directory holds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := worktrees.List(layout())
			if err != nil {
				return err
			}
			for _, e := range entries {
				holds := e.Branch
				if holds == "" {
					holds = "detached"
				}
				fmt.Printf("%-20s %s\n", e.Label, holds)
			}
			return nil
		},
	}

	prune := &cobra.Command{
		Use:   "prune",
		Short: "Forget the working directories git can no longer find",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return worktrees.Prune(layout())
		},
	}

	cmd.AddCommand(add, release, listCmd, prune)
	return cmd
}

// short abbreviates a commit id for a message meant to be read.
func short(id string) string {
	id = trimNewline(id)
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
