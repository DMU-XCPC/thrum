package main

import (
	"fmt"

	"github.com/multiagentworkspace/ws/tool/internal/daemon"
	"github.com/multiagentworkspace/ws/tool/internal/occupancy"
	"github.com/spf13/cobra"
)

// newActorCmd is the running world: who exists, who is alive, and who is on what.
//
// The registry is not a file in the workspace, because the workspace records names and
// this records processes. Starting and waking an actor are the daemon's, at
// `ws daemon start` and `ws daemon wake`.
func newActorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actor",
		Short: "Ask about the running world: who is alive, and who is on a job",
		Long: "A site answers for itself, and every site is asked in parallel, so this works\n" +
			"precisely when something is unreachable: the ones that answer are the ones that are\n" +
			"there.",
	}

	registry := &cobra.Command{
		Use:   "registry",
		Short: "What this site is running, and how each was started",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, running, err := daemon.Local(layout(), daemon.Request{Verb: daemon.VerbStatus})
			if err != nil {
				return err
			}
			if !running {
				fmt.Println("no daemon is running here, so nothing is being supervised")
				return nil
			}
			var data daemon.StatusData
			if err := decode(resp, &data); err != nil {
				return err
			}
			if len(data.Actors) == 0 {
				fmt.Println("nothing is running here")
				return nil
			}
			for _, a := range data.Actors {
				state := "alive"
				if !a.Alive() {
					state = "gone"
				}
				fmt.Printf("%-10s %-28s pid %-7d %-6s %v\n", a.Actor, a.Job, a.PID, state, a.Command)
			}
			return nil
		},
	}

	var jobID string
	ask := &cobra.Command{
		Use:   "ask --job <id>",
		Short: "Who is on a job right now, here and at every site that answers",
		Long: "The file at var/occupy/ is a claim; this is the answer, and it comes from the\n" +
			"tooling because the tooling started the process and knows whether it is still alive.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			answered := false

			// This site first, through the daemon when there is one: it is what
			// knows the running world rather than only what was written down.
			if resp, running, err := daemon.Local(l, daemon.Request{
				Verb: daemon.VerbAsk,
				Args: daemon.Args(map[string]string{"job": jobID}),
			}); err == nil && running && len(resp.Data) > 0 {
				var here map[string]string
				if err := decode(resp, &here); err == nil && here["occupant"] != "" {
					fmt.Printf("%-20s %-14s since %s\n", here["site"], here["occupant"], here["since"])
					answered = true
				}
			} else if note, ok, err := occupancy.Get(l, jobID); err == nil && ok {
				// Without a daemon the note is all there is: a claim, and the honest
				// answer is that it is one.
				fmt.Printf("%-20s %-14s since %s  (a claim, not a live answer)\n",
					daemon.Site(l), note.Occupant, note.Since)
				answered = true
			}

			peers := daemon.NewPeers(l)
			for _, peer := range peers.Report() {
				if !peer.Reachable || peer.Name == peers.Site() {
					continue
				}
				ref, err := peers.Lookup(peer.Name)
				if err != nil {
					continue
				}
				resp, err := peers.Ask(ref, daemon.Request{
					Verb: daemon.VerbAsk,
					Args: daemon.Args(map[string]string{"job": jobID}),
				})
				if err != nil || len(resp.Data) == 0 {
					continue
				}
				var there map[string]string
				if err := decode(resp, &there); err != nil || there["occupant"] == "" {
					continue
				}
				fmt.Printf("%-20s %-14s since %s\n", there["site"], there["occupant"], there["since"])
				answered = true
			}

			if !answered {
				fmt.Printf("nobody, as far as any site that answered knows\n")
			}
			return nil
		},
	}
	ask.Flags().StringVar(&jobID, "job", "", "the job (required)")
	_ = ask.MarkFlagRequired("job")

	cmd.AddCommand(registry, ask)
	return cmd
}
