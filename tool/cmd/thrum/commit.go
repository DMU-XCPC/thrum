package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/commit"
	"github.com/spf13/cobra"
)

// newCommitCmd is the way a hand-written document gets committed.
//
// The design says briefs, handoffs, topics and conventions are written by actors in an
// editor, and that every commit goes through the tooling — which is where the
// coordination lock lives. Without this, an actor that edits a topic has no way to
// commit it that produces the same message form as everything else.
//
// The paths are required rather than assumed: one event per commit is the design's own
// rule, and staging everything in sight is how one actor's commit picks up another's
// work in progress.
func newCommitCmd() *cobra.Command {
	var summary string

	cmd := &cobra.Command{
		Use:   "commit --summary <text> -- <path>...",
		Short: "Commit one coordination event, for a document written by hand",
		Long: "The event is inferred from the paths, so that a topic, a convention, a brief's body\n" +
			"and an archived document all get the same message form as an operation would give\n" +
			"them. Every path has to belong to the same event, because one commit is one event.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()

			var kind commit.Kind
			var subject string
			paths := make([]string, 0, len(args))
			for _, arg := range args {
				rel, err := filepath.Rel(l.Root, arg)
				if err != nil || strings.HasPrefix(rel, "..") {
					rel = arg
				}
				where, what, err := commit.Infer(filepath.ToSlash(rel))
				if err != nil {
					return err
				}
				if kind == "" {
					kind, subject = where, what
				} else if kind != where || subject != what {
					return fmt.Errorf("%s is a different event from %s: one commit is one event, so commit them separately",
						rel, paths[0])
				}
				paths = append(paths, filepath.ToSlash(rel))
			}

			id, err := commit.Event(l, kind, subject, summary, paths...)
			if err != nil {
				return err
			}
			fmt.Printf("committed %s (%s)\n", strings.TrimSpace(string(kind)+" "+subject), short(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&summary, "summary", "", "what changed, in plain language (required)")
	_ = cmd.MarkFlagRequired("summary")
	return cmd
}
