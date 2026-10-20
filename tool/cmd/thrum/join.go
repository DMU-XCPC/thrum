package main

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/discovery"
	"github.com/multiagentworkspace/thrum/tool/internal/exchange"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/join"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

func newJoinCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "join <token>",
		Short: "Become another site of an existing workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := join.Decode(args[0])
			if err != nil {
				return err
			}
			if token.Reach == "" {
				reach, err := discovery.Query(token.Workspace, token.Peer, 3*time.Second)
				if err != nil {
					return fmt.Errorf("the inviter did not answer on the LAN: %w", err)
				}
				token.Reach = reach
			}
			root := dir
			if root == "" {
				root = "."
			}
			if err := join.Join(root, token); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "joined %s as %s\n", root, token.Site)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "where the workspace goes (default the current directory)")
	return cmd
}

func newSyncCmd() *cobra.Command {
	var withCode bool
	var job, site string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Exchange the line, the ledger and the store with the peers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			if err := ensureDaemon(l); err != nil {
				return err
			}
			resp, err := daemon.LocalDo(l, daemon.Request{Verb: "history.exchange"})
			if err != nil {
				var refusal *daemon.Refusal
				if errors.As(err, &refusal) {
					return err
				}
				return fmt.Errorf("this site's endpoint is not answering here: %w", err)
			}
			var out struct {
				Settled     []exchange.Resolution `json:"settled"`
				Unreachable []string              `json:"unreachable"`
			}
			if err := json.Unmarshal(resp.Data, &out); err != nil {
				return err
			}
			for _, r := range out.Settled {
				fmt.Fprintf(cmd.OutOrStdout(),
					"settled %s: the earlier claim (%s) keeps it; the other (%s, %s) is void\n",
					r.Path, r.Winner, r.Loser, r.LoserSide)
			}
			for _, name := range out.Unreachable {
				fmt.Fprintf(cmd.OutOrStdout(), "unreachable: %s (left for the next sync)\n", name)
			}
			if withCode {
				if job == "" || site == "" {
					return fmt.Errorf("--code needs --job and --site")
				}
				if err := copyCode(l, site, job); err != nil {
					return err
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "exchanged")
			return nil
		},
	}
	cmd.Flags().BoolVar(&withCode, "code", false, "deprecated: use `thrum code send`")
	_ = cmd.Flags().MarkHidden("code")
	cmd.Flags().StringVar(&job, "job", "", "the job whose working tree is copied with --code")
	cmd.Flags().StringVar(&site, "site", "", "the helper site for --code")
	return cmd
}

// copyCode sends the job's working tree to a helper: tracked, uncommitted and
// untracked files, no .git. It adds no remote and writes no ref.
func copyCode(l layout.Layout, site, job string) error {
	b, _, ok, err := jobs.ReadBrief(l, job)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no job named %q", job)
	}
	dir, ok, err := worktrees.Where(l, b.Repo, job)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("job %q has no worktree here", job)
	}
	out, err := gitx.Run(dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	var rels []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			rels = append(rels, p)
		}
	}
	client, err := daemon.Dial(l, site)
	if err != nil {
		return err
	}
	defer client.Close()
	args, err := json.Marshal(map[string]string{"job": job})
	if err != nil {
		return err
	}
	if _, err := client.Do(daemon.Request{Verb: "code.receive", Args: args}); err != nil {
		return err
	}
	tw := tar.NewWriter(client.Conn())
	for _, rel := range rels {
		path := filepath.Join(dir, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			h.Linkname = link
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(tw, f)
			_ = f.Close()
			if cerr != nil {
				return cerr
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "copied %d files to %s\n", len(rels), site)
	return nil
}
