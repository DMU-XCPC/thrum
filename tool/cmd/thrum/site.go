package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/join"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

type siteView struct {
	Site       string `json:"site"`
	Key        string `json:"key"`
	Reach      string `json:"reach,omitempty"`
	Local      string `json:"local_reach,omitempty"`
	Discovered string `json:"discovered,omitempty"`
	NextHop    string `json:"next_hop,omitempty"`
	Hops       int    `json:"hops,omitempty"`
}

type routeView struct {
	NextHop string `json:"next_hop" yaml:"next_hop"`
	Hops    int    `json:"hops" yaml:"hops"`
}

// discoveredAddress is the last address a peer announced on the LAN. It is only a
// hint; the roster's declared address and this machine's local view come first.
func discoveredAddress(l layout.Layout, name string) string {
	data, err := os.ReadFile(filepath.Join(l.Var(), "discovered", name+".yaml"))
	if err != nil {
		return ""
	}
	var d struct {
		Reach string `yaml:"reach"`
		Seen  string `yaml:"seen"`
	}
	if format.DecodeWhole(data, &d) != nil {
		return ""
	}
	return d.Reach
}

// readRoutes reads the daemon's learned table; it is a hint, not state that syncs.
func readRoutes(l layout.Layout) map[string]routeView {
	data, err := os.ReadFile(filepath.Join(l.Var(), "routes.yaml"))
	if err != nil {
		return nil
	}
	var d struct {
		Sites map[string]routeView `yaml:"sites"`
	}
	if format.DecodeWhole(data, &d) != nil {
		return nil
	}
	return d.Sites
}

func newSiteCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "site", Short: "The roster, and how this machine reaches the others"}

	var inviteAt string
	invite := &cobra.Command{
		Use:   "invite <name> --at <host:port>",
		Short: "Declare a new site and print the token that joins it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			self := readSiteName(l)
			if self == "" {
				return fmt.Errorf("this machine does not name its site")
			}
			name := args[0]
			if name == self {
				return fmt.Errorf("%q is this site", name)
			}
			if _, err := os.Stat(l.Site(name)); err == nil {
				return fmt.Errorf("site %q is already declared", name)
			}
			// The new site's address is optional: omit it and the new machine is
			// found on the LAN. So is the inviter's: the token carries it when it is
			// declared, and the joiner asks the LAN when it is not.
			reach := daemon.Reach(l, self)
			var me records.Site
			if _, err := records.Read(l.Site(self), &me); err != nil {
				return err
			}
			peerKey := me.Key
			if peerKey == "" {
				var err error
				peerKey, err = daemon.PublicKey(l)
				if err != nil {
					return err
				}
			}
			ws, err := join.WorkspaceID(l.Root)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(l.Sites(), 0o755); err != nil {
				return err
			}
			nonce := join.NewNonce()
			if err := records.Write(l.Site(name), records.Site{
				Site: name, Reach: inviteAt, Invite: join.InviteHash(nonce),
			}); err != nil {
				return err
			}
			if err := line.Commit(l.Root, []string{l.Site(name)},
				fmt.Sprintf("site %s: invited", name)); err != nil {
				return err
			}
			token, err := join.Encode(join.Token{
				Workspace: ws, Site: name, Peer: self, Reach: reach,
				PeerKey: peerKey, At: inviteAt, Nonce: nonce,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), token)
			return nil
		},
	}
	invite.Flags().StringVar(&inviteAt, "at", "", "the host:port the new site will listen on; omit to find it on the LAN")

	var at string
	reach := &cobra.Command{
		Use:   "reach <name> --at <host:port>",
		Short: "Say where this machine reaches a site",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if at == "" {
				return fmt.Errorf("--at is required")
			}
			l := workspaceRoot()
			if err := os.MkdirAll(l.ConfigDir(), 0o755); err != nil {
				return err
			}
			data, err := format.MarshalYAML(struct {
				Reach string `yaml:"reach"`
			}{Reach: at})
			if err != nil {
				return err
			}
			path := filepath.Join(l.ConfigDir(), args[0]+".yaml")
			if err := format.WriteFileAtomic(path, data); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is reached at %s\n", args[0], at)
			return nil
		},
	}
	reach.Flags().StringVar(&at, "at", "", "host:port")

	list := &cobra.Command{
		Use:   "list",
		Short: "The roster, and what this machine knows of each site",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			views, err := siteViews(l)
			if err != nil {
				return err
			}
			if jsonFlag {
				return printJSON(cmd, views)
			}
			for _, v := range views {
				addr := v.Local
				if addr == "" {
					addr = v.Reach
				}
				if addr == "" {
					addr = v.Discovered
				}
				if addr == "" {
					addr = "no reach"
				}
				via := ""
				if v.NextHop != "" {
					via = fmt.Sprintf("via %s (%d)", v.NextHop, v.Hops)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-20s %-22s %s\n", v.Site, shortKey(v.Key), addr, via)
			}
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show <name>",
		Short: "One site's declaration and this machine's reach",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			var s records.Site
			ok, err := records.Read(l.Site(args[0]), &s)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no site named %q", args[0])
			}
			v := siteView{Site: s.Site, Key: s.Key, Reach: s.Reach, Local: localReach(l, s.Site), Discovered: discoveredAddress(l, s.Site)}
			if r, ok := readRoutes(l)[s.Site]; ok {
				v.NextHop, v.Hops = r.NextHop, r.Hops
			}
			if jsonFlag {
				return printJSON(cmd, v)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "site   %s\nkey    %s\nreach  %s\nlocal  %s\ndisc.  %s\n",
				v.Site, v.Key, orNone(v.Reach), orNone(v.Local), orNone(v.Discovered))
			if v.NextHop != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "next   %s (%d hop(s))\n", v.NextHop, v.Hops)
			}
			return nil
		},
	}

	routes := &cobra.Command{
		Use:   "routes",
		Short: "This machine's route table, as the daemon learned it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			table := readRoutes(l)
			if jsonFlag {
				return printJSON(cmd, table)
			}
			names := make([]string, 0, len(table))
			for name := range table {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				r := table[name]
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s via %-16s %d hop(s)\n", name, r.NextHop, r.Hops)
			}
			return nil
		},
	}

	doctor := &cobra.Command{
		Use:   "doctor",
		Short: "For each peer: declared, addressed, and answering",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			views, err := siteViews(l)
			if err != nil {
				return err
			}
			self := readSiteName(l)
			for _, v := range views {
				if v.Site == self {
					fmt.Fprintf(cmd.OutOrStdout(), "%-16s this site\n", v.Site)
					continue
				}
				addr := v.Local
				if addr == "" {
					addr = v.Reach
				}
				if addr == "" {
					fmt.Fprintf(cmd.OutOrStdout(), "%-16s no reach entered\n", v.Site)
					continue
				}
				client, err := daemon.Dial(l, v.Site)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%-16s did not answer: %v\n", v.Site, err)
					continue
				}
				client.Close()
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s answered at %s\n", v.Site, addr)
			}
			return nil
		},
	}

	cmd.AddCommand(reach, list, show, routes, doctor, invite, newSiteKeyCmd(), newSiteForgetCmd())
	return cmd
}

func siteViews(l layout.Layout) ([]siteView, error) {
	entries, err := os.ReadDir(l.Sites())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	routes := readRoutes(l)
	var out []siteView
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		var s records.Site
		ok, err := records.Read(filepath.Join(l.Sites(), e.Name()), &s)
		if err != nil {
			return nil, err
		}
		if ok {
			v := siteView{Site: s.Site, Key: s.Key, Reach: s.Reach, Local: localReach(l, s.Site), Discovered: discoveredAddress(l, s.Site)}
			if r, ok := routes[s.Site]; ok {
				v.NextHop, v.Hops = r.NextHop, r.Hops
			}
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Site < out[j].Site })
	return out, nil
}

func localReach(l layout.Layout, name string) string {
	data, err := os.ReadFile(filepath.Join(l.ConfigDir(), name+".yaml"))
	if err != nil {
		return ""
	}
	var cfg struct {
		Reach string `yaml:"reach"`
	}
	if err := format.DecodeWhole(data, &cfg); err != nil {
		return ""
	}
	return cfg.Reach
}

func readSiteName(l layout.Layout) string {
	data, err := os.ReadFile(l.SiteFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func shortKey(key string) string {
	fields := strings.Fields(key)
	if len(fields) < 2 {
		return key
	}
	k := fields[1]
	if len(k) > 16 {
		k = k[:16] + "…"
	}
	return fields[0] + " " + k
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
