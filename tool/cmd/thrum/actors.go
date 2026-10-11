package main

import (
	"fmt"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/status"
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
				fmt.Printf("%-20s %-20s %-34s %s%s\n", e.Name, e.Site, roster.Said(e), e.LastSeen, job)
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
			entry, ok, silent := findActor(l, args[0])
			if !ok {
				return noSuchName(args[0], silent, "the question")
			}
			fmt.Printf("name       %s\n", entry.Name)
			fmt.Printf("site       %s\n", entry.Site)
			fmt.Printf("first seen %s\n", entry.FirstSeen)
			fmt.Printf("last seen  %s\n", entry.LastSeen)
			fmt.Printf("says       %s\n", roster.Said(entry))
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
		Short: "Who is on a job right now, according to the site that holds it",
		Long: "The file at var/occupy/ is a claim, and the site that holds the job is the one site\n" +
			"whose claim counts: a job is materialised at a site, and that site's tooling is\n" +
			"what refuses a second actor. Nothing on disk can say whether an actor is still\n" +
			"working, and this does not pretend otherwise.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			line, err := whoIsOn(layout(), jobID)
			if err != nil {
				return err
			}
			fmt.Println(line)
			return nil
		},
	}
	ask.Flags().StringVar(&jobID, "job", "", "the job (required)")
	_ = ask.MarkFlagRequired("job")

	online := &cobra.Command{
		Use:   "online",
		Short: "Say that this name is working here now",
		Long: "A session has a beginning and an end, and both are the actor's to state: this records\n" +
			"the beginning. Nothing expires, so an entry that says online is a statement made at a\n" +
			"time rather than a promise about now — and a session that ends without saying so leaves\n" +
			"exactly that behind.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := roster.SetState(layout(), actor, roster.StateOnline, now()); err != nil {
				return err
			}
			fmt.Printf("%s is online here\n", actor)
			return nil
		},
	}
	offline := &cobra.Command{
		Use:   "offline",
		Short: "Say that this name has stopped",
		Long: "The other end of a session, stated the same way. It takes nothing away: a name is not a\n" +
			"permission, no record is touched, and nothing is refused for having stopped.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := roster.SetState(layout(), actor, roster.StateOffline, now()); err != nil {
				return err
			}
			fmt.Printf("%s is offline\n", actor)
			return nil
		},
	}

	cmd.AddCommand(listCmd, show, online, offline, ask)
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
// findActor answers where a name is, keeping apart the two answers that are not the same: no
// site has seen it, and a site that may have seen it did not answer.
//
// A site learns its peers' rosters, so the usual answer is already here and costs nothing. The
// asking is what makes an answer possible for a name that has only ever been used at a site
// this one has not heard from since — and asking one site at a time, rather than merging every
// answer into one list, is what keeps *cannot be established* sayable: a merge cannot tell a
// peer that has nothing from a peer that said nothing.
//
// It returns the sites that could not be asked at all, which is the same thing as not
// answering: a site with no address entered here cannot say anything either.
func findActor(l workspace.Layout, name string) (roster.Entry, bool, []string) {
	if local, err := roster.Local(l); err == nil {
		if entry, ok := roster.Find(name, local); ok {
			return entry, true, nil
		}
	}

	peers := daemon.NewPeers(l)
	refs, err := peers.Refs()
	if err != nil {
		return roster.Entry{}, false, nil
	}
	var silent []string
	var lists [][]roster.Entry
	for _, ref := range refs {
		if ref.Name == peers.Site() || ref.Stale {
			continue
		}
		// Declared here, and no address entered for it, so it cannot be asked: the same as
		// not answering, and not the same as it never having seen the name.
		if ref.Reach == "" {
			silent = append(silent, ref.Name)
			continue
		}
		resp, err := peers.Ask(ref, daemon.Request{Verb: daemon.VerbRoster})
		if err != nil || !resp.OK {
			silent = append(silent, ref.Name)
			continue
		}
		var body struct {
			Actors []roster.Entry `json:"actors"`
		}
		if err := decode(resp, &body); err != nil {
			silent = append(silent, ref.Name)
			continue
		}
		lists = append(lists, body.Actors)
	}
	if entry, ok := roster.Find(name, roster.Merge(peers.Site(), lists...)); ok {
		return entry, true, silent
	}
	return roster.Entry{}, false, silent
}

// annotateOccupants adds what each occupant last said about itself.
//
// The report is about the workspace and does not read the roster: what the tooling knows about
// names is the tooling's, so the command joins the two rather than the report reaching for it.
func annotateOccupants(l workspace.Layout, report *status.Report) {
	entries, err := roster.Local(l)
	if err != nil {
		return
	}
	for i := range report.Occupancy {
		if entry, ok := roster.Find(report.Occupancy[i].Occupant, entries); ok {
			report.Occupancy[i].Presence = roster.Presence(entry)
		}
	}
}

// noSuchName is the refusal for a name every site that could be asked has never seen, and the
// different one for a name a silent site might have.
func noSuchName(name string, silent []string, what string) error {
	if len(silent) > 0 {
		return fmt.Errorf("%s did not answer, so where %q is cannot be established: %s failed, and nothing was queued — try again when that site is up", strings.Join(silent, ", "), name, what)
	}
	return fmt.Errorf("no site has seen an actor named %q: check the name, or let them act first", name)
}

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
