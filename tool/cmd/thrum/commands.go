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

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the derived views: the task board, worktrees, leases, occupancy, payloads",
		Long: "Status reads rather than maintains. The brief says whether a job is still meant to\n" +
			"happen, the last handoff says how the last stretch went, git says which directory\n" +
			"holds which branch, and var/ says who is on what here.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := status.Gather(layout())
			if err != nil {
				return err
			}
			report.Print(cmd.OutOrStdout())
			return nil
		},
	}
}
