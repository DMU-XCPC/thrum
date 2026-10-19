package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
)

// newMigrateCmd moves an existing workspace's networking records from the old
// `reach` name to `endpoint` (the roster) and `via` (this machine's route). It is
// idempotent: a workspace already on the new names reports nothing to do.
func newMigrateCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Move a workspace's networking records to the endpoint/via names",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			out := cmd.OutOrStdout()

			// The roster: declared endpoints are versioned in the line.
			sites, err := filepath.Glob(filepath.Join(l.Root, "doc", "site", "*.yaml"))
			if err != nil {
				return err
			}
			var changed []string
			for _, path := range sites {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var d struct {
					Site     string `yaml:"site"`
					Key      string `yaml:"key"`
					Endpoint string `yaml:"endpoint"`
					Reach    string `yaml:"reach"`
				}
				if err := format.DecodeWhole(data, &d); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				if d.Reach == "" {
					continue
				}
				changed = append(changed, path)
				fmt.Fprintf(out, "roster: %s  reach -> endpoint\n", filepath.Base(path))
				if dryRun {
					continue
				}
				endpoint := d.Endpoint
				if endpoint == "" {
					endpoint = d.Reach
				}
				if err := writeEndpoint(path, d.Site, d.Key, endpoint); err != nil {
					return err
				}
			}

			// This machine's route overrides are site-local, not versioned.
			configs, err := filepath.Glob(filepath.Join(l.ConfigDir(), "*.yaml"))
			if err != nil {
				return err
			}
			for _, path := range configs {
				name := filepath.Base(path)
				name = name[:len(name)-len(filepath.Ext(name))]
				cfg := readLocalConfig(l, name)
				if cfg.Reach == "" {
					continue
				}
				fmt.Fprintf(out, "local:  %s  reach -> via\n", filepath.Base(path))
				if dryRun {
					continue
				}
				via := cfg.Via
				if via == "" {
					via = cfg.Reach
				}
				cfg.Via, cfg.Reach = via, ""
				if err := writeLocalConfig(l, name, cfg); err != nil {
					return err
				}
			}

			if len(changed) == 0 {
				fmt.Fprintln(out, "nothing to migrate")
				return nil
			}
			if dryRun {
				fmt.Fprintln(out, "(dry run: nothing written)")
				return nil
			}
			if err := line.Commit(l.Root, changed, "site: rename reach to endpoint"); err != nil {
				return err
			}
			fmt.Fprintln(out, "migrated; check with `thrum verify` and `thrum site routes`")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report without writing")
	return cmd
}

// writeEndpoint rewrites one site record with the endpoint name.
func writeEndpoint(path, site, key, endpoint string) error {
	front, err := format.MarshalYAML(struct {
		Site     string `yaml:"site"`
		Key      string `yaml:"key"`
		Endpoint string `yaml:"endpoint,omitempty"`
	}{Site: site, Key: key, Endpoint: endpoint})
	if err != nil {
		return err
	}
	return format.SaveDocument(path, front, []byte(""))
}
