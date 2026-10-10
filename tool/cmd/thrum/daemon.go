package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/spf13/cobra"
)

// The daemon is an implementation detail, and nothing a person types names it.
//
// It exists because a site has to be *reachable*: to answer a peer's question about
// who is on a job, to hold a lease another site asks for, and to take delivery of a
// message addressed to an actor here. Everything on one machine works without it — a
// mailbox is a directory and a roster is a file — so it is started when this site says
// it accepts peers, and it stays until it is stopped.
//
// The commands here are hidden because they are for the tool itself, for an operator
// who wants it under systemd, and for a system sshd that names `thrum daemon exec` as
// a forced command.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "The implementation detail that makes this site reachable",
		Hidden: true,
	}

	var sshListen string

	run := &cobra.Command{
		Use:    "run",
		Short:  "Take the site's lock and serve until asked to stop",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
	run.Flags().StringVar(&sshListen, "ssh-listen", "",
		"where to accept peers, host:port or unix:<path> (default: var/daemon.yaml)")

	stop := &cobra.Command{
		Use:    "stop",
		Short:  "Ask the daemon here to leave",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Stop(layout()); err != nil {
				return err
			}
			fmt.Println("stopping")
			return nil
		},
	}

	// exec is what a peer runs: the same handler as the socket's, one request in, one
	// response out, and no shell.
	execCmd := &cobra.Command{
		Use:    "exec",
		Short:  "Answer one request from a peer, on standard input and output",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemon.Exec(daemon.NewService(layout()).Handle, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	cmd.AddCommand(run, stop, execCmd)
	return cmd
}

// askDaemon sends one request to the daemon here, if there is one. No daemon is not an
// error: most operations do not need one.
func askDaemon(l workspace.Layout, req daemon.Request) (daemon.Response, bool, error) {
	return daemon.Local(l, req)
}

// ensureServing starts this site's daemon when the site says it accepts peers and
// nothing is listening.
//
// A site that says it accepts nobody needs no daemon, and starting one anyway would be
// a background process nobody asked for. A site that does accept them needs something
// there to accept them, because a message that arrives at a quiet site is a message
// that is refused.
func ensureServing(l workspace.Layout) error {
	if daemon.ListenSpec(l) == "" {
		return nil
	}
	if _, running, err := askDaemon(l, daemon.Request{Verb: daemon.VerbStatus}); err == nil && running {
		return nil
	}
	return startDaemonWithin(l, 3*time.Second)
}

// startDaemon runs this site's daemon in the background and waits for it to answer.
//
// It is detached from this shell, in its own session, and its output goes to
// var/daemon.log — which is the only place a person can find out why it refused
// something.
func startDaemon(l workspace.Layout) error {
	return startDaemonWithin(l, 10*time.Second)
}

// startDaemonWithin is the same, with the patience the caller has. A command that does
// not need a daemon should not wait long for one: the site will be reachable a moment
// later, and the operator can look at the log.
func startDaemonWithin(l workspace.Layout, patience time.Duration) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.Var(), 0o755); err != nil {
		return err
	}
	log, err := os.OpenFile(l.DaemonLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()

	cmd := exec.Command(self, "--root", l.Root, "daemon", "run")
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// The child is meant to outlive this command, so it is not waited for.
	_ = cmd.Process.Release()

	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if _, running, err := askDaemon(l, daemon.Request{Verb: daemon.VerbStatus}); err == nil && running {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("the daemon did not come up; see %s", l.DaemonLog())
}

// decode reads a response body into the caller's own structure.
func decode(resp daemon.Response, into any) error {
	if len(resp.Data) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Data, into)
}

// printJSON is how a command hands its answer to other tooling rather than to a person.
func printJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
