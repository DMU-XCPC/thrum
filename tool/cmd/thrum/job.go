package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
)

func newJobCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "job", Short: "Open a job, write its handoff, close it"}

	var repo, label, purpose, goal, scope, criteria, owner, touches, depends string
	var keep, ephemeral bool

	open := &cobra.Command{
		Use:   "open <job-id> --repo <name> --label <label>",
		Short: "Write the brief, create the branch, and materialise the directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if repo == "" || label == "" {
				return fmt.Errorf("--repo and --label are required")
			}
			path, err := jobs.Open(workspaceRoot(), jobs.OpenOptions{
				Job: args[0], Repo: repo, Label: label, Purpose: purpose,
				Goal: goal, Scope: scope, Criteria: criteria, Owner: owner,
				Touches: splitList(touches), DependsOn: splitList(depends),
				Keep: keep, Ephemeral: ephemeral,
			}, time.Now())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
	open.Flags().StringVar(&repo, "repo", "", "the code repository the job works in")
	open.Flags().StringVar(&label, "label", "", "the area of work")
	open.Flags().StringVar(&purpose, "purpose", "", "what the area is for")
	open.Flags().StringVar(&goal, "goal", "", "what the job is for")
	open.Flags().StringVar(&scope, "scope", "", "what is out of scope")
	open.Flags().StringVar(&criteria, "criteria", "", "how it will be judged")
	open.Flags().StringVar(&owner, "owner", "", "who the work is promised to")
	open.Flags().StringVar(&touches, "touches", "", "paths the job expects to touch, comma-separated")
	open.Flags().StringVar(&depends, "depends-on", "", "job ids that must be done first, comma-separated")
	open.Flags().BoolVar(&keep, "keep", false, "keep this job's payload after it closes")
	open.Flags().BoolVar(&ephemeral, "ephemeral", false, "destroy the working directory on release")

	list := &cobra.Command{
		Use:   "list",
		Short: "The task board",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := jobs.List(workspaceRoot())
			if err != nil {
				return err
			}
			if jsonFlag {
				return printJSON(cmd, rows)
			}
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-26s %-8s %-9s %-14s %s\n",
					r.Job, r.Repo, r.Status, r.State, r.Owner)
			}
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show <job-id>",
		Short: "One job in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			b, body, ok, err := jobs.ReadBrief(l, args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no job named %q", args[0])
			}
			h, hasHandoff, err := jobs.Last(l, args[0])
			if err != nil {
				return err
			}
			if jsonFlag {
				return printJSON(cmd, map[string]any{"brief": b, "last_handoff": h})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "job      %s\nrepo     %s\nbranch   %s\nstatus   %s\n",
				b.Job, b.Repo, b.Branch, b.Status)
			if b.Owner != "" {
				fmt.Fprintf(out, "owner    %s\n", b.Owner)
			}
			fmt.Fprintf(out, "touches  %s\n", strings.Join(b.Touches, ", "))
			if hasHandoff {
				fmt.Fprintf(out, "last     %s, %s..%s\nnext     %s\n",
					h.Status, h.Started, h.Ended, h.Next)
			} else {
				fmt.Fprintln(out, "last     no handoff yet")
			}
			fmt.Fprintf(out, "\nbrief\n\n%s", body)
			return nil
		},
	}

	claim := &cobra.Command{
		Use:   "claim <job-id>",
		Short: "Set the owner on a job that has none",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jobs.Claim(workspaceRoot(), args[0], actorName()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s owns %s\n", actorName(), args[0])
			return nil
		},
	}

	var to string
	reassign := &cobra.Command{
		Use:   "reassign <job-id> --to <actor>",
		Short: "Change an owner",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			if err := jobs.Reassign(workspaceRoot(), args[0], to); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s owns %s\n", to, args[0])
			return nil
		},
	}
	reassign.Flags().StringVar(&to, "to", "", "the new owner")

	relinquish := &cobra.Command{
		Use:   "relinquish <job-id>",
		Short: "Give a job up, making it claimable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jobs.Relinquish(workspaceRoot(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is unowned\n", args[0])
			return nil
		},
	}

	var status, body, next, supersedes string
	var artifacts, worktreeLabels, blockedBy, metrics []string
	handoff := &cobra.Command{
		Use:   "handoff <job-id> --status <s>",
		Short: "Record one stretch of work in the ledger",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := parseMetrics(metrics)
			if err != nil {
				return err
			}
			if err := jobs.Handoff(workspaceRoot(), jobs.HandoffOptions{
				Job: args[0], Status: status, Next: next, BodyFile: body,
				Artifacts: artifacts, Worktrees: worktreeLabels,
				BlockedBy: blockedBy, Supersedes: supersedes, Metrics: m,
			}, actorName(), time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "recorded %s as %s\n", args[0], status)
			return nil
		},
	}
	handoff.Flags().StringVar(&status, "status", "", "done, partial, blocked or aborted")
	handoff.Flags().StringVar(&next, "next", "", "what whoever continues does first")
	handoff.Flags().StringVar(&body, "body", "", "the body, from a file or - for stdin")
	handoff.Flags().StringArrayVar(&artifacts, "artifact", nil, "a file under job/<job-id>/ to name")
	handoff.Flags().StringArrayVar(&worktreeLabels, "worktree", nil, "a label the work used")
	handoff.Flags().StringArrayVar(&blockedBy, "blocked-by", nil, "a job this stretch waits on")
	handoff.Flags().StringVar(&supersedes, "supersedes", "", "a handoff this one corrects")
	handoff.Flags().StringArrayVar(&metrics, "metric", nil, "a count, as key=value")

	var closeStatus string
	closeCmd := &cobra.Command{
		Use:   "close <job-id> --status done|abandoned",
		Short: "End a job and file its brief",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jobs.Close(workspaceRoot(), args[0], closeStatus, time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "closed %s as %s\n", args[0], closeStatus)
			return nil
		},
	}
	closeCmd.Flags().StringVar(&closeStatus, "status", "", "done or abandoned")

	cmd.AddCommand(open, list, show, claim, reassign, relinquish, handoff, closeCmd)
	return cmd
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseMetrics(values []string) (map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for _, v := range values {
		k, val, ok := strings.Cut(v, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--metric %q is not key=value", v)
		}
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			out[k] = f
		} else {
			out[k] = val
		}
	}
	return out, nil
}
