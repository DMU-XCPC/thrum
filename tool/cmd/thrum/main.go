// Command thrum is the workspace tool: the operations a workspace needs, and the daemon
// that gives them liveness and peers.
//
// The command surface is not part of the design. What is part of it is that every
// operation exists and refuses what it must.
package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

// The tree the command operates on, and who is running it. Both are ambient: an actor
// is a stable identity rather than a per-command choice, and a workspace root is
// usually simply where the command was run.
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
	}
	root.PersistentFlags().StringVar(&rootDir, "root", ".", "the workspace root")
	root.PersistentFlags().StringVar(&actor, "actor", defaultActor(), "who is running this (default: $THRUM_ACTOR, else $USER)")

	root.AddCommand(
		newCreateCmd(),
		newVerifyCmd(),
		newStatusCmd(),
		newOpenCmd(),
		newHandoffCmd(),
		newCloseCmd(),
		newClaimCmd(),
		newRelinquishCmd(),
		newWorktreeCmd(),
		newStartCmd(),
		newStopCmd(),
		newOccupancyCmd(),
		newResourceCmd(),
		newLeaseCmd(),
		newAssetCmd(),
		newBundleCmd(),
		newDaemonCmd(),
		newActorCmd(),
		newSyncCmd(),
		newCollectCmd(),
		newCleanCmd(),
	)
	return root
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
