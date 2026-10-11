package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

// newSiteCmd is how a machine's own half of a workspace is written.
//
// All of it is site-local and none of it is committed: how this machine is reached, how it
// reaches the others, and the key it offers. Writing it by hand works, and it is also how a
// site ends up half-configured with nothing noticing until somebody sends it a message, so
// the tooling writes it instead.
func newSiteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Say how this machine is reached, and how it reaches the others",
		Long: "Everything here writes this machine's own half of the workspace, under var/, and none\n" +
			"of it is ever committed: how to reach a machine is that machine's business. A site with\n" +
			"no listen: accepts no peers and needs no daemon; a peer with no reach for this site\n" +
			"cannot ask it anything.",
	}

	var listenAt string
	listen := &cobra.Command{
		Use:   "listen [--at <address>]",
		Short: "Accept peers at this address, or stop accepting them",
		Long: "The address is where this machine's daemon listens: host:port, or unix:/path/to.sock\n" +
			"for a socket a tunnel or a system sshd reaches. With no --at, this site accepts no\n" +
			"peers and runs no daemon.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			values, err := readSiteFile(l.DaemonConfig())
			if err != nil {
				return err
			}
			if listenAt == "" {
				delete(values, "listen")
			} else {
				values["listen"] = listenAt
			}
			if err := writeSiteFile(l.DaemonConfig(), values); err != nil {
				return err
			}
			if listenAt == "" {
				fmt.Println("this site accepts no peers")
				return nil
			}
			fmt.Printf("this site accepts peers at %s\n", listenAt)
			fmt.Println("each site that has to reach it needs its own var/config/" + siteName(l) + ".yaml pointing there")
			return nil
		},
	}
	listen.Flags().StringVar(&listenAt, "at", "", "host:port, or unix:/path/to.sock; empty means this site accepts no peers")

	var sshUser, keyPath string
	var forget bool
	reach := &cobra.Command{
		Use:   "reach <peer> [<address>]",
		Short: "Say how this site reaches another one, or forget it",
		Long: "This is this site's outbound half, and only its own: it is how this machine dials that\n" +
			"one. The other site has its own file saying how it dials this one, and nothing here can\n" +
			"write that.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			peer := args[0]
			if !validName(peer) {
				return fmt.Errorf("%q is not a site name: a name is a slug", peer)
			}
			path := filepath.Join(l.Config(), peer+".yaml")
			if forget {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
				fmt.Printf("this site no longer reaches %s\n", peer)
				return nil
			}
			if len(args) < 2 || args[1] == "" {
				return fmt.Errorf("how to reach %s: give an address, like host:22, user@host:22 or unix:/path/to.sock", peer)
			}
			values := map[string]any{"reach": args[1]}
			if sshUser != "" {
				values["ssh_user"] = sshUser
			}
			if keyPath != "" {
				values["key"] = keyPath
			}
			if err := writeSiteFile(path, values); err != nil {
				return err
			}
			fmt.Printf("this site reaches %s at %s\n", peer, args[1])
			if !declaredSite(l, peer) {
				fmt.Fprintf(os.Stderr, "thrum: the roster does not declare %s yet, so this reach will be reported as stale until some site declares it\n", peer)
			}
			return nil
		},
	}
	reach.Flags().StringVar(&sshUser, "ssh-user", "", "the user to dial as, when it is not the one git and ssh would pick")
	reach.Flags().StringVar(&keyPath, "key", "", "the private key to offer, when it is not var/secrets/id_ed25519")
	reach.Flags().BoolVar(&forget, "forget", false, "remove the reach for this peer")

	key := &cobra.Command{
		Use:   "key",
		Short: "Make this site's key if it has none, and print the line a peer authorises",
		Long: "The key is this site's identity to the others: the private half stays in\n" +
			"var/secrets/, and the public half goes into each peer's var/secrets/authorized_keys.\n" +
			"Nothing here writes on another machine, so the line is printed to be carried over.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			signer, err := ensureSiteKey(l)
			if err != nil {
				return err
			}
			public := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
			fmt.Println(public)
			fmt.Fprintf(os.Stderr, "add that line to var/secrets/authorized_keys on each site that has to reach this one\n")
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show",
		Short: "Print what this site says about itself, and what it is missing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			fmt.Printf("site      %s\n", siteName(l))
			if spec := daemon.ListenSpec(l); spec != "" {
				fmt.Printf("accepts   peers at %s\n", spec)
			} else {
				fmt.Println("accepts   nobody: no listen: in var/daemon.yaml, so no daemon and no peers")
			}
			if _, err := os.Stat(filepath.Join(l.Secrets(), "id_ed25519")); err == nil {
				fmt.Println("key       var/secrets/id_ed25519 (print the line with `thrum site key`)")
			} else {
				fmt.Println("key       none yet: `thrum site key` makes one")
			}

			refs, err := daemon.NewPeers(l).Refs()
			if err != nil {
				return err
			}
			if len(refs) == 0 {
				fmt.Println("reaches   nobody: no var/config/<peer>.yaml, so this site asks no other site anything")
				return nil
			}
			sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
			for _, ref := range refs {
				switch {
				case ref.Stale:
					fmt.Printf("reaches   %s: the roster no longer declares that site\n", ref.Name)
				case ref.Reach == "":
					fmt.Printf("reaches   %s: nothing, and no address was ever entered (local half with no reach)\n", ref.Name)
				default:
					fmt.Printf("reaches   %s at %s\n", ref.Name, ref.Reach)
				}
			}
			return nil
		},
	}

	cmd.AddCommand(listen, reach, key, show)
	return cmd
}

// declaredSite reports whether the roster declares a site, which is what keeps a reach from
// being stale: a machine nobody declares is a machine the workspace does not know about.
func declaredSite(l workspace.Layout, name string) bool {
	entries, err := workspace.ReadResources(l, "site")
	if err != nil {
		return false
	}
	_, ok := entries[name]
	return ok
}

// siteName is the name this machine calls itself by.
func siteName(l workspace.Layout) string {
	raw, err := os.ReadFile(l.SiteFile())
	if err != nil {
		return "(unnamed)"
	}
	return strings.TrimSpace(string(raw))
}

// validName is the shape a site name has, which is the shape every name here has.
func validName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return name[0] != '-' && name[len(name)-1] != '-'
}

// readSiteFile is one of this site's own YAML files, as a map, so that writing one key does
// not throw the others away.
func readSiteFile(path string) (map[string]any, error) {
	values := map[string]any{}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if values == nil {
		values = map[string]any{}
	}
	return values, nil
}

// writeSiteFile writes one of this site's own YAML files, and says so when it is a file the
// tooling does not have a name for.
func writeSiteFile(path string, values map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(values)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

// ensureSiteKey makes this site's key the first time it is asked for one, and returns it.
//
// It is the same kind of key the daemon's host key is, and it is the one a peer authorises:
// this site offers it when it dials another, and the other offers its own.
func ensureSiteKey(l workspace.Layout) (ssh.Signer, error) {
	path := filepath.Join(l.Secrets(), "id_ed25519")
	if raw, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(raw)
	}
	if err := os.MkdirAll(l.Secrets(), 0o700); err != nil {
		return nil, err
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		return nil, err
	}
	encoded := pem.EncodeToMemory(block)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(encoded)
}
