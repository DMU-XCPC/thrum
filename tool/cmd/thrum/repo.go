package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/repos"
)

type repoView struct {
	Repo     string `json:"repo"`
	About    string `json:"about,omitempty"`
	Origin   string `json:"origin,omitempty"`
	Retired  string `json:"retired,omitempty"`
	Material bool   `json:"materialised"`
}

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Declare the code repositories a workspace has",
	}

	var about, from string
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Declare a code repository and materialise it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := repos.Add(workspaceRoot(), args[0], about, from); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", args[0])
			return nil
		},
	}
	add.Flags().StringVar(&about, "about", "", "what the repository holds, in a person's words")
	add.Flags().StringVar(&from, "from", "", "where to materialise it from: a URL or a path")

	list := &cobra.Command{
		Use:   "list",
		Short: "Every declared repository, and which this site has",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			decls, err := repos.List(l)
			if err != nil {
				return err
			}
			views := make([]repoView, 0, len(decls))
			for _, r := range decls {
				_, err := os.Stat(l.CodeRepo(r.Repo))
				views = append(views, repoView{
					Repo: r.Repo, About: r.About, Origin: r.Origin, Retired: r.Retired,
					Material: err == nil,
				})
			}
			if jsonFlag {
				return printJSON(cmd, views)
			}
			if len(views) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no repositories declared yet")
				return nil
			}
			for _, v := range views {
				where := "not here"
				if v.Material {
					where = "here"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-8s %s\n", v.Repo, where, v.About)
			}
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show <name>",
		Short: "One repository in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			r, ok, err := repos.Find(l, args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no repository named %q", args[0])
			}
			_, statErr := os.Stat(l.CodeRepo(r.Repo))
			v := repoView{Repo: r.Repo, About: r.About, Origin: r.Origin, Retired: r.Retired,
				Material: statErr == nil}
			if jsonFlag {
				return printJSON(cmd, v)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "repo   %s\n", v.Repo)
			if v.About != "" {
				fmt.Fprintf(out, "about  %s\n", v.About)
			}
			if v.Origin != "" {
				fmt.Fprintf(out, "origin %s\n", v.Origin)
			}
			fmt.Fprintf(out, "here   %t (%s)\n", v.Material, l.CodeRepo(v.Repo))
			if v.Retired != "" {
				fmt.Fprintf(out, "retired %s\n", v.Retired)
			}
			return nil
		},
	}

	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Retire a declaration; the repository's bytes are left alone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := repos.Remove(workspaceRoot(), args[0], time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(add, list, show, remove)
	return cmd
}
