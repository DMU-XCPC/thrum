package main

import (
	"fmt"

	"github.com/multiagentworkspace/thrum/tool/internal/status"
	"github.com/multiagentworkspace/thrum/tool/internal/validate"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/spf13/cobra"
)

func layout() workspace.Layout { return workspace.New(rootDir) }

func newCreateCmd() *cobra.Command {
	var site string

	cmd := &cobra.Command{
		Use:   "create <dir>",
		Short: "Create a workspace from the template",
		Long: "Create instantiates a workspace. It is not a file copy: the code and asset\n" +
			"repositories are initialised rather than duplicated, and the empty directories\n" +
			"are created rather than copied, because a repository cannot carry an empty\n" +
			"directory.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if site == "" {
				site = defaultSite()
			}
			l, err := workspace.Create(workspace.CreateOptions{
				Root:  args[0],
				Site:  site,
				Actor: actor,
			})
			if err != nil {
				return err
			}
			fmt.Printf("created %s as site %s\n", l.Root, site)
			return nil
		},
	}
	cmd.Flags().StringVar(&site, "site", "", "the name of this site (default: this hostname, slugified)")
	return cmd
}

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Run every mechanical check and report what fails",
		Long: "Verify runs the checks the design collects in one list. It reports and never\n" +
			"repairs: the thing being reported may be somebody else's work in progress, and a\n" +
			"wrong repair costs more than a stale line.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			findings := validate.Validate(layout())
			if len(findings) == 0 {
				fmt.Println("ok")
				return nil
			}
			for _, f := range findings {
				fmt.Println(f)
			}
			return fmt.Errorf("%d findings", len(findings))
		},
	}
}

// printActors is what the tooling knows about names, and who answers: the roster, and
// which sites this one can reach right now. Reachability is a question about the
// present and is never cached.
func printActors(cmd *cobra.Command, l workspace.Layout) {
	out := cmd.OutOrStdout()
	entries, reports := rosterEverywhere(l)

	fmt.Fprintln(out, "actors")
	if len(entries) == 0 {
		fmt.Fprintln(out, "  (no names yet: an actor exists as soon as it does something)")
	}
	for _, e := range entries {
		job := ""
		if e.Job != "" {
			job = "  on " + e.Job
		}
		fmt.Fprintf(out, "  %-16s %-14s last seen %s%s\n", e.Name, e.Site, e.LastSeen, job)
	}

	fmt.Fprintln(out, "sites")
	if len(reports) == 0 {
		fmt.Fprintln(out, "  (the roster declares no sites)")
	}
	for _, p := range reports {
		state := "unreachable"
		if p.Reachable {
			state = "reachable"
		}
		note := ""
		if p.Note != "" {
			note = "  " + p.Note
		}
		fmt.Fprintf(out, "  %-20s %-12s %-24s%s\n", p.Name, state, p.Reach, note)
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the derived views: the task board, worktrees, leases, occupancy, payloads",
		Long: "Status reads rather than maintains. The brief says whether a job is still meant to\n" +
			"happen, the last handoff says how the last stretch went, git says which directory\n" +
			"holds which branch, and var/ says who is on what here.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			report, err := status.Gather(l)
			if err != nil {
				return err
			}
			report.Print(cmd.OutOrStdout())
			// Names and reachability are questions about the present, so they are
			// answered here rather than in a file.
			printActors(cmd, l)
			return nil
		},
	}
}
