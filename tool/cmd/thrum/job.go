package main

import (
	"fmt"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/leases"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
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
	Occupant    string   `json:"occupant,omitempty"`
	OccupantAt  string   `json:"occupant_since,omitempty"`
	Next        string   `json:"next,omitempty"`
	Agent       string   `json:"last_agent,omitempty"`
	LastHandoff string   `json:"last_handoff,omitempty"`
	Leases      []string `json:"leases,omitempty"`
}

// newJobCmd prints one job's picture, for a person or for whatever handed the actor the job.
func newJobCmd() *cobra.Command {
	var (
		jobID  string
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "job --job <id>",
		Short: "Print everything about one job: what it is for, where it is, and who is on it",
		Long: "The brief's goal, scope and acceptance criteria; the newest handoff's baton; the\n" +
			"working directory here, if this site has one; who says they are on it; and the units\n" +
			"it holds. --json is the same answer for whatever asked.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
	cmd.Flags().StringVar(&jobID, "job", "", "the job (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the same answer as JSON")
	_ = cmd.MarkFlagRequired("job")
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
