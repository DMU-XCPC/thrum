package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

func newSiteKeyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "key", Short: "Rotate this site's key, or withdraw another's"}

	rotate := &cobra.Command{
		Use:   "rotate",
		Short: "Replace this site's key and publish the new public half",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			self := readSiteName(l)
			if self == "" {
				return fmt.Errorf("this machine does not name its site")
			}
			seed, pub, err := workspace.GenerateSiteKey()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(l.SecretsDir(), 0o700); err != nil {
				return err
			}
			if old, err := os.ReadFile(l.PrivateKey()); err == nil {
				if err := os.WriteFile(l.PrivateKey()+".prev", old, 0o600); err != nil {
					return err
				}
			}
			body := base64.StdEncoding.EncodeToString(seed) + "\n"
			if err := os.WriteFile(l.PrivateKey(), []byte(body), 0o600); err != nil {
				return err
			}
			var s records.Site
			ok, err := records.Read(l.Site(self), &s)
			if err != nil {
				return err
			}
			if !ok {
				s = records.Site{Site: self}
			}
			s.Key = pub
			if err := records.Write(l.Site(self), s); err != nil {
				return err
			}
			if err := line.Commit(l.Root, []string{l.Site(self)},
				fmt.Sprintf("site %s: key rotated", self)); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), pub)
			return nil
		},
	}

	revoke := &cobra.Command{
		Use:   "revoke <site>",
		Short: "Withdraw another site's key; every peer will refuse it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			name := args[0]
			if name == readSiteName(l) {
				return fmt.Errorf("refusing to revoke this site's own key: rotate it instead")
			}
			var s records.Site
			ok, err := records.Read(l.Site(name), &s)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no site named %q", name)
			}
			if s.Key == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "%s has no key to revoke\n", name)
				return nil
			}
			s.Key = ""
			if err := records.Write(l.Site(name), s); err != nil {
				return err
			}
			if err := line.Commit(l.Root, []string{l.Site(name)},
				fmt.Sprintf("site %s: key revoked", name)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "revoked %s\n", name)
			return nil
		},
	}

	cmd.AddCommand(rotate, revoke)
	return cmd
}

func newSiteForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <site>",
		Short: "Remove a site from the roster; its name stays taken",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			name := args[0]
			if name == readSiteName(l) {
				return fmt.Errorf("refusing to forget this site")
			}
			if _, err := os.Stat(l.Site(name)); err != nil {
				return fmt.Errorf("no site named %q", name)
			}
			entries, err := os.ReadDir(l.Sites())
			if err != nil {
				return err
			}
			if len(entries) <= 1 {
				return fmt.Errorf("refusing to forget the last site")
			}
			if err := os.Remove(l.Site(name)); err != nil {
				return err
			}
			if err := line.Commit(l.Root, []string{l.Site(name)},
				fmt.Sprintf("site %s: forgotten", name)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "forgot %s\n", name)
			return nil
		},
	}
}
