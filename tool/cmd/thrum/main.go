package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// The version is one string, and a pair of sites compares nothing else before they
// talk.
const version = "0.1.0"

var (
	rootFlag  string
	actorFlag string
	jsonFlag  bool
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "thrum:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "thrum",
		Short:         "A coordination fabric for a multi-actor workspace",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&rootFlag, "root", "", "the workspace (default $THRUM_ROOT, then the current directory)")
	root.PersistentFlags().StringVar(&actorFlag, "actor", "", "the actor name (default $THRUM_ACTOR, then $USER)")
	root.PersistentFlags().BoolVar(&jsonFlag, "json", false, "print the answer as machine-readable data")
	root.AddCommand(newCreateCmd(), newVersionCmd(), newActorCmd(), newRepoCmd(),
		newJobCmd(), newWorktreeCmd(), newWorkCmd(), newWhereCmd(), newEnvCmd())
	return root
}

// workspaceRoot resolves the workspace every command acts on.
func workspaceRoot() layout.Layout {
	root := rootFlag
	if root == "" {
		root = os.Getenv("THRUM_ROOT")
	}
	if root == "" {
		root, _ = os.Getwd()
	}
	return layout.New(root)
}

// actorName resolves the name a command acts as. It is an address, not a claim.
func actorName() string {
	if actorFlag != "" {
		return actorFlag
	}
	if a := os.Getenv("THRUM_ACTOR"); a != "" {
		return a
	}
	return os.Getenv("USER")
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print what this is",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "thrum %s\n", version)
			return nil
		},
	}
}

func newCreateCmd() *cobra.Command {
	var site string
	var code []string

	cmd := &cobra.Command{
		Use:   "create <dir> --site <name> [--code <name>=<path>]...",
		Short: "Create a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if site == "" {
				return fmt.Errorf("--site is required")
			}
			sources, err := parseCode(code)
			if err != nil {
				return err
			}
			if err := workspace.Create(workspace.CreateOptions{
				Root: args[0],
				Site: site,
				Code: sources,
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&site, "site", "", "the name of this site")
	cmd.Flags().StringArrayVar(&code, "code", nil, "a code repository, as <name>=<path> (repeatable)")
	return cmd
}

// parseCode reads --code name=path. The name is explicit because it is the name a
// brief's repo field will carry, and a name derived from a path is a name nobody
// chose.
func parseCode(values []string) ([]workspace.CodeSource, error) {
	out := make([]workspace.CodeSource, 0, len(values))
	for _, v := range values {
		name, path, ok := cut(v, '=')
		if !ok || name == "" || path == "" {
			return nil, fmt.Errorf("--code %q is not <name>=<path>", v)
		}
		out = append(out, workspace.CodeSource{Name: name, Path: path})
	}
	return out, nil
}

func cut(s string, sep byte) (before, after string, found bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
