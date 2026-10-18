package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/retire"
	"github.com/multiagentworkspace/thrum/tool/internal/status"
	"github.com/multiagentworkspace/thrum/tool/internal/verify"
)

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Run every mechanical check and report what fails",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			problems := verify.Verify(workspaceRoot())
			out := cmd.OutOrStdout()
			if jsonFlag {
				if err := printJSON(cmd, problems); err != nil {
					return err
				}
			} else if len(problems) == 0 {
				fmt.Fprintln(out, "ok")
			} else {
				for _, p := range problems {
					fmt.Fprintln(out, p.String())
				}
			}
			if len(problems) > 0 {
				return fmt.Errorf("%d problem(s)", len(problems))
			}
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "The board, the working directories, and the repositories here",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := status.Build(workspaceRoot(), actorName())
			if err != nil {
				return err
			}
			if jsonFlag {
				return printJSON(cmd, r)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "site  %s\nroot  %s\n\njobs\n", r.Site, r.Root)
			for _, j := range r.Jobs {
				fmt.Fprintf(out, "  %-24s %-8s %-9s %-12s %s\n", j.Job, j.Repo, j.Status, j.State, j.Owner)
			}
			fmt.Fprintln(out, "\nworktrees")
			for _, w := range r.Worktrees {
				state := "declared"
				if w.Path != "" {
					state = "materialised"
				}
				fmt.Fprintf(out, "  %-16s %-8s %-12s %s\n", w.Label, w.Repo, state, w.Branch)
			}
			fmt.Fprintln(out, "\npayloads")
			if len(r.Payloads) == 0 {
				fmt.Fprintln(out, "  none")
			}
			for _, p := range r.Payloads {
				at := ""
				if p.CollectibleAt != "" {
					at = "  collectible at " + p.CollectibleAt
				}
				fmt.Fprintf(out, "  %-24s %-12s %8d bytes%s\n", p.Job, p.State, p.Bytes, at)
			}
			fmt.Fprintln(out, "\nrepos")
			for _, rp := range r.Repos {
				state := "not here"
				if rp.Materialised {
					state = "here"
				}
				fmt.Fprintf(out, "  %-16s %s\n", rp.Name, state)
			}
			fmt.Fprintln(out, "\nmail")
			if len(r.Mail) == 0 {
				fmt.Fprintln(out, "  none")
			}
			for _, m := range r.Mail {
				fmt.Fprintf(out, "  %-24s %-14s %s\n", m.ID, m.From, m.Subject)
			}
			return nil
		},
	}
}

func newCommitCmd() *cobra.Command {
	var summary string
	cmd := &cobra.Command{
		Use:   "commit --summary <text> [-- <paths>...]",
		Short: "Record one hand-made change as one event in the line",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if summary == "" {
				return fmt.Errorf("--summary is required")
			}
			l := workspaceRoot()
			paths := args
			if len(paths) == 0 {
				changed, err := changedUnder(l.Root)
				if err != nil {
					return err
				}
				paths = changed
			}
			if len(paths) == 0 {
				return fmt.Errorf("nothing to record under doc/")
			}
			rels := make([]string, 0, len(paths))
			for _, p := range paths {
				rel, err := docRelative(l.Root, p)
				if err != nil {
					return err
				}
				rels = append(rels, rel)
			}
			kind, subject := inferEvent(rels[0])
			for _, rel := range rels[1:] {
				k, s := inferEvent(rel)
				if k != kind || s != subject {
					return fmt.Errorf("%s and %s are different events; record them separately", rels[0], rel)
				}
			}
			message := kind
			if subject != "" {
				message += " " + subject
			}
			message += ": " + summary
			if err := line.Commit(l.Root, rels, message); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "recorded %s\n", message)
			return nil
		},
	}
	cmd.Flags().StringVar(&summary, "summary", "", "what changed")
	return cmd
}

func newRetireCmd() *cobra.Command {
	var why, supersededBy string
	cmd := &cobra.Command{
		Use:   "retire <path> --why <text>",
		Short: "Move a document to the archive with a notice",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := retire.Retire(workspaceRoot(), args[0], why, supersededBy, time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "retired %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&why, "why", "", "why it is retired")
	cmd.Flags().StringVar(&supersededBy, "superseded-by", "", "the doc/ path that replaces it")
	return cmd
}

// changedUnder lists the changed and untracked paths under doc/, for a commit that
// names no paths.
func changedUnder(root string) ([]string, error) {
	out, err := gitx.Run(root, "diff", "--name-only", "HEAD", "--", "doc")
	if err != nil {
		return nil, err
	}
	untracked, err := gitx.Run(root, "ls-files", "--others", "--exclude-standard", "--", "doc")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, blob := range []string{out, untracked} {
		for _, ln := range strings.Split(blob, "\n") {
			if ln = strings.TrimSpace(ln); ln != "" {
				paths = append(paths, ln)
			}
		}
	}
	return paths, nil
}

// docRelative resolves a path argument to a workspace-relative path under doc/.
func docRelative(root, arg string) (string, error) {
	abs := arg
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, arg)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel != "doc" && !strings.HasPrefix(rel, "doc/") {
		return "", fmt.Errorf("%s is not under doc/: hand edits are to doc/, and a handoff belongs to job handoff", arg)
	}
	return rel, nil
}

func inferEvent(rel string) (kind, subject string) {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] != "doc" {
		return "record", ""
	}
	switch parts[1] {
	case "actor", "site", "repo", "worktree":
		kind = parts[1]
		if len(parts) >= 3 {
			subject = strings.TrimSuffix(parts[len(parts)-1], ".yaml")
		}
		return kind, subject
	case "task":
		if len(parts) >= 4 {
			return "brief", parts[2]
		}
	case "topic":
		if len(parts) >= 3 {
			return "topic", parts[2]
		}
	case "convention":
		return "convention", strings.TrimSuffix(parts[len(parts)-1], ".md")
	case "asset":
		if len(parts) >= 4 {
			return "asset", parts[2] + "@" + strings.TrimSuffix(parts[len(parts)-1], ".md")
		}
	case "template":
		return "template", strings.TrimSuffix(parts[len(parts)-1], ".md")
	}
	return "record", ""
}
