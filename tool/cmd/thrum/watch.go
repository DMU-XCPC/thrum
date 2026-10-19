package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
)

// newWatchCmd is the extension interface: one process serves every actor a plugin is
// responsible for, and the plugin says which actors those are.
func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "A two-way stream of this workspace's mail, for a plugin",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			out := cmd.OutOrStdout()

			scope := map[string]bool{}
			seen := map[string]bool{}

			type op struct {
				Op     string   `json:"op"`
				Actor  string   `json:"actor"`
				Actors []string `json:"actors"`
			}
			reply := func(v any) error {
				b, err := json.Marshal(v)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(out, "%s\n", b)
				return err
			}
			scopeOf := func() []string {
				names := make([]string, 0, len(scope))
				for a := range scope {
					names = append(names, a)
				}
				sort.Strings(names)
				return names
			}

			lines := make(chan op, 8)
			go func() {
				sc := bufio.NewScanner(cmd.InOrStdin())
				for sc.Scan() {
					var o op
					if err := json.Unmarshal(sc.Bytes(), &o); err != nil {
						lines <- op{Op: "?"}
						continue
					}
					lines <- o
				}
				close(lines)
			}()

			tick := time.NewTicker(500 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case o, ok := <-lines:
					if !ok {
						return nil
					}
					switch o.Op {
					case "watch":
						if o.Actor == "" {
							_ = reply(map[string]any{"kind": "error", "reason": "watch needs an actor"})
							continue
						}
						if _, ok, err := actors.Find(l, o.Actor); err != nil || !ok {
							_ = reply(map[string]any{"kind": "error", "actor": o.Actor, "reason": "actor is not declared"})
							continue
						}
						scope[o.Actor] = true
						_ = reply(map[string]any{"kind": "watching", "actor": o.Actor, "actors": scopeOf()})
					case "unwatch":
						delete(scope, o.Actor)
						_ = reply(map[string]any{"kind": "unwatching", "actor": o.Actor, "actors": scopeOf()})
					case "scope":
						scope = map[string]bool{}
						for _, a := range o.Actors {
							scope[a] = true
						}
						_ = reply(map[string]any{"kind": "scope", "actors": scopeOf()})
					case "ping":
						_ = reply(map[string]any{"kind": "pong"})
					default:
						_ = reply(map[string]any{"kind": "error", "reason": "unknown op"})
					}
				case <-tick.C:
					for a := range scope {
						msgs, err := messages.List(l, a)
						if err != nil {
							continue
						}
						for _, m := range msgs {
							k := a + "\x00" + m.ID
							if seen[k] {
								continue
							}
							seen[k] = true
							_ = reply(map[string]any{"kind": "message.delivered", "actor": a, "message": m})
						}
					}
				}
			}
		},
	}
}
