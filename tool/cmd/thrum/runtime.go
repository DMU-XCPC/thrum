package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/commit"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/leases"
	"github.com/multiagentworkspace/thrum/tool/internal/occupancy"
	"github.com/multiagentworkspace/thrum/tool/internal/payload"
	"github.com/multiagentworkspace/thrum/tool/internal/resources"
	"github.com/multiagentworkspace/thrum/tool/internal/status"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/spf13/cobra"
)

// newOccupancyCmd is the site-local note about who is on a job here, which the tooling
// writes and clears rather than an actor editing it.
func newOccupancyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "occupancy",
		Short: "Say who is on a job at this site, and since when",
		Long: "The note describes a running world rather than a decision, so it is written and\n" +
			"cleared here and read through here. It is a claim and not a lock: a name stays\n" +
			"behind after its process is gone, and nothing on disk can tell a working actor\n" +
			"from a dead one.",
	}

	var (
		jobID    string
		takeover bool
	)

	occupy := &cobra.Command{
		Use:   "occupy",
		Short: "Record that an actor is on a job",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := occupancy.Occupy(layout(), jobID, actor, takeover, time.Now().UTC()); err != nil {
				return err
			}
			fmt.Printf("%s is on %s\n", actor, jobID)
			return nil
		},
	}
	occupy.Flags().StringVar(&jobID, "job", "", "the job id (required)")
	occupy.Flags().BoolVar(&takeover, "take-over", false, "take it over even though it names somebody else")
	_ = occupy.MarkFlagRequired("job")

	vacate := &cobra.Command{
		Use:   "vacate",
		Short: "Clear the note for a job",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			was, err := occupancy.Vacate(layout(), jobID)
			if err != nil {
				return err
			}
			if was.Occupant == "" {
				fmt.Printf("%s was not occupied\n", jobID)
			} else {
				fmt.Printf("%s is no longer occupied (it was %s)\n", jobID, was.Occupant)
			}
			return nil
		},
	}
	vacate.Flags().StringVar(&jobID, "job", "", "the job id (required)")
	_ = vacate.MarkFlagRequired("job")

	sweep := &cobra.Command{
		Use:   "sweep",
		Short: "Clear the notes of jobs that have left doc/task/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			swept, err := occupancy.Sweep(layout())
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
		},
	}

	show := &cobra.Command{
		Use:   "show",
		Short: "Print the notes this site holds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := status.Gather(layout())
			if err != nil {
				return err
			}
			for _, o := range report.Occupancy {
				fmt.Printf("%-28s %-14s since %s\n", o.Job, o.Occupant, o.Since)
			}
			return nil
		},
	}

	cmd.AddCommand(occupy, vacate, sweep, show)
	return cmd
}

// newResourceCmd declares and removes the things a workspace shares. Only the tooling
// writes these files, because a registry is the one file that holds several entries.
func newResourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resource",
		Short: "Declare or remove the resources a workspace shares",
		Long: "A declaration is a recipe rather than an inventory: a declared entry with no\n" +
			"directory is simply not materialised here, and that is normal. The other direction\n" +
			"is the error.",
	}

	var (
		kind      string
		name      string
		purpose   string
		capacity  int
		ephemeral bool
		fields    []string
		summary   string
	)

	declare := &cobra.Command{
		Use:   "declare",
		Short: "Add an entry to a kind's registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]string{}
			for _, f := range fields {
				k, v, ok := strings.Cut(f, "=")
				if !ok {
					return fmt.Errorf("--field takes key=value, got %q", f)
				}
				extra[k] = v
			}
			var eph *bool
			if cmd.Flags().Changed("ephemeral") {
				eph = &ephemeral
			}
			if err := resources.Declare(layout(), kind, resources.Entry{
				Name:      name,
				Purpose:   purpose,
				Capacity:  capacity,
				Ephemeral: eph,
				Extra:     extra,
			}); err != nil {
				return err
			}
			if summary == "" {
				summary = fmt.Sprintf("declared %s", name)
			}
			id, err := commit.Event(layout(), commit.KindResource, "", summary,
				relToRoot(layout(), layout().ResourceKind(kind)))
			if err != nil {
				return err
			}
			fmt.Printf("declared %s %s (%s)\n", kind, name, short(id))
			return nil
		},
	}
	declare.Flags().StringVar(&kind, "kind", format.WorktreeKind, "the kind of resource")
	declare.Flags().StringVar(&name, "name", "", "the resource's name (required)")
	declare.Flags().StringVar(&purpose, "purpose", "", "what it is for (required)")
	declare.Flags().IntVar(&capacity, "capacity", 0, "how many holders at once; omitted means one, which means exclusive")
	declare.Flags().BoolVar(&ephemeral, "ephemeral", false, "for worktrees: exists for a single use")
	declare.Flags().StringArrayVar(&fields, "field", nil, "a field of your own, key=value; it belongs to the reader")
	declare.Flags().StringVar(&summary, "summary", "", "the commit's summary")
	_ = declare.MarkFlagRequired("name")
	_ = declare.MarkFlagRequired("purpose")

	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove an entry, refusing while something is using it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := layout().ResourceKind(kind)
			if err := resources.Remove(layout(), kind, name); err != nil {
				return err
			}
			if summary == "" {
				summary = fmt.Sprintf("removed %s", name)
			}
			id, err := commit.Event(layout(), commit.KindResource, "", summary, relToRoot(layout(), path))
			if err != nil {
				return err
			}
			fmt.Printf("removed %s %s (%s)\n", kind, name, short(id))
			return nil
		},
	}
	remove.Flags().StringVar(&kind, "kind", format.WorktreeKind, "the kind of resource")
	remove.Flags().StringVar(&name, "name", "", "the resource's name (required)")
	remove.Flags().StringVar(&summary, "summary", "", "the commit's summary")
	_ = remove.MarkFlagRequired("name")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Print every declared resource, by kind",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, err := resources.All(layout())
			if err != nil {
				return err
			}
			kinds, err := resources.Kinds(layout())
			if err != nil {
				return err
			}
			for _, k := range kinds {
				fmt.Println(k)
				for _, n := range sortedNames(all[k]) {
					e := all[k][n]
					extra := ""
					if e.Capacity > 1 {
						extra = fmt.Sprintf("  capacity %d", e.Capacity)
					}
					if e.Ephemeral != nil && *e.Ephemeral {
						extra += "  ephemeral"
					}
					fmt.Printf("  %-20s %s%s\n", n, e.Purpose, extra)
				}
			}
			return nil
		},
	}

	cmd.AddCommand(declare, remove, listCmd)
	return cmd
}

// newLeaseCmd is the one runtime record with a mechanism behind it.
func newLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lease",
		Short: "Take, renew, release and list the units of a declared resource",
		Long: "A lease is the only way to say that a unit of a resource is yours until then. It\n" +
			"covers exclusion as well as capacity, because an exclusive resource is simply one\n" +
			"whose capacity is one, which is the default.",
	}

	var (
		resource string
		unit     string
		jobID    string
		ttl      time.Duration
	)

	take := &cobra.Command{
		Use:   "take",
		Short: "Take a unit of a resource until a time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lease, err := leases.Take(layout(), leases.Request{
				Resource: resource, Unit: unit, Occupant: actor, Job: jobID, TTL: ttl,
			}, time.Now().UTC())
			if err != nil {
				return err
			}
			fmt.Printf("holding %s.%s until %s\n", lease.Resource, lease.Unit, lease.Expires)
			return nil
		},
	}
	bindLeaseFlags(take, &resource, &unit, &jobID, &ttl)

	renew := &cobra.Command{
		Use:   "renew",
		Short: "Extend a lease you hold",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lease, err := leases.Renew(layout(), leases.Request{
				Resource: resource, Unit: unit, Occupant: actor, Job: jobID, TTL: ttl,
			}, time.Now().UTC())
			if err != nil {
				return err
			}
			fmt.Printf("holding %s.%s until %s\n", lease.Resource, lease.Unit, lease.Expires)
			return nil
		},
	}
	bindLeaseFlags(renew, &resource, &unit, &jobID, &ttl)

	release := &cobra.Command{
		Use:   "release",
		Short: "Release a lease you hold",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := leases.Release(layout(), resource, unit, actor); err != nil {
				return err
			}
			fmt.Printf("released %s.%s\n", resource, unit)
			return nil
		},
	}
	release.Flags().StringVar(&resource, "resource", "", "the resource (required)")
	release.Flags().StringVar(&unit, "unit", "", "which unit (required)")
	_ = release.MarkFlagRequired("resource")
	_ = release.MarkFlagRequired("unit")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Print every lease this site holds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			held, err := leases.Held(layout())
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			for _, lease := range held {
				state := "held"
				if free, _ := leases.Free(layout(), lease, now); free {
					state = "free"
				}
				fmt.Printf("%-20s %-6s %-14s %-28s until %s\n",
					lease.Resource+"."+lease.Unit, state, lease.Occupant, lease.Job, lease.Expires)
			}
			return nil
		},
	}

	cmd.AddCommand(take, renew, release, listCmd)
	return cmd
}

func bindLeaseFlags(cmd *cobra.Command, resource, unit, job *string, ttl *time.Duration) {
	cmd.Flags().StringVar(resource, "resource", "", "the resource (required)")
	cmd.Flags().StringVar(unit, "unit", "", "which unit; empty takes the first free one")
	cmd.Flags().StringVar(job, "job", "", "the job it is held for (required)")
	cmd.Flags().DurationVar(ttl, "ttl", 2*time.Hour, "how long it is held for")
	_ = cmd.MarkFlagRequired("resource")
	_ = cmd.MarkFlagRequired("job")
}

func newCollectCmd() *cobra.Command {
	var (
		window time.Duration
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Apply the retention policy to the payloads of closed jobs",
		Long: "The cheap half goes the moment a job closes, and the rest once the window has\n" +
			"passed, measured from the close. A job whose brief says keep: true is skipped\n" +
			"entirely. Collection never makes a record false: a handoff keeps the raw hash of\n" +
			"every artifact it names, so a collected artifact is a detectable absence.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outcomes, err := payload.Collect(layout(), payload.Options{
				Window: &window,
				DryRun: dryRun,
			})
			if err != nil {
				return err
			}
			for _, o := range outcomes {
				line := o.Summary()
				if dryRun {
					line = "would take: " + line
				}
				fmt.Println(line)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&window, "window", payload.DefaultWindow, "how long the larger half is kept after the close")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would go without removing anything")
	return cmd
}

func newCleanCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove what the coordination repository neither tracks nor ignores",
		Long: "This is the guarded clean: git clean without -x, so the ignored planes — .bare/,\n" +
			"store/, var/, job/ and worktree/ — are not candidates at all. A bare git clean -xdf\n" +
			"at the workspace root would delete the code repository and the entire source history\n" +
			"with it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := workspace.Clean(layout(), dryRun)
			if err != nil {
				return err
			}
			for _, p := range paths {
				if dryRun {
					fmt.Printf("would remove %s\n", p)
				} else {
					fmt.Printf("removed %s\n", p)
				}
			}
			if len(paths) == 0 {
				fmt.Println("nothing to remove")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would go without removing anything")
	return cmd
}

func relToRoot(l workspace.Layout, path string) string {
	return strings.TrimPrefix(strings.TrimPrefix(path, l.Root), "/")
}

func sortedNames(m map[string]format.Resource) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
