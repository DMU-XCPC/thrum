package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/repos"
)

// newSiteTransportCmd points this site's repositories at a peer's daemon: sync then
// fetches, merges and pushes over the site-authenticated channel.
func newSiteTransportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "transport <peer>",
		Short: "Point this site's repositories at a peer's daemon",
		Long:  "Replaces each repository's origin with a git ext:: remote that goes through the peer's daemon; no external git remote is needed after this.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			peer := args[0]
			if peer == readSiteName(l) {
				return fmt.Errorf("%q is this site", peer)
			}
			if peerReach(l, peer) == "" {
				return fmt.Errorf("no reach is entered for %q; set one with site reach", peer)
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if strings.ContainsAny(l.Root, " \t\n") || strings.ContainsAny(exe, " \t\n") {
				return fmt.Errorf("the workspace or binary path contains a space; git's ext transport cannot carry it")
			}
			url := func(repo, service string) string {
				return "ext::" + exe + " --root " + l.Root +
					" git-proxy --peer " + peer + " --repo " + repo + " --service " + service
			}

			type target struct {
				name string
				dir  string
			}
			targets := []target{{"line", l.Root}, {"ledger", l.Ledger()}}
			if _, err := os.Stat(l.Store()); err == nil {
				targets = append(targets, target{"store", l.Store()})
			}
			decls, err := repos.List(l)
			if err != nil {
				return err
			}
			for _, d := range decls {
				if _, err := os.Stat(l.CodeRepo(d.Repo)); err == nil {
					targets = append(targets, target{d.Repo, l.CodeRepo(d.Repo)})
				}
			}
			for _, t := range targets {
				_, _ = gitx.Run(t.dir, "remote", "remove", "origin")
				if err := gitx.OK(t.dir, "remote", "add", "origin", url(t.name, "upload-pack")); err != nil {
					return err
				}
				if err := gitx.OK(t.dir, "remote", "set-url", "--push", "origin", url(t.name, "receive-pack")); err != nil {
					return err
				}
				if err := gitx.OK(t.dir, "config", "protocol.ext.allow", "always"); err != nil {
					return err
				}
			}
			for _, dir := range []string{l.Root, l.Ledger()} {
				if err := gitx.OK(dir, "config", "receive.denyCurrentBranch", "updateInstead"); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pointed %d repositories at %s\n", len(targets), peer)
			return nil
		},
	}
}

func peerReach(l layout.Layout, peer string) string {
	if b, err := os.ReadFile(filepath.Join(l.ConfigDir(), peer+".yaml")); err == nil {
		var cfg struct {
			Reach string `yaml:"reach"`
		}
		if err := format.DecodeWhole(b, &cfg); err == nil && cfg.Reach != "" {
			return cfg.Reach
		}
	}
	var s records.Site
	if ok, err := records.Read(l.Site(peer), &s); err == nil && ok {
		return s.Reach
	}
	return ""
}
