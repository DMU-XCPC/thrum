// Command thrum is the workspace tool: the operations a workspace needs, and the
// daemon that makes a site reachable.
//
// The command surface is not part of the design. What is part of it is that every
// operation exists and refuses what it must.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/spf13/cobra"
)

// The tree the command operates on, and who is running it. Both are ambient: an actor
// is a stable name rather than a per-command choice, and a workspace root is usually
// where the command was run — or what whoever started this work said it was.
var (
	rootDir string
	actor   string
)

func main() {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "thrum:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "thrum",
		Short: "Operate a multi-agent workspace",
		Long: "thrum creates a workspace from the template and then does the work that has\n" +
			"to be done by a tool rather than by hand: the operations that two actors must\n" +
			"not both succeed at, the ones that span several files or repositories, the\n" +
			"ones that compute something a hand gets wrong, and the ones that answer a\n" +
			"question no file can.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Being a name is being an actor, so the first thing any command does is let
		// the roster know this name is here. There is no join, no invite, and nothing
		// to be added to.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			l := workspace.New(rootDir)
			if _, err := os.Stat(l.Var()); err != nil {
				return nil // nothing to register with yet
			}
			if err := roster.Seen(l, actor, "", now()); err != nil {
				return err
			}
			// A site that says it accepts peers has to have something accepting them,
			// or a message addressed to an actor here is refused for no good reason.
			// A command that does not need a daemon does not wait long for one.
			//
			// The daemon's own commands are the exception, and they have to be: the
			// thing that gets started must not start itself, or it spends its life
			// spawning children that each try to start it again.
			if isDaemonCommand(cmd) {
				return nil
			}
			if err := ensureServing(l); err != nil {
				fmt.Fprintf(os.Stderr, "thrum: this site accepts peers and no daemon is serving them: %v\n", err)
			}
			return nil
		},
	}
	root.PersistentFlags().StringVar(&rootDir, "root", defaultRoot(), "the workspace root")
	root.PersistentFlags().StringVar(&actor, "actor", defaultActor(), "who is running this (default: $THRUM_ACTOR, else $USER)")

	root.AddCommand(
		newCreateCmd(),
		newVerifyCmd(),
		newStatusCmd(),
		newSyncCmd(),
		newCleanCmd(),
		newCollectCmd(),
		// Jobs.
		newOpenCmd(),
		newHandoffCmd(),
		newCloseCmd(),
		newClaimCmd(),
		newRelinquishCmd(),
		newWorktreeCmd(),
		newCommitCmd(),
		// Runtime.
		newOccupancyCmd(),
		newResourceCmd(),
		newLeaseCmd(),
		// Assets and payloads.
		newAssetCmd(),
		newBundleCmd(),
		// Actors, and reaching them.
		newActorCmd(),
		newEnvCmd(),
		newSendCmd(),
		newInboxCmd(),
		newReadCmd(),
		newReplyCmd(),
		newPurgeCmd(),
		// The daemon is an implementation detail, and these are hidden.
		newDaemonCmd(),
	)
	return root
}

// isDaemonCommand reports whether this command is the daemon itself, or one of the
// things it is asked to do.
func isDaemonCommand(cmd *cobra.Command) bool {
	return strings.HasPrefix(cmd.CommandPath(), "thrum daemon")
}

// defaultRoot is where the workspace is: what the environment says, else where the
// command was run. An actor started inside a working directory is told where the
// workspace is, and it should not have to pass it again on every command.
func defaultRoot() string {
	if root := os.Getenv("THRUM_ROOT"); root != "" {
		return root
	}
	return "."
}

func defaultActor() string {
	for _, candidate := range []string{os.Getenv("THRUM_ACTOR"), os.Getenv("USER"), os.Getenv("LOGNAME")} {
		if s := slugify(candidate); s != "" {
			return s
		}
	}
	return "unknown"
}

// slugify turns a hostname or a user name into something a workspace can use as a
// name.
func slugify(s string) string {
	var b strings.Builder
	lastHyphen := true // leading hyphens are dropped
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) && r < unicode.MaxASCII, unicode.IsDigit(r):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func defaultSite() string {
	host, err := os.Hostname()
	if err == nil {
		if s := slugify(host); s != "" {
			return s
		}
	}
	return "site"
}

// list splits a comma-separated flag, dropping empties. The design's lists are short
// and typed by people, so a comma is enough.
func list(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// now is the clock, in the one form this workspace writes time in.
func now() time.Time { return time.Now().UTC() }
