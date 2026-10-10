package main

import (
	"fmt"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/spf13/cobra"
)

// start runs an actor on a job, and stop ends it. These are the two things that need
// something alive, and they say what a person is doing rather than which process is
// doing it: the daemon that supervises is started here and never named.
func newStartCmd() *cobra.Command {
	var (
		jobID   string
		actorID string
	)

	cmd := &cobra.Command{
		Use:   "start --job <id> [--actor <name>] -- <command>...",
		Short: "Run an actor on a job, and supervise it",
		Long: "An actor is a harness this site has installed, not something the workspace ships:\n" +
			"the command is a fact about the machine. What it is started with is the design's —\n" +
			"the workspace root, the job id, and the variables that point a job's tools at its\n" +
			"payload rather than at their own temporary and cache directories.\n\n" +
			"Starting one takes the job's claim, so a second actor on a job somebody is already\n" +
			"working is refused.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			who := actorID
			if who == "" {
				who = actor
			}
			resp, err := needDaemon(layout(), daemon.Request{
				Verb: daemon.VerbStart,
				Args: daemon.Args(map[string]any{"job": jobID, "actor": who, "command": args}),
			})
			if err != nil {
				return err
			}
			var started daemon.Actor
			if err := decode(resp, &started); err != nil {
				return err
			}
			fmt.Printf("started %s on %s as pid %d\n", started.Actor, started.Job, started.PID)
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job to work on (required)")
	cmd.Flags().StringVar(&actorID, "actor", "", "who this actor is (default: this actor)")
	_ = cmd.MarkFlagRequired("job")
	return cmd
}

func newStopCmd() *cobra.Command {
	var jobID string

	cmd := &cobra.Command{
		Use:   "stop --job <id>",
		Short: "End the actor on a job, and clear the claim it held",
		Long: "Ending a stretch is what stops a job being somebody's: the claim is cleared with\n" +
			"it, because a note left behind would say somebody was still working.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			if _, running, err := askDaemon(l, daemon.Request{
				Verb: daemon.VerbStop,
				Args: daemon.Args(map[string]string{"job": jobID}),
			}); err != nil {
				return err
			} else if !running {
				// Nothing here is supervising anything — but the claim is a file, and
				// it still says somebody is working. Clearing it is the whole of
				// stopping when there is no daemon to stop.
				was, err := occupancy.Vacate(l, jobID)
				if err != nil {
					return err
				}
				if was.Occupant == "" {
					fmt.Printf("nothing was running %s here\n", jobID)
					return nil
				}
			}
			fmt.Printf("stopped %s\n", jobID)
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job (required)")
	_ = cmd.MarkFlagRequired("job")
	return cmd
}
