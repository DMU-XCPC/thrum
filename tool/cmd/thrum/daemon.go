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
// It exists for two things a file cannot do: supervising an actor, and acting later on
// behalf of a job that is waiting. An operation that needs either asks for one here —
// the tool starts it, detached, and it leaves on its own once nothing is running and
// nothing is waiting. The two commands below are hidden because they are for the tool
// itself, for an operator who wants it under systemd, and for a system sshd that names
// `thrum daemon exec` as a forced command.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "The implementation detail behind `start`, `stop` and being woken",
		Hidden: true,
	}

	var sshListen string
	run := &cobra.Command{
		Use:    "run",
		Short:  "Take the site's lock and serve until asked to stop, or until there is nothing left to do",
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

	cmd.AddCommand(run, execCmd)
	return cmd
}

// askDaemon sends one request to the daemon here, if there is one. No daemon is not an
// error: it means this site is running nothing, and most operations do not care.
func askDaemon(l workspace.Layout, req daemon.Request) (daemon.Response, bool, error) {
	return daemon.Local(l, req)
}

// needDaemon sends one request, starting a daemon when there is none — because the
// operation asked for something that has to be alive, and refusing to run is how a
// workspace stays stuck.
func needDaemon(l workspace.Layout, req daemon.Request) (daemon.Response, error) {
	if resp, running, err := daemon.Local(l, req); err == nil && running {
		return resp, nil
	}
	if err := startDaemon(l); err != nil {
		return daemon.Response{}, err
	}
	resp, running, err := daemon.Local(l, req)
	if err != nil {
		return resp, err
	}
	if !running {
		return resp, fmt.Errorf("the daemon did not answer; see %s", l.DaemonLog())
	}
	return resp, nil
}

// startDaemonIfNeeded starts one only when there is none: an operation that needs
// something alive should not restart a daemon that is already there.
func startDaemonIfNeeded(l workspace.Layout) error {
	if _, running, err := daemon.Local(l, daemon.Request{Verb: daemon.VerbStatus}); err == nil && running {
		return nil
	}
	return startDaemon(l)
}

// startDaemon runs this site's daemon in the background and waits for it to answer.
//
// It is detached from this shell, in its own session, and its output goes to
// var/daemon.log — which is the only place a person can find out why it refused
// something.
func startDaemon(l workspace.Layout) error {
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

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, running, err := daemon.Local(l, daemon.Request{Verb: daemon.VerbStatus}); err == nil && running {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("the daemon did not come up; see %s", l.DaemonLog())
}

// announce tells everyone who cares that a job is no longer something to wait for: the
// daemon here, if one is running, and every peer directly.
//
// Peers are told over SSH rather than through a daemon here, because there may not be
// one — and because the site that has to wake something is the site that is waiting.
func announce(l workspace.Layout, job, actor string) []string {
	req := daemon.Request{
		Verb: daemon.VerbNotify,
		Args: daemon.Args(map[string]string{"job": job, "actor": actor}),
	}
	_, _, _ = daemon.Local(l, req) // the daemon here wakes its own waiters
	told, _ := daemon.NewPeers(l).Broadcast(req)
	return told
}

// decode reads a response body into the caller's own structure.
func decode(resp daemon.Response, into any) error {
	if len(resp.Data) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Data, into)
}
