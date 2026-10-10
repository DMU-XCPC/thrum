package main

import (
	"fmt"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/spf13/cobra"
)

// newActorCmd is what the tooling knows about names.
//
// An actor is a name, and becoming one takes nothing but using one: there is no join,
// no registration step, and no list to be added to. What the tooling knows is who it
// has seen, when it last heard from them, and where — never used to refuse anything,
// because a name is not a permission.
func newActorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actor",
		Short: "Say which names exist, where each was last heard from, and what they are on",
		Long: "The roster is the tooling's, not a file in the workspace: the workspace records\n" +
			"names and claims, and this is what the tooling has seen. Every site answers for\n" +
			"its own, and they are asked in parallel.",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Every name this site and its peers have seen",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			entries, reports := rosterEverywhere(l)
			if len(entries) == 0 {
				fmt.Println("no names yet: an actor exists as soon as it does something")
				return nil
			}
			for _, e := range entries {
				job := ""
				if e.Job != "" {
					job = "  on " + e.Job
				}
				fmt.Printf("%-20s %-20s %s%s\n", e.Name, e.Site, e.LastSeen, job)
			}
			for _, p := range reports {
				if !p.Reachable && p.Note != "" {
					fmt.Printf("(%s did not answer: %s)\n", p.Name, p.Note)
				}
			}
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show <name>",
		Short: "Everything the tooling knows about one name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			entries, _ := rosterEverywhere(l)
			entry, ok := roster.Find(args[0], entries)
			if !ok {
				return fmt.Errorf("no actor named %q here or at any site that answered", args[0])
			}
			fmt.Printf("name       %s\n", entry.Name)
			fmt.Printf("site       %s\n", entry.Site)
			fmt.Printf("first seen %s\n", entry.FirstSeen)
			fmt.Printf("last seen  %s\n", entry.LastSeen)
			if entry.Job != "" {
				fmt.Printf("on         %s\n", entry.Job)
			}
			// What it says it is on is a hint. The claim is a file, and it is the only
			// answer to who is on a job right now.
			if note, ok, err := occupancyOf(l, entry.Job); err == nil && ok {
				fmt.Printf("claim      %s since %s\n", note.Occupant, note.Since)
			}
			return nil
		},
	}

	var jobID string
	ask := &cobra.Command{
		Use:   "ask --job <id>",
		Short: "Who is on a job right now, here and at every site that answers",
		Long: "The file at var/occupy/ is a claim, and this reports the claim: a name that stayed\n" +
			"behind says somebody was working, and nothing on disk can say whether they still are.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			answered := false

			if note, ok, err := occupancyOf(l, jobID); err != nil {
				return err
			} else if ok {
				fmt.Printf("%-20s %-14s since %s\n", roster.Site(l), note.Occupant, note.Since)
				answered = true
			}

			peers := daemon.NewPeers(l)
			for _, report := range peers.Report() {
				if !report.Reachable || report.Name == peers.Site() {
					continue
				}
				ref, err := peers.Lookup(report.Name)
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
				fmt.Println("nobody, as far as any site that answered knows")
			}
			return nil
		},
	}
	ask.Flags().StringVar(&jobID, "job", "", "the job (required)")
	_ = ask.MarkFlagRequired("job")

	cmd.AddCommand(listCmd, show, ask)
	return cmd
}

// newEnvCmd prints what an actor needs so that nothing it runs writes outside the
// workspace.
//
// It is the design's "Where your tools write" made runnable: the base variables name
// paths inside the workspace, so they read the same on every site, and whoever starts
// the work supplies them.
func newEnvCmd() *cobra.Command {
	var jobID string

	cmd := &cobra.Command{
		Use:   "env --job <id>",
		Short: "Print the environment variables that keep a job's tools inside the workspace",
		Long: "eval \"$(thrum env --job <id>)\" before running anything for a job. A tool run\n" +
			"without this writes outside the workspace, where nothing collects or reviews what\n" +
			"it leaves.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			if jobID != "" {
				if _, _, err := workspace.ReadBrief(l, jobID); err != nil {
					return err
				}
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "export THRUM_ROOT=%s\n", l.Root)
			fmt.Fprintf(out, "export THRUM_ACTOR=%s\n", actor)
			if jobID != "" {
				fmt.Fprintf(out, "export THRUM_JOB=%s\n", jobID)
				fmt.Fprintf(out, "export TMPDIR=%s\n", l.JobSub(jobID, "tmp"))
				fmt.Fprintf(out, "export XDG_STATE_HOME=%s\n", l.JobSub(jobID, "state"))
			}
			fmt.Fprintf(out, "export XDG_CACHE_HOME=%s\n", l.Cache())
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job whose payload the variables should point at")
	return cmd
}

// rosterEverywhere merges what every site that answers says, newest hearing wins.
func rosterEverywhere(l workspace.Layout) ([]roster.Entry, []daemon.PeerReport) {
	local, err := roster.Local(l)
	if err != nil {
		local = nil
	}

	peers := daemon.NewPeers(l)
	reports := peers.Report()
	lists := [][]roster.Entry{local}
	for _, report := range reports {
		if !report.Reachable || report.Name == peers.Site() {
			continue
		}
		ref, err := peers.Lookup(report.Name)
		if err != nil {
			continue
		}
		resp, err := peers.Ask(ref, daemon.Request{Verb: daemon.VerbRoster})
		if err != nil || !resp.OK {
			continue
		}
		var body struct {
			Actors []roster.Entry `json:"actors"`
		}
		if err := decode(resp, &body); err != nil {
			continue
		}
		lists = append(lists, body.Actors)
	}
	return roster.Merge(peers.Site(), lists...), reports
}

// occupancyOf reads the claim for a job, locally. It is a claim, and this does not
// pretend otherwise: the file is the only answer to who is on a job right now.
func occupancyOf(l workspace.Layout, job string) (format.Occupy, bool, error) {
	if job == "" {
		return format.Occupy{}, false, nil
	}
	return occupancy.Get(l, job)
}
