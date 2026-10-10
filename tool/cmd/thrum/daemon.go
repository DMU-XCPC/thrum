package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/leases"
	"github.com/spf13/cobra"
)

// newDaemonCmd is the one long-running process a site may run.
//
// Every command in this program works without it. The daemon exists for the three
// things a file cannot do — being alive, waking something, and holding an invariant
// across machines — and it adds no rule of its own.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run this site's daemon, and ask it what only it can answer",
		Long: "The daemon is optional, and the workspace is its state: a record, a lease and a\n" +
			"claim are all on disk, so losing the process loses only supervision — what it was\n" +
			"running and how it was started. Restarting it is safe.",
	}

	var sshListen string

	run := &cobra.Command{
		Use:     "run [--ssh-listen <where>]",
		Aliases: []string{"serve"},
		Short:   "Take the site's lock and serve until asked to stop: the forced command, and this site's repositories (also: serve)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("listening; interrupt to stop")
			return daemon.Run(layout(), daemon.Options{
				SSHListen: sshListen,
				Ready: func(socket, sshAddress string) {
					fmt.Printf("local %s\n", socket)
					if sshAddress != "" {
						fmt.Printf("peers %s (forced command %q)\n", sshAddress, daemon.ForcedCommand)
					}
				},
			})
		},
	}
	run.Flags().StringVar(&sshListen, "ssh-listen", "", "accept peers at host:port; empty means this site uses nothing remote")

	// exec is what a peer runs. It is the same handler as the socket's, so that a
	// system sshd with this as a forced command and this daemon's own SSH server do
	// exactly the same thing.
	execCmd := &cobra.Command{
		Use:   "exec",
		Short: "Answer one request from a peer, on standard input and output",
		Long: "This is the forced command: one request in, one response out, and no shell. Point\n" +
			"a system sshd at it with command=\"thrum daemon exec\" in authorized_keys, or let the\n" +
			"daemon serve SSH itself.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemon.Exec(daemon.NewService(layout()).Handle, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Ask the daemon here what only it can answer: what it is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, running, err := daemon.Local(layout(), daemon.Request{Verb: daemon.VerbStatus})
			if err != nil {
				return err
			}
			if !running {
				fmt.Println("no daemon is running here")
				fmt.Println("everything works without one; it is what runs and supervises actors, and what answers for this site")
				return nil
			}
			var data daemon.StatusData
			if err := decode(resp, &data); err != nil {
				return err
			}
			fmt.Printf("site %s  root %s  up %s\n", data.Site, data.Root, data.Uptime)
			fmt.Println("actors")
			if len(data.Actors) == 0 {
				fmt.Println("  (none)")
			}
			for _, a := range data.Actors {
				state := "running"
				if !a.Alive() {
					state = "gone"
					if a.Note != "" {
						state = "gone (" + a.Note + ")"
					}
				}
				fmt.Printf("  %-10s %-28s pid %-7d %s\n", a.Actor, a.Job, a.PID, state)
			}
			// Reachability is a question about the present, asked separately and
			// never as part of a status a peer might be asked for.
			fmt.Println("peers")
			var peers []daemon.PeerReport
			if resp, running, err := daemon.Local(layout(), daemon.Request{Verb: daemon.VerbPeers}); err == nil && running {
				var body struct {
					Peers []daemon.PeerReport `json:"peers"`
				}
				if err := decode(resp, &body); err == nil {
					peers = body.Peers
				}
			}
			for _, p := range peers {
				state := "no"
				if p.Reachable {
					state = "yes"
				}
				note := ""
				if p.Note != "" {
					note = "  " + p.Note
				}
				fmt.Printf("  %-20s %-3s %s%s\n", p.Name, state, p.Reach, note)
			}
			return nil
		},
	}

	var (
		jobID   string
		actorID string
		timeout time.Duration
	)

	start := &cobra.Command{
		Use:   "start --job <id> [--actor <name>] -- <command>...",
		Short: "Run an actor on a job, and supervise it",
		Long: "An actor is a harness this site has installed, not something the workspace ships: the\n" +
			"command is a fact about the machine. What it is started with is the design's — the\n" +
			"workspace root, the job id, and the variables that point a job's tools at its payload\n" +
			"rather than at their own temporary and cache directories.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			who := actorID
			if who == "" {
				who = actor
			}
			resp, running, err := daemon.Local(layout(), daemon.Request{
				Verb: daemon.VerbStart,
				Args: daemon.Args(map[string]any{"job": jobID, "actor": who, "command": args}),
			})
			if err != nil {
				return err
			}
			if !running {
				return fmt.Errorf("no daemon is running here, and starting an actor is what the daemon is for: run `thrum daemon run` first")
			}
			var a daemon.Actor
			if err := decode(resp, &a); err != nil {
				return err
			}
			fmt.Printf("started %s on %s as pid %d\n", a.Actor, a.Job, a.PID)
			return nil
		},
	}
	start.Flags().StringVar(&jobID, "job", "", "the job to work on (required)")
	start.Flags().StringVar(&actorID, "actor", "", "who this actor is (default: this actor)")
	_ = start.MarkFlagRequired("job")

	stop := &cobra.Command{
		Use:   "stop --job <id>",
		Short: "End the actor on a job and clear the claim it held",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, running, err := daemon.Local(layout(), daemon.Request{
				Verb: daemon.VerbStop,
				Args: daemon.Args(map[string]string{"job": jobID}),
			})
			if err != nil {
				return err
			}
			if !running {
				return fmt.Errorf("no daemon is running here")
			}
			_ = resp
			fmt.Printf("stopped %s\n", jobID)
			return nil
		},
	}
	stop.Flags().StringVar(&jobID, "job", "", "the job (required)")
	_ = stop.MarkFlagRequired("job")

	notify := &cobra.Command{
		Use:     "wake --job <id>",
		Aliases: []string{"notify"},
		Short:   "Say that a job reached done, and let every site wake what was waiting on it (also: notify)",
		Long: "No list of waiters is kept anywhere: the waiting is already written down in the\n" +
			"handoffs, in blocked_by, and this reads it. Nothing is woken because a file changed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, running, err := daemon.Local(layout(), daemon.Request{
				Verb: daemon.VerbAnnounce,
				Args: daemon.Args(map[string]string{"job": jobID, "actor": actor}),
			})
			if err != nil {
				return err
			}
			if !running {
				return fmt.Errorf("no daemon is running here")
			}
			var body struct {
				Woken []string `json:"woken"`
				Told  []string `json:"told"`
			}
			if err := decode(resp, &body); err != nil {
				return err
			}
			for _, job := range body.Woken {
				fmt.Printf("woke %s\n", job)
			}
			fmt.Printf("told %d sites\n", len(body.Told))
			return nil
		},
	}
	notify.Flags().StringVar(&jobID, "job", "", "the job that finished (required)")
	_ = notify.MarkFlagRequired("job")

	peers := &cobra.Command{
		Use:   "peers",
		Short: "Say which sites are reachable, right now",
		Long: "This is a question about the present and is never cached. Every site answers for\n" +
			"itself, and the roster says who should exist.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report := daemon.NewPeers(layout()).Report()
			if len(report) == 0 {
				fmt.Println("the roster declares no sites")
				return nil
			}
			for _, p := range report {
				state := "unreachable"
				if p.Reachable {
					state = "reachable"
				}
				note := ""
				if p.Note != "" {
					note = "  " + p.Note
				}
				fmt.Printf("%-20s %-12s %-24s%s\n", p.Name, state, p.Reach, note)
			}
			return nil
		},
	}

	var (
		resource string
		unit     string
		ttl      time.Duration
	)

	// Taking a lease at the site that holds the resource is the one operation that
	// needs several machines, and it is the mechanism the design names for holding
	// exclusion across them.
	coordinate := &cobra.Command{
		Use:   "coordinate --resource <name> --job <id>",
		Short: "Take a unit at the site the resource lives on",
		Long: "A resource is leased where it lives and never where it is used, so exclusion stays on\n" +
			"the machine the thing is physically on and nothing central is involved.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			holders, err := daemon.Held(l, resource)
			if err != nil {
				return err
			}
			peers := daemon.NewPeers(l)

			var local bool
			var remote string
			for _, h := range holders {
				if h.Local {
					local = true
					continue
				}
				if remote != "" {
					return fmt.Errorf("%s says it lives at %s and at %s: a resource has one home, and two sites claiming one is not something this workspace settles", resource, remote, h.Site)
				}
				remote = h.Site
			}

			if remote != "" {
				ref, err := peers.Lookup(remote)
				if err != nil {
					return err
				}
				resp, err := peers.Ask(ref, daemon.Request{
					Verb: daemon.VerbLeaseTake,
					Args: daemon.Args(map[string]any{
						"resource":    resource,
						"unit":        unit,
						"occupant":    actor,
						"job":         jobID,
						"ttl_seconds": int(ttl.Seconds()),
					}),
				})
				if err != nil {
					return err
				}
				var lease struct {
					Unit    string `json:"unit"`
					Expires string `json:"expires"`
				}
				if err := decode(resp, &lease); err != nil {
					return err
				}
				fmt.Printf("%s took %s.%s at %s until %s\n", actor, resource, lease.Unit, remote, lease.Expires)
				return nil
			}

			if !local {
				return fmt.Errorf("no site says it holds %s: a resource is leased where it lives, and where that is has to be said by the machine that has it", resource)
			}
			lease, err := leases.Take(l, leases.Request{
				Resource: resource, Unit: unit, Occupant: actor, Job: jobID, TTL: ttl,
			}, time.Now().UTC())
			if err != nil {
				return err
			}
			fmt.Printf("%s took %s.%s here until %s\n", actor, resource, lease.Unit, lease.Expires)
			return nil
		},
	}
	coordinate.Flags().StringVar(&resource, "resource", "", "the resource (required)")
	coordinate.Flags().StringVar(&unit, "unit", "", "which unit; empty takes the first free one")
	coordinate.Flags().StringVar(&jobID, "job", "", "the job it is held for (required)")
	coordinate.Flags().DurationVar(&ttl, "ttl", 2*time.Hour, "how long it is held for")
	_ = coordinate.MarkFlagRequired("resource")
	_ = coordinate.MarkFlagRequired("job")
	_ = timeout

	cmd.AddCommand(run, execCmd, status, start, stop, notify, peers, coordinate)
	return cmd
}

// decode reads a response body into the caller's own structure.
func decode(resp daemon.Response, into any) error {
	if len(resp.Data) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Data, into)
}
