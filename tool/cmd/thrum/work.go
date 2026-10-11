package main

import (
	"fmt"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
	"github.com/spf13/cobra"
)

// newWorkCmd is the way into a stretch of work: one command that ends with an actor standing
// in the right directory, with the environment that keeps its tools inside the workspace.
//
// It composes operations and never events. Declaring a label, opening a job and claiming one
// are three different things that are recorded separately, so this runs them in order and
// says what each one did — it does not roll them into one commit, because one commit is one
// event and a message cannot be true of three.
func newWorkCmd() *cobra.Command {
	var (
		jobID      string
		label      string
		purpose    string
		ephemeral  bool
		owner      string
		goal       string
		scope      string
		criteria   string
		briefPath  string
		ownedPaths string
		dependsOn  string
		keep       bool
		base       string
		summary    string
	)

	cmd := &cobra.Command{
		Use:   "work --job <id> [--label <label>]",
		Short: "Take up a job: the label, the job, the claim, and the directory",
		Long: "This is `open`, `claim` and `occupy` in the order they have to happen, and it ends by\n" +
			"printing the working directory and the environment line to run before anything else.\n" +
			"Each step that records something records it as its own event. A label that is already\n" +
			"declared is used as it is; a job that already exists is not reopened; a job somebody\n" +
			"else owns is worked without being claimed, and it says so.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			// A claim this site no longer holds a directory for, met below and taken over.
			staleClaim := ""

			// Where the job is, when it exists: a job has one home, and working on it from
			// a site that does not hold it is how two actors end up on one job.
			if workspace.JobExists(l, jobID) {
				brief, _, err := workspace.ReadBrief(l, jobID)
				if err != nil {
					return err
				}
				if brief.Status != format.StatusActive {
					return fmt.Errorf("%s is %s, and only a job that is still meant to happen is worked: `thrum worktree add --label <label> --detach <rev>` looks at its branch without working on it", jobID, brief.Status)
				}
				if home, err := resolveJob(l, jobID); err == nil && home.remote != "" {
					return fmt.Errorf("%s is materialised at %s: work on it where it is, or bring it over first", jobID, home.remote)
				}
				// Somebody else being on it is refused before anything is created, not
				// after: a materialised directory and a declared label left behind by a
				// refusal are work nobody asked for. But a claim is stale once this site no
				// longer holds the job — an actor that stopped without vacating leaves one
				// behind, and releasing the directory does not clear the note — and a stale
				// claim must not block the job for everybody. It is taken over below, and
				// said out loud when it is.
				if note, ok, err := occupancy.Get(l, jobID); err != nil {
					return err
				} else if ok && note.Occupant != actor {
					if _, _, err := worktreeOf(l, jobID); err == nil {
						return fmt.Errorf("%s is occupied by %s since %s: ask them for it rather than working on it beside them", jobID, note.Occupant, note.Since)
					}
					staleClaim = note.Occupant
				}
			}

			// The label, if it is not named: the directory that already holds the job is
			// the answer, and only when there is none is one derived from where the job
			// was last worked. Nothing is invented, and nothing here is state.
			if held, _, err := worktreeOf(l, jobID); err == nil {
				if label == "" {
					label = held
				}
			} else if label == "" {
				label = carriedLabel(l, jobID)
			}
			if label == "" {
				return fmt.Errorf("which area: name one with --label, and say what it is for with --purpose if it is new")
			}

			// The label, when the caller says it is new. Declaring is its own event, which
			// is why it is not folded into the open below.
			if purpose != "" {
				if err := declareLabel(l, label, purpose, ephemeral); err != nil {
					return err
				}
			}

			// The job, when it does not exist yet.
			if !workspace.JobExists(l, jobID) {
				body, err := briefBody(goal, scope, criteria, briefPath)
				if err != nil {
					return err
				}
				if owner == "" {
					owner = actor
				}
				id, err := jobs.Open(l, jobs.OpenOptions{
					Job:        jobID,
					Owner:      owner,
					DependsOn:  list(dependsOn),
					OwnedPaths: list(ownedPaths),
					Keep:       keep,
					Label:      label,
					Base:       base,
					Summary:    summary,
					Body:       body,
				})
				if err != nil {
					return err
				}
				fmt.Printf("opened %s in %s (%s)\n", jobID, label, short(id))
			}

			// The working directory, when this site has not materialised it.
			if _, _, err := worktreeOf(l, jobID); err != nil {
				if err := worktrees.Add(l, label, jobID, ""); err != nil {
					return err
				}
				fmt.Printf("materialised %s\n", label)
			}

			// The claim, when nobody has it. An owned job is not taken from its owner by
			// asking to work on it: that is a decision, and a message is how it is asked for.
			brief, _, err := workspace.ReadBrief(l, jobID)
			if err != nil {
				return err
			}
			switch {
			case brief.Owner == "":
				id, err := jobs.SetOwner(l, jobID, actor, "", "")
				if err != nil {
					return err
				}
				fmt.Printf("claimed by %s (%s)\n", actor, short(id))
			case brief.Owner != actor:
				fmt.Printf("%s is owned by %s: working on it without claiming it\n", jobID, brief.Owner)
			}

			// And say we are on it, here, which is where the directory now is.
			if note, ok, err := occupancy.Get(l, jobID); err != nil {
				return err
			} else if !ok || note.Occupant != actor {
				if staleClaim != "" {
					fmt.Printf("%s was occupied by %s, and this site held no directory for it: that claim is stale, and is taken over\n", jobID, staleClaim)
				}
				if err := occupancy.Occupy(l, jobID, actor, staleClaim != "", now()); err != nil {
					return err
				}
			}

			_, dir, err := worktreeOf(l, jobID)
			if err != nil {
				return err
			}
			fmt.Printf("work in %s\n", dir)
			fmt.Printf("eval \"$(thrum env --job %s)\"\n", jobID)
			return nil
		},
	}
	cmd.Flags().StringVar(&jobID, "job", "", "the job (required)")
	cmd.Flags().StringVar(&label, "label", "", "the working directory to work in; without it, the one the last handoff used")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what the label is for, when declaring it")
	cmd.Flags().BoolVar(&ephemeral, "ephemeral", false, "whether the label exists for a single use, when declaring it")
	cmd.Flags().StringVar(&owner, "owner", "", "who owns the job, when it is being opened")
	cmd.Flags().StringVar(&goal, "goal", "", "what the job is for, when it is being opened")
	cmd.Flags().StringVar(&scope, "scope", "", "what is out of scope, when it is being opened")
	cmd.Flags().StringVar(&criteria, "criteria", "", "how it will be known to be done, when it is being opened")
	cmd.Flags().StringVar(&briefPath, "brief", "", "read the brief's body from this file, or from - for standard input")
	cmd.Flags().StringVar(&ownedPaths, "owned-paths", "", "paths this job touches, relative to the code repository root")
	cmd.Flags().StringVar(&dependsOn, "depends-on", "", "job ids that must reach done first")
	cmd.Flags().BoolVar(&keep, "keep", false, "protect this job's payload from collection")
	cmd.Flags().StringVar(&base, "base", "", "the commit to branch from (default: the code repository's default branch)")
	cmd.Flags().StringVar(&summary, "summary", "", "the commit's summary")
	_ = cmd.MarkFlagRequired("job")
	return cmd
}

// carriedLabel is where the job was last worked, read from the newest handoff's worktrees.
//
// It is a derivation rather than a new piece of state: the handoff already records which
// labelled directories the work used, and an actor taking the job up again is almost always
// going back to the same area.
func carriedLabel(l workspace.Layout, job string) string {
	last, _, err := jobs.LastHandoff(l, job)
	if err != nil || last == nil || len(last.Worktrees) == 0 {
		return ""
	}
	return last.Worktrees[0]
}
