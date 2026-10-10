package main

import (
	"fmt"
	"strings"

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

// newJoinCmd makes this machine another site of a workspace that already exists.
//
// Creating and joining are different acts: a new site takes the shared line rather than
// beginning a history of its own, because a second first commit would be a second
// workspace.
func newJoinCmd() *cobra.Command {
	var (
		remote string
		site   string
		code   string
		store  string
	)

	cmd := &cobra.Command{
		Use:   "join <dir> --remote <url> --site <name>",
		Short: "Make this machine another site of a workspace that already exists",
		Long: "The records come from the shared line, so nothing is committed here: this fetches,\n" +
			"checks out, and fills in what a repository cannot carry — the ignore rules, the\n" +
			"empty directories, this site's name, and its own copies of the code and asset\n" +
			"repositories. The roster has to declare the site first, because the roster is what\n" +
			"the other sites read to decide who exists.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := workspace.Join(workspace.JoinOptions{
				Root:   args[0],
				Remote: remote,
				Site:   site,
				Code:   code,
				Store:  store,
			})
			if err != nil {
				return err
			}
			fmt.Printf("joined %s as site %s\n", l.Root, site)
			if code == "" {
				fmt.Println("no --code was given, so the code repository here is empty: point it at the project's, or start one")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "the coordination repository to join (required)")
	cmd.Flags().StringVar(&site, "site", "", "the name of this site (required)")
	cmd.Flags().StringVar(&code, "code", "", "the code repository to clone here")
	cmd.Flags().StringVar(&store, "store", "", "the asset repository to clone here")
	_ = cmd.MarkFlagRequired("remote")
	_ = cmd.MarkFlagRequired("site")
	return cmd
}

// newUpdateCmd brings this workspace up to date with the template the tooling carries.
//
// The two halves are treated differently, and that is the whole design of this command.
// The unversioned skeleton — the empty directories, and the ignore rules in both copies —
// is the tooling's, so bringing it up to date loses nothing. `doc/template/` is the
// project's: it is versioned, in the shared line, and a workspace may keep a different
// one on purpose, so a difference there is reported and only written when asked for.
func newUpdateCmd() *cobra.Command {
	var (
		dryRun        bool
		adoptTemplate bool
		narrow        bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Bring this workspace up to date with the template the tooling carries",
		Long: "A workspace materialises the template once, and after that the two can drift: the\n" +
			"tooling learns to ignore something new, or grows a directory, and nothing notices,\n" +
			"because none of it is in a repository. This compares the two.\n\n" +
			"Directories and the ignore rules are the tooling's, so they are brought up to date\n" +
			"here. Rules this workspace has and the tooling does not are kept unless --narrow is\n" +
			"given: widening what is ignored is safe on its own, and narrowing it is a decision.\n\n" +
			"doc/template/ is the project's, so a difference is reported rather than written.\n" +
			"With --adopt-template the tooling's copies are written over this workspace's and\n" +
			"left uncommitted, so that git diff shows exactly what would change; committing or\n" +
			"reverting them is then a person's decision, recorded as the event it is.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			res, err := workspace.Update(l, workspace.UpdateOptions{
				DryRun:        dryRun,
				AdoptTemplate: adoptTemplate,
				Narrow:        narrow,
			})
			if err != nil {
				return err
			}
			prefix := ""
			if dryRun {
				prefix = "would: "
			}
			for _, dir := range res.DirsCreated {
				fmt.Printf("%screate %s\n", prefix, dir)
			}
			if res.IgnoreWritten {
				fmt.Printf("%srewrite .gitignore and .git/info/exclude from the tooling's list\n", prefix)
			}
			for _, rule := range res.IgnoreKept {
				fmt.Printf("kept the rule %q, which this workspace had and the tooling does not: --narrow drops it\n", rule)
			}
			for _, t := range res.Templates {
				state := "differs from the tooling's"
				if len(t.Workspace) == 0 {
					state = "is missing"
				}
				fmt.Printf("doc/template/%s %s\n", strings.TrimPrefix(t.Path, "doc/template/"), state)
			}
			for _, path := range res.TemplatesAdopted {
				fmt.Printf("wrote %s from the tooling's copy, uncommitted\n", path)
			}
			if len(res.TemplatesAdopted) > 0 {
				fmt.Printf("record it when you are content with it: thrum commit --summary %q -- %s\n",
					"adopted the tooling's template", strings.Join(res.TemplatesAdopted, " "))
			} else if len(res.Templates) > 0 && !adoptTemplate {
				fmt.Println("the tooling's copy is not written on its own initiative: --adopt-template writes it, uncommitted")
			}
			if len(res.DirsCreated) == 0 && !res.IgnoreWritten && len(res.Templates) == 0 {
				fmt.Println("up to date: the directories and the ignore rules are the tooling's")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would change, and write nothing")
	cmd.Flags().BoolVar(&adoptTemplate, "adopt-template", false, "also write the tooling's doc/template/ files, uncommitted, for a person to record")
	cmd.Flags().BoolVar(&narrow, "narrow", false, "also drop ignore rules the tooling no longer has")
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
