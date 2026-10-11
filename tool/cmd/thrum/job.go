package main

import (
	"fmt"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/leases"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/status"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/spf13/cobra"
)

// jobView is everything an actor needs about one job before it starts, in one place.
//
// It is derived from what is already there — the brief, the newest handoff, the working
// directory, the occupancy note, the leases — because an actor is given a workspace root and
// a job id and derives the rest. This only does the deriving in one step rather than six.
type jobView struct {
	Job         string   `json:"job"`
	Status      string   `json:"status"`
	Owner       string   `json:"owner,omitempty"`
	Goal        string   `json:"goal,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Criteria    string   `json:"criteria,omitempty"`
	Label       string   `json:"label,omitempty"`
	Worktree    string   `json:"worktree,omitempty"`
	OnIt        string   `json:"on_it,omitempty"`
	Occupant    string   `json:"occupant,omitempty"`
	OccupantAt  string   `json:"occupant_since,omitempty"`
	Presence    string   `json:"occupant_presence,omitempty"`
	Next        string   `json:"next,omitempty"`
	Agent       string   `json:"last_agent,omitempty"`
	LastHandoff string   `json:"last_handoff,omitempty"`
	Leases      []string `json:"leases,omitempty"`
}

// newJobCmd is what a person asks about work: the board, one job in full, and who is on it.
//
// Everything here is derived — from the briefs, the handoffs, the working directories, the
// claims and the roster — so there is nothing to keep in step, and each subcommand is one of
// those questions rather than a new place a fact lives.
func newJobCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "The board, one job in full, and who is on it",
		Long: "`job list` is every job with what the last handoff said about it; `job show <id>` is one\n" +
			"job in full; `job open <id>` starts one and `job close <id>` ends it; `job handoff <id>`\n" +
			"records a stretch of work; `job claim <id>` and `job relinquish <id>` are ownership; and\n" +
			"`job occupy <id>` and `job vacate <id>` are the claim that somebody is on it, written\n" +
			"where the job is so that one site decides.",
	}
	cmd.AddCommand(newJobListCmd(), newJobShowCmd(), newJobOccupyCmd(), newJobVacateCmd(),
		newJobClaimCmd(), newJobRelinquishCmd(), newOpenCmd(), newHandoffCmd(), newCloseCmd())
	return cmd
}

// newJobListCmd is the board: the brief says whether a job is still meant to happen, and the
// newest handoff says whether the last stretch got anywhere.
func newJobListCmd() *cobra.Command {
	var (
		owner    string
		lastSaid string
		asJSON   bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every job with what the last handoff said about it",
		Long: "The same board `thrum status` prints, on its own. --owner and --status narrow it, and\n" +
			"--json is the same answer for whatever asked.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			report, err := status.Gather(l)
			if err != nil {
				return err
			}
			jobs := make([]status.Job, 0, len(report.Jobs))
			for _, job := range report.Jobs {
				if owner != "" && job.Owner != owner {
					continue
				}
				if lastSaid != "" && job.State != lastSaid {
					continue
				}
				jobs = append(jobs, job)
			}
			if asJSON {
				return printJSON(cmd, jobs)
			}
			if len(jobs) == 0 {
				fmt.Println("no job matches")
				return nil
			}
			for _, job := range jobs {
				where := "archived"
				if job.Live {
					where = "doc/task/"
				}
				who := job.Owner
				if who == "" {
					who = "unowned"
				}
				next := ""
				if job.Next != "" {
					next = "  next: " + firstLine(job.Next)
				}
				fmt.Printf("%-28s %-14s %-14s %s%s\n", job.ID, job.State, who, where, next)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "only jobs this name owns")
	cmd.Flags().StringVar(&lastSaid, "status", "", "only jobs the board describes this way, such as blocked or not started")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the same answer as JSON")
	return cmd
}

// newJobShowCmd is one job in full, for a person or for whatever handed the actor the job.
func newJobShowCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Everything about one job: what it is for, where it is, and who is on it",
		Long: "The brief's goal, scope and acceptance criteria; the newest handoff's baton; the\n" +
			"working directory here, if this site has one; who is on it according to the site that\n" +
			"holds it; and the units it holds. --json is the same answer for whatever asked.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			l := layout()
			// ReadBrief answers with the path it found the brief at, because a job that
			// has closed lives in the archive; the body is read from there.
			brief, path, err := workspace.ReadBrief(l, jobID)
			if err != nil {
				return err
			}
			_, body, err := format.LoadDocument(path)
			if err != nil {
				return err
			}
			sections := briefSections(string(body))

			v := jobView{
				Job:      jobID,
				Status:   brief.Status,
				Owner:    brief.Owner,
				Goal:     sections["Goal"],
				Scope:    sections["Scope"],
				Criteria: sections["Acceptance criteria"],
			}
			if label, dir, err := worktreeOf(l, jobID); err == nil {
				v.Label, v.Worktree = label, dir
			}
			if note, ok, err := occupancy.Get(l, jobID); err != nil {
				return err
			} else if ok {
				v.Occupant, v.OccupantAt = note.Occupant, note.Since
				if entries, err := roster.Local(l); err == nil {
					if entry, found := roster.Find(note.Occupant, entries); found {
						v.Presence = roster.Presence(entry)
					}
				}
			}
			if last, name, err := jobs.LastHandoff(l, jobID); err == nil && last != nil {
				v.Next, v.Agent, v.LastHandoff = last.Next, last.Agent, name
			}
			held, err := leases.Held(l)
			if err != nil {
				return err
			}
			for _, lease := range held {
				if lease.Job == jobID {
					v.Leases = append(v.Leases, lease.Resource+"."+lease.Unit)
				}
			}

			// Who is on it is the claim, and a claim counts where the job is: a site that
			// does not hold the job cannot answer for it, so the question goes there, and
			// "cannot be established" is said rather than guessed.
			if line, err := whoIsOn(l, jobID); err == nil {
				v.OnIt = line
			} else {
				v.OnIt = "cannot be established: " + err.Error()
			}

			if asJSON {
				return printJSON(cmd, v)
			}
			fmt.Printf("%s  %s", v.Job, v.Status)
			if v.Owner != "" {
				fmt.Printf("  owned by %s", v.Owner)
			}
			fmt.Println()
			if v.Goal != "" {
				fmt.Printf("goal      %s\n", firstLine(v.Goal))
			}
			if v.Scope != "" {
				fmt.Printf("scope     %s\n", firstLine(v.Scope))
			}
			if v.Criteria != "" {
				fmt.Printf("done when %s\n", firstLine(v.Criteria))
			}
			if v.Worktree != "" {
				fmt.Printf("work in   %s\n", v.Worktree)
			} else {
				fmt.Println("work in   nowhere here: `thrum work --job " + v.Job + "` materialises a working directory")
			}
			if v.Occupant != "" {
				fmt.Printf("occupied  %s since %s\n", v.Occupant, v.OccupantAt)
				if v.Presence != "" {
					fmt.Printf("          %s\n", v.Presence)
				}
			}
			if v.OnIt != "" {
				fmt.Printf("on it     %s\n", v.OnIt)
			}
			if v.Next != "" {
				fmt.Printf("next      %s\n", firstLine(v.Next))
				fmt.Printf("          (from %s, %s)\n", v.LastHandoff, v.Agent)
			}
			if len(v.Leases) > 0 {
				fmt.Printf("holds     %s\n", strings.Join(v.Leases, ", "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the same answer as JSON")
	return cmd
}

// newJobOccupyCmd records that an actor is on a job.
func newJobOccupyCmd() *cobra.Command {
	var takeover bool

	cmd := &cobra.Command{
		Use:   "occupy <id>",
		Short: "Record that an actor is on a job",
		Long: "The note describes a running world rather than a decision: it is a claim and not a lock,\n" +
			"a name stays behind after its process is gone, and the roster says what that name last\n" +
			"said about itself.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			l := layout()
			home, err := resolveJob(l, jobID)
			if err != nil {
				return err
			}
			// The claim is written where the job is, so that the one site that can answer
			// for it is the one site that decides. Anywhere else it would be an opinion,
			// and two sites' opinions are how one job gets two actors.
			if home.remote == "" {
				if err := occupancy.Occupy(l, jobID, actor, takeover, now()); err != nil {
					return err
				}
				fmt.Printf("%s is on %s\n", actor, jobID)
				return nil
			}
			if _, err := claimAt(l, home, daemon.Request{
				Verb: daemon.VerbOccupy,
				Args: daemon.Args(map[string]any{"job": jobID, "actor": actor, "takeover": takeover}),
			}); err != nil {
				return err
			}
			fmt.Printf("%s is on %s at %s\n", actor, jobID, home.remote)
			return nil
		},
	}
	cmd.Flags().BoolVar(&takeover, "take-over", false, "take it over even though it names somebody else")
	return cmd
}

// newJobVacateCmd clears a claim, or every claim that has gone stale.
func newJobVacateCmd() *cobra.Command {
	var stale bool

	cmd := &cobra.Command{
		Use:   "vacate <id>",
		Short: "Clear the note for a job, or every note that has gone stale",
		Long: "A claim is cleared where it is: this site if it holds the job, and the site that holds it\n" +
			"now otherwise. `--stale` clears instead the notes for jobs that have left `doc/task/` and\n" +
			"for jobs this site no longer holds, which is what a claim whose process is gone leaves\n" +
			"behind.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			if stale {
				swept, err := occupancy.Sweep(l)
				if err != nil {
					return err
				}
				for _, job := range swept {
					fmt.Printf("swept %s\n", job)
				}
				if len(swept) == 0 {
					fmt.Println("nothing stale")
				}
				return nil
			}
			if len(args) == 0 {
				return fmt.Errorf("which job: `thrum job vacate <id>`, or `thrum job vacate --stale`")
			}
			jobID := args[0]
			// A claim is cleared where it is. When the job has moved away from the site
			// that made it, --stale is what clears it; here, clearing means either this
			// site or the site that holds the job now.
			was, err := occupancy.Vacate(l, jobID)
			if err != nil {
				return err
			}
			if was.Occupant != "" {
				fmt.Printf("%s is no longer occupied (it was %s)\n", jobID, was.Occupant)
				return nil
			}
			home, err := resolveJob(l, jobID)
			if err != nil {
				fmt.Printf("%s was not occupied here\n", jobID)
				return nil
			}
			if home.remote == "" || home.claim.Occupant == "" {
				fmt.Printf("%s was not occupied\n", jobID)
				return nil
			}
			if _, err := claimAt(l, home, daemon.Request{
				Verb: daemon.VerbVacate,
				Args: daemon.Args(map[string]string{"job": jobID}),
			}); err != nil {
				return err
			}
			fmt.Printf("%s is no longer occupied at %s (it was %s)\n", jobID, home.remote, home.claim.Occupant)
			return nil
		},
	}
	cmd.Flags().BoolVar(&stale, "stale", false, "clear every note that has gone stale instead")
	return cmd
}

// briefSections pulls the `## Name` blocks out of a brief's body, which is where what a job
// is for, what is out of it, and how it will be judged live.
func briefSections(body string) map[string]string {
	out := map[string]string{}
	name := ""
	var lines []string
	flush := func() {
		if name != "" {
			out[name] = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			name, lines = strings.TrimSpace(strings.TrimPrefix(line, "## ")), nil
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return out
}

// firstLine is the first non-empty line, because a section may open with a blank one.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// newJobClaimCmd is ownership: a job with no owner is one nobody is responsible for.
func newJobClaimCmd() *cobra.Command {
	var owner string

	cmd := &cobra.Command{
		Use:   "claim <id>",
		Short: "Take responsibility for a job, or hand it to somebody else",
		Long: "A job with no owner is one nobody is responsible for. Whoever takes it up writes\n" +
			"itself in without asking, because there is nobody left to ask; an owner may also\n" +
			"reassign a job it holds.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			who := owner
			if who == "" {
				who = actor
			}
			id, err := jobs.SetOwner(layout(), jobID, who, actor, "")
			if err != nil {
				return err
			}
			if id == "" {
				fmt.Printf("%s is already owned by %s\n", jobID, who)
				return nil
			}
			fmt.Printf("%s is now owned by %s (%s)\n", jobID, who, short(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "who takes it (default: this actor)")
	return cmd
}

// newJobRelinquishCmd gives ownership up, which is what makes a job claimable.
func newJobRelinquishCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "relinquish <id>",
		Short: "Give up ownership, leaving the job claimable",
		Long: "Giving ownership up is what makes a job claimable, and it is the owner's act. When\n" +
			"the owner is an actor who no longer exists, a person gives it up on their behalf,\n" +
			"since the job would otherwise be stuck forever.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			id, err := jobs.SetOwner(layout(), jobID, "", actor, "")
			if err != nil {
				return err
			}
			if id == "" {
				fmt.Printf("%s was already unowned\n", jobID)
				return nil
			}
			fmt.Printf("%s is now unowned (%s)\n", jobID, short(id))
			return nil
		},
	}
	return cmd
}
