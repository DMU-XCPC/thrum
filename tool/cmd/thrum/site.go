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
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

type siteView struct {
	Site  string `json:"site"`
	Key   string `json:"key"`
	Reach string `json:"reach,omitempty"`
	Local string `json:"local_reach,omitempty"`
}

func newSiteCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "site", Short: "The roster, and how this machine reaches the others"}

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
					addr = "no reach"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-20s %s\n", v.Site, shortKey(v.Key), addr)
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
			v := siteView{Site: s.Site, Key: s.Key, Reach: s.Reach, Local: localReach(l, s.Site)}
			if jsonFlag {
				return printJSON(cmd, v)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "site   %s\nkey    %s\nreach  %s\nlocal  %s\n",
				v.Site, v.Key, orNone(v.Reach), orNone(v.Local))
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

	cmd.AddCommand(reach, list, show, doctor)
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
			out = append(out, siteView{Site: s.Site, Key: s.Key, Reach: s.Reach, Local: localReach(l, s.Site)})
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
